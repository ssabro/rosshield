package sqliterepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// host_key.go — TOFU host key 어댑터 (scanrun SSH 통합 Stage 1).
//
// design doc `docs/design/notes/scanrun-ssh-integration-design.md` §6 Stage 1.
// repo.go·robot.go에서 분리 — 도메인 격리 + 파일 크기 한계 회피(repo.go 355 + robot.go 584).

// RecordFirstTouch는 robot.HostKeyService.RecordFirstTouch 구현입니다.
//
// 멱등: 같은 (tenant, robot, fingerprint) UNIQUE — 중복 호출 시 LastVerifiedAt + trust_state='trusted'만
// 갱신하고 같은 row 반환. trust_state='revoked'였던 row도 'trusted'로 복구.
//
// audit 'robot.host_key.first_touched' emit (Deps.HostKeyAudit이 non-nil 시).
// emit은 새로 INSERT된 경우와 revoked → trusted 복구 시에만 — LastVerifiedAt 단순 갱신은 noise이므로 emit 안 함.
func (r *Repo) RecordFirstTouch(ctx context.Context, tx storage.Tx, req robot.RecordFirstTouchRequest) (robot.RobotHostKey, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.RobotHostKey{}, storage.ErrTenantMissing
	}
	if err := validateFirstTouchRequest(req); err != nil {
		return robot.RobotHostKey{}, err
	}

	now := r.deps.Clock.Now().UTC()

	// 멱등: 같은 (tenant, robot, fingerprint) row 존재 확인.
	existing, err := selectHostKeyByFingerprint(ctx, tx, tenantID, req.RobotID, req.FingerprintSHA256)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return robot.RobotHostKey{}, fmt.Errorf("robot: select host key: %w", err)
	}

	if err == nil {
		// 존재 — LastVerifiedAt 갱신 + 필요 시 'revoked' → 'trusted' 복구.
		recovered := existing.TrustState == robot.HostKeyTrustStateRevoked
		if _, err := tx.Exec(ctx,
			`UPDATE robot_host_keys SET last_verified_at = ?, trust_state = ?
			   WHERE id = ? AND tenant_id = ?`,
			now.Format(rfc3339Nano), string(robot.HostKeyTrustStateTrusted),
			existing.ID, string(tenantID)); err != nil {
			return robot.RobotHostKey{}, fmt.Errorf("robot: update host key: %w", err)
		}
		existing.LastVerifiedAt = now
		existing.TrustState = robot.HostKeyTrustStateTrusted

		// recovered 시에만 audit emit (revoked → trusted는 의미 있는 상태 변화).
		if recovered && r.deps.HostKeyAudit != nil {
			if err := r.deps.HostKeyAudit.EmitHostKeyFirstTouched(ctx, tx, existing); err != nil {
				return robot.RobotHostKey{}, fmt.Errorf("robot: emit host key first_touched audit: %w", err)
			}
		}
		return existing, nil
	}

	// 신규 INSERT.
	hk := robot.RobotHostKey{
		ID:                r.deps.IDGen.New("hk"),
		TenantID:          tenantID,
		RobotID:           req.RobotID,
		FingerprintSHA256: req.FingerprintSHA256,
		KeyType:           req.KeyType,
		KeyBlob:           append([]byte(nil), req.KeyBlob...), // copy — 호출자 mutation 방지
		FirstSeenAt:       now,
		LastVerifiedAt:    now,
		TrustState:        robot.HostKeyTrustStateTrusted,
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO robot_host_keys
    (id, tenant_id, robot_id, fingerprint_sha256, key_type, key_blob,
     first_seen_at, last_verified_at, trust_state)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hk.ID, string(hk.TenantID), hk.RobotID, hk.FingerprintSHA256, hk.KeyType, hk.KeyBlob,
		hk.FirstSeenAt.Format(rfc3339Nano), hk.LastVerifiedAt.Format(rfc3339Nano),
		string(hk.TrustState),
	); err != nil {
		if isUniqueViolation(err) {
			// race — 동시 INSERT, 동일 (tenant, robot, fingerprint).
			// 다시 SELECT해 멱등 보장.
			again, selectErr := selectHostKeyByFingerprint(ctx, tx, tenantID, req.RobotID, req.FingerprintSHA256)
			if selectErr != nil {
				return robot.RobotHostKey{}, fmt.Errorf("robot: insert host key (race): %w", err)
			}
			return again, nil
		}
		return robot.RobotHostKey{}, fmt.Errorf("robot: insert host key: %w", err)
	}

	if r.deps.HostKeyAudit != nil {
		if err := r.deps.HostKeyAudit.EmitHostKeyFirstTouched(ctx, tx, hk); err != nil {
			return robot.RobotHostKey{}, fmt.Errorf("robot: emit host key first_touched audit: %w", err)
		}
	}
	return hk, nil
}

// GetTrustedKey는 robot.HostKeyService.GetTrustedKey 구현입니다.
//
// 같은 robot의 trusted row가 0건이면 storage.ErrNotFound — 호출자(KnownHostsManager)가 first-touch 진입.
// 다중 trusted row(이론적으로는 RecordFirstTouch가 같은 fingerprint만 trusted 보장)는
// 가장 최신 LastVerifiedAt 기준 단일 row 반환.
func (r *Repo) GetTrustedKey(ctx context.Context, tx storage.Tx, robotID string) (robot.RobotHostKey, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.RobotHostKey{}, storage.ErrTenantMissing
	}
	if strings.TrimSpace(robotID) == "" {
		return robot.RobotHostKey{}, robot.ErrHostKeyEmptyRobotID
	}
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, robot_id, fingerprint_sha256, key_type, key_blob,
       first_seen_at, last_verified_at, trust_state
  FROM robot_host_keys
 WHERE tenant_id = ? AND robot_id = ? AND trust_state = ?
 ORDER BY last_verified_at DESC
 LIMIT 1`,
		string(tenantID), robotID, string(robot.HostKeyTrustStateTrusted))
	hk, err := scanHostKeyRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return robot.RobotHostKey{}, storage.ErrNotFound
	}
	if err != nil {
		return robot.RobotHostKey{}, fmt.Errorf("robot: select trusted host key: %w", err)
	}
	return hk, nil
}

// ResetTrust는 robot.HostKeyService.ResetTrust 구현입니다.
//
// 운영자 명시 reset — (tenant, robot)의 모든 trusted row를 revoked로 marking.
// 영향 row 수 반환. audit 'robot.host_key.reset' emit (Deps.HostKeyAudit non-nil).
//
// 영향 row가 0이면 audit emit 없이 0 반환 — no-op (이미 reset된 상태).
func (r *Repo) ResetTrust(ctx context.Context, tx storage.Tx, robotID string) (int, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return 0, storage.ErrTenantMissing
	}
	if strings.TrimSpace(robotID) == "" {
		return 0, robot.ErrHostKeyEmptyRobotID
	}

	res, err := tx.Exec(ctx, `
UPDATE robot_host_keys
   SET trust_state = ?
 WHERE tenant_id = ? AND robot_id = ? AND trust_state = ?`,
		string(robot.HostKeyTrustStateRevoked), string(tenantID), robotID,
		string(robot.HostKeyTrustStateTrusted))
	if err != nil {
		return 0, fmt.Errorf("robot: update host key trust: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("robot: rows affected: %w", err)
	}

	revokedCount := int(affected)
	if revokedCount > 0 && r.deps.HostKeyAudit != nil {
		if err := r.deps.HostKeyAudit.EmitHostKeyReset(ctx, tx, robotID, tenantID, revokedCount); err != nil {
			return revokedCount, fmt.Errorf("robot: emit host key reset audit: %w", err)
		}
	}
	return revokedCount, nil
}

// selectHostKeyByFingerprint는 (tenant, robot, fingerprint)로 단일 row를 조회합니다.
//
// 미존재 시 sql.ErrNoRows.
func selectHostKeyByFingerprint(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, robotID, fingerprint string) (robot.RobotHostKey, error) {
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, robot_id, fingerprint_sha256, key_type, key_blob,
       first_seen_at, last_verified_at, trust_state
  FROM robot_host_keys
 WHERE tenant_id = ? AND robot_id = ? AND fingerprint_sha256 = ?`,
		string(tenantID), robotID, fingerprint)
	return scanHostKeyRow(row)
}

// scanHostKeyRow는 단일 row를 RobotHostKey로 매핑합니다.
//
// row가 *sql.Row 또는 storage.Tx의 동등 표면이라 가정. timestamp는 RFC3339Nano 문자열.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanHostKeyRow(row rowScanner) (robot.RobotHostKey, error) {
	var (
		hk           robot.RobotHostKey
		tenantID     string
		firstSeenStr string
		lastVerified string
		trustState   string
	)
	if err := row.Scan(
		&hk.ID, &tenantID, &hk.RobotID, &hk.FingerprintSHA256, &hk.KeyType, &hk.KeyBlob,
		&firstSeenStr, &lastVerified, &trustState,
	); err != nil {
		return robot.RobotHostKey{}, err
	}
	hk.TenantID = storage.TenantID(tenantID)
	first, err := time.Parse(rfc3339Nano, firstSeenStr)
	if err != nil {
		return robot.RobotHostKey{}, fmt.Errorf("robot: parse first_seen_at: %w", err)
	}
	hk.FirstSeenAt = first
	last, err := time.Parse(rfc3339Nano, lastVerified)
	if err != nil {
		return robot.RobotHostKey{}, fmt.Errorf("robot: parse last_verified_at: %w", err)
	}
	hk.LastVerifiedAt = last
	hk.TrustState = robot.HostKeyTrustState(trustState)
	return hk, nil
}

// validateFirstTouchRequest는 RecordFirstTouchRequest 입력 검증입니다.
func validateFirstTouchRequest(req robot.RecordFirstTouchRequest) error {
	if strings.TrimSpace(req.RobotID) == "" {
		return robot.ErrHostKeyEmptyRobotID
	}
	if strings.TrimSpace(req.FingerprintSHA256) == "" {
		return robot.ErrHostKeyEmptyFingerprint
	}
	if strings.TrimSpace(req.KeyType) == "" {
		return robot.ErrHostKeyEmptyKeyType
	}
	if len(req.KeyBlob) == 0 {
		return robot.ErrHostKeyEmptyKeyBlob
	}
	return nil
}
