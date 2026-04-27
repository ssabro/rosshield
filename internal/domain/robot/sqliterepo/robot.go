// Stage C — Robot CRUD + Credential CRUD (한 Tx) + GetCredentialMaterial + RotateCredential.
package sqliterepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// maxRobotNameLen은 robot.name 길이 상한입니다.
const maxRobotNameLen = 200

// CreateRobot는 robot.Service.CreateRobot 구현입니다.
//
// 한 Tx에:
//  1. fleet 존재·활성 검증
//  2. 입력 검증
//  3. credentialID 생성 → KEK로 wrap → INSERT credentials
//  4. robotID 생성 → INSERT robots (FK credential_id)
//  5. audit emit `robot.created`
//
// 한 Tx 위반 시 모두 rollback (P9 원자성).
func (r *Repo) CreateRobot(ctx context.Context, tx storage.Tx, req robot.CreateRobotRequest) (robot.CreateRobotResult, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.CreateRobotResult{}, storage.ErrTenantMissing
	}
	if r.deps.KEK == nil {
		return robot.CreateRobotResult{}, errors.New("robot: KEK not configured")
	}

	if err := validateRobotRequest(&req); err != nil {
		return robot.CreateRobotResult{}, err
	}

	// 1. Fleet 존재 검증 (활성만).
	if err := assertFleetActive(ctx, tx, tenantID, req.FleetID); err != nil {
		return robot.CreateRobotResult{}, err
	}

	now := r.deps.Clock.Now().UTC()
	credentialID := r.deps.IDGen.New("cr")

	// 2. Credential wrap.
	ciphertext, meta, err := robot.WrapMaterial(r.deps.KEK, tenantID, credentialID, req.Material, now)
	if err != nil {
		return robot.CreateRobotResult{}, fmt.Errorf("robot: wrap credential: %w", err)
	}

	cred := robot.Credential{
		ID:               credentialID,
		TenantID:         tenantID,
		Type:             req.Material.Type,
		EncryptedPayload: ciphertext,
		EncryptionMeta:   meta,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := insertCredential(ctx, tx, cred); err != nil {
		return robot.CreateRobotResult{}, fmt.Errorf("robot: insert credential: %w", err)
	}

	// 3. Robot INSERT.
	rb := robot.Robot{
		ID:           r.deps.IDGen.New("ro"),
		TenantID:     tenantID,
		FleetID:      req.FleetID,
		CredentialID: credentialID,
		Name:         strings.TrimSpace(req.Name),
		Host:         strings.TrimSpace(req.Host),
		Port:         req.Port,
		AuthType:     req.AuthType,
		OSDistro:     req.OSDistro,
		ROSDistro:    req.ROSDistro,
		Tags:         req.Tags,
		Role:         req.Role,
		Criticality:  req.Criticality,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := insertRobot(ctx, tx, rb); err != nil {
		return robot.CreateRobotResult{}, mapRobotInsertErr(err)
	}

	// 4. Audit emit.
	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitRobotCreated(ctx, tx, rb, credentialID); err != nil {
			return robot.CreateRobotResult{}, fmt.Errorf("robot: emit audit: %w", err)
		}
	}

	return robot.CreateRobotResult{Robot: rb, Credential: cred}, nil
}

// GetRobot은 robot.Service.GetRobot 구현입니다 (활성만).
func (r *Repo) GetRobot(ctx context.Context, tx storage.Tx, id string) (robot.Robot, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.Robot{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, robotSelectColumns+`
  FROM robots
 WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`,
		id, string(tenantID))
	return scanRobot(row.Scan)
}

// ListRobots는 robot.Service.ListRobots 구현입니다.
func (r *Repo) ListRobots(ctx context.Context, tx storage.Tx, fleetID string) ([]robot.Robot, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	var (
		rows *sql.Rows
		err  error
	)
	if fleetID == "" {
		rows, err = tx.Query(ctx, robotSelectColumns+`
  FROM robots
 WHERE tenant_id = ? AND deleted_at IS NULL
 ORDER BY created_at ASC`, string(tenantID))
	} else {
		rows, err = tx.Query(ctx, robotSelectColumns+`
  FROM robots
 WHERE tenant_id = ? AND fleet_id = ? AND deleted_at IS NULL
 ORDER BY created_at ASC`, string(tenantID), fleetID)
	}
	if err != nil {
		return nil, fmt.Errorf("robot: list robots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []robot.Robot
	for rows.Next() {
		rb, err := scanRobot(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("robot: list robots iterate: %w", err)
	}
	return out, nil
}

// DeleteRobot은 robot.Service.DeleteRobot 구현입니다.
//
// soft delete (deleted_at = now) + 연결된 credential.revoked_at 설정 + audit emit.
// 이미 삭제된 robot에 대해 호출 시 storage.ErrNotFound (Phase 1 멱등 아님).
func (r *Repo) DeleteRobot(ctx context.Context, tx storage.Tx, id string) error {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return storage.ErrTenantMissing
	}

	// 활성 robot 조회 — 삭제 대상의 credential_id 확보.
	rb, err := r.GetRobot(ctx, tx, id)
	if err != nil {
		return err
	}

	now := r.deps.Clock.Now().UTC()
	tsStr := now.Format(rfc3339Nano)

	res, err := tx.Exec(ctx, `
UPDATE robots
   SET deleted_at = ?, updated_at = ?
 WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`,
		tsStr, tsStr, id, string(tenantID))
	if err != nil {
		return fmt.Errorf("robot: soft delete: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("robot: rows affected: %w", err)
	}
	if affected == 0 {
		return storage.ErrNotFound
	}

	// Credential cascade revoke (R3-5 — soft cascade).
	if _, err := tx.Exec(ctx, `
UPDATE credentials
   SET revoked_at = COALESCE(revoked_at, ?), updated_at = ?
 WHERE id = ? AND tenant_id = ?`,
		tsStr, tsStr, rb.CredentialID, string(tenantID)); err != nil {
		return fmt.Errorf("robot: revoke credential: %w", err)
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitRobotDeleted(ctx, tx, id, tenantID); err != nil {
			return fmt.Errorf("robot: emit audit: %w", err)
		}
	}
	return nil
}

// GetCredentialMaterial은 robot.Service.GetCredentialMaterial 구현입니다.
//
// Robot의 credential을 unwrap하여 평문 CredentialMaterial 반환.
// Robot soft-deleted 또는 credential revoked면 storage.ErrNotFound.
func (r *Repo) GetCredentialMaterial(ctx context.Context, tx storage.Tx, robotID string) (robot.CredentialMaterial, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.CredentialMaterial{}, storage.ErrTenantMissing
	}
	if r.deps.KEK == nil {
		return robot.CredentialMaterial{}, errors.New("robot: KEK not configured")
	}

	rb, err := r.GetRobot(ctx, tx, robotID)
	if err != nil {
		return robot.CredentialMaterial{}, err
	}
	cred, err := selectCredentialActive(ctx, tx, tenantID, rb.CredentialID)
	if err != nil {
		return robot.CredentialMaterial{}, err
	}
	return robot.UnwrapMaterial(r.deps.KEK, cred.EncryptedPayload, cred.EncryptionMeta)
}

// RotateCredential은 robot.Service.RotateCredential 구현입니다.
//
// 새 credential 생성 → robot.credential_id 갱신 → 기존 credential.revoked_at 설정 → audit emit.
// 모두 같은 Tx (R3-3 수동 API).
func (r *Repo) RotateCredential(ctx context.Context, tx storage.Tx, req robot.RotateCredentialRequest) (robot.RotateCredentialResult, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.RotateCredentialResult{}, storage.ErrTenantMissing
	}
	if r.deps.KEK == nil {
		return robot.RotateCredentialResult{}, errors.New("robot: KEK not configured")
	}

	rb, err := r.GetRobot(ctx, tx, req.RobotID)
	if err != nil {
		return robot.RotateCredentialResult{}, err
	}
	oldCredID := rb.CredentialID

	now := r.deps.Clock.Now().UTC()
	newCredID := r.deps.IDGen.New("cr")

	ciphertext, meta, err := robot.WrapMaterial(r.deps.KEK, tenantID, newCredID, req.Material, now)
	if err != nil {
		return robot.RotateCredentialResult{}, fmt.Errorf("robot: wrap new credential: %w", err)
	}
	newCred := robot.Credential{
		ID:               newCredID,
		TenantID:         tenantID,
		Type:             req.Material.Type,
		EncryptedPayload: ciphertext,
		EncryptionMeta:   meta,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := insertCredential(ctx, tx, newCred); err != nil {
		return robot.RotateCredentialResult{}, fmt.Errorf("robot: insert new credential: %w", err)
	}

	// Robot.credential_id 갱신 + AuthType 동기화.
	if _, err := tx.Exec(ctx, `
UPDATE robots
   SET credential_id = ?, auth_type = ?, updated_at = ?
 WHERE id = ? AND tenant_id = ?`,
		newCredID, mapMaterialAuthType(req.Material.Type), now.Format(rfc3339Nano),
		req.RobotID, string(tenantID)); err != nil {
		return robot.RotateCredentialResult{}, fmt.Errorf("robot: update robot credential_id: %w", err)
	}

	// 기존 credential revoke.
	if _, err := tx.Exec(ctx, `
UPDATE credentials
   SET revoked_at = COALESCE(revoked_at, ?), updated_at = ?
 WHERE id = ? AND tenant_id = ?`,
		now.Format(rfc3339Nano), now.Format(rfc3339Nano),
		oldCredID, string(tenantID)); err != nil {
		return robot.RotateCredentialResult{}, fmt.Errorf("robot: revoke old credential: %w", err)
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitCredentialRotated(ctx, tx, req.RobotID, oldCredID, newCredID, tenantID); err != nil {
			return robot.RotateCredentialResult{}, fmt.Errorf("robot: emit audit: %w", err)
		}
	}

	return robot.RotateCredentialResult{
		NewCredentialID: newCredID,
		OldCredentialID: oldCredID,
	}, nil
}

// TestConnection은 robot.Service.TestConnection 구현입니다 (Stage E).
//
// 절차: GetRobot(활성 검증·tenant 격리) → GetCredentialMaterial(unwrap) → SSHTester에 위임.
// SSHTester nil이면 ErrSSHTesterNotConfigured (E6 결선 전).
func (r *Repo) TestConnection(ctx context.Context, tx storage.Tx, robotID string) error {
	if r.deps.SSHTester == nil {
		return robot.ErrSSHTesterNotConfigured
	}
	rb, err := r.GetRobot(ctx, tx, robotID)
	if err != nil {
		return err
	}
	mat, err := r.GetCredentialMaterial(ctx, tx, robotID)
	if err != nil {
		return err
	}
	return r.deps.SSHTester.TestConnection(ctx, rb.Host, rb.Port, rb.AuthType, mat)
}

// --- helpers ---

const robotSelectColumns = `
SELECT id, tenant_id, fleet_id, credential_id, name, host, port,
       auth_type, os_distro, ros_distro, tags, role, criticality,
       created_at, updated_at, last_scan_at, deleted_at`

func insertRobot(ctx context.Context, tx storage.Tx, rb robot.Robot) error {
	tagsJSON, err := json.Marshal(rb.Tags)
	if err != nil {
		return fmt.Errorf("robot: marshal tags: %w", err)
	}
	if rb.Tags == nil {
		tagsJSON = []byte("[]")
	}
	_, err = tx.Exec(ctx, `
INSERT INTO robots (
    id, tenant_id, fleet_id, credential_id, name, host, port,
    auth_type, os_distro, ros_distro, tags, role, criticality,
    created_at, updated_at, last_scan_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		rb.ID, string(rb.TenantID), rb.FleetID, rb.CredentialID, rb.Name, rb.Host, rb.Port,
		string(rb.AuthType), rb.OSDistro, rb.ROSDistro, string(tagsJSON), rb.Role, string(rb.Criticality),
		rb.CreatedAt.Format(rfc3339Nano), rb.UpdatedAt.Format(rfc3339Nano))
	return err
}

func insertCredential(ctx context.Context, tx storage.Tx, c robot.Credential) error {
	metaJSON, err := json.Marshal(c.EncryptionMeta)
	if err != nil {
		return fmt.Errorf("robot: marshal encryption meta: %w", err)
	}
	var rotationDueAt sql.NullString
	if c.RotationDueAt != nil {
		rotationDueAt = sql.NullString{String: c.RotationDueAt.Format(rfc3339Nano), Valid: true}
	}
	_, err = tx.Exec(ctx, `
INSERT INTO credentials (
    id, tenant_id, type, encrypted_payload, encryption_meta,
    rotation_due_at, created_at, updated_at, revoked_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		c.ID, string(c.TenantID), string(c.Type), c.EncryptedPayload, string(metaJSON),
		rotationDueAt, c.CreatedAt.Format(rfc3339Nano), c.UpdatedAt.Format(rfc3339Nano))
	return err
}

func selectCredentialActive(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, credentialID string) (robot.Credential, error) {
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, type, encrypted_payload, encryption_meta,
       rotation_due_at, created_at, updated_at, revoked_at
  FROM credentials
 WHERE id = ? AND tenant_id = ? AND revoked_at IS NULL`,
		credentialID, string(tenantID))
	return scanCredential(row.Scan)
}

func scanCredential(scan func(...any) error) (robot.Credential, error) {
	var (
		id, tenantID, credType, metaJSON, createdAt, updatedAt string
		payload                                                []byte
		rotationDueAt, revokedAt                               sql.NullString
	)
	if err := scan(&id, &tenantID, &credType, &payload, &metaJSON,
		&rotationDueAt, &createdAt, &updatedAt, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return robot.Credential{}, storage.ErrNotFound
		}
		return robot.Credential{}, fmt.Errorf("robot: scan credential: %w", err)
	}
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return robot.Credential{}, fmt.Errorf("robot: parse credential created_at: %w", err)
	}
	updated, err := time.Parse(rfc3339Nano, updatedAt)
	if err != nil {
		return robot.Credential{}, fmt.Errorf("robot: parse credential updated_at: %w", err)
	}
	var meta robot.EncryptionMeta
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return robot.Credential{}, fmt.Errorf("robot: unmarshal encryption meta: %w", err)
	}
	c := robot.Credential{
		ID:               id,
		TenantID:         storage.TenantID(tenantID),
		Type:             robot.CredentialType(credType),
		EncryptedPayload: payload,
		EncryptionMeta:   meta,
		CreatedAt:        created,
		UpdatedAt:        updated,
	}
	if rotationDueAt.Valid {
		t, err := time.Parse(rfc3339Nano, rotationDueAt.String)
		if err != nil {
			return robot.Credential{}, fmt.Errorf("robot: parse rotation_due_at: %w", err)
		}
		c.RotationDueAt = &t
	}
	if revokedAt.Valid {
		t, err := time.Parse(rfc3339Nano, revokedAt.String)
		if err != nil {
			return robot.Credential{}, fmt.Errorf("robot: parse credential revoked_at: %w", err)
		}
		c.RevokedAt = &t
	}
	return c, nil
}

func scanRobot(scan func(...any) error) (robot.Robot, error) {
	var (
		id, tenantID, fleetID, credentialID, name, host string
		authType, osDistro, rosDistro, tagsJSON         string
		role, criticality, createdAt, updatedAt         string
		port                                            int
		lastScanAt, deletedAt                           sql.NullString
	)
	if err := scan(&id, &tenantID, &fleetID, &credentialID, &name, &host, &port,
		&authType, &osDistro, &rosDistro, &tagsJSON, &role, &criticality,
		&createdAt, &updatedAt, &lastScanAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return robot.Robot{}, storage.ErrNotFound
		}
		return robot.Robot{}, fmt.Errorf("robot: scan robot: %w", err)
	}
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return robot.Robot{}, fmt.Errorf("robot: parse robot created_at: %w", err)
	}
	updated, err := time.Parse(rfc3339Nano, updatedAt)
	if err != nil {
		return robot.Robot{}, fmt.Errorf("robot: parse robot updated_at: %w", err)
	}
	var tags []string
	if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
		return robot.Robot{}, fmt.Errorf("robot: unmarshal tags: %w", err)
	}
	rb := robot.Robot{
		ID:           id,
		TenantID:     storage.TenantID(tenantID),
		FleetID:      fleetID,
		CredentialID: credentialID,
		Name:         name,
		Host:         host,
		Port:         port,
		AuthType:     robot.AuthType(authType),
		OSDistro:     osDistro,
		ROSDistro:    rosDistro,
		Tags:         tags,
		Role:         role,
		Criticality:  robot.Criticality(criticality),
		CreatedAt:    created,
		UpdatedAt:    updated,
	}
	if lastScanAt.Valid {
		t, err := time.Parse(rfc3339Nano, lastScanAt.String)
		if err != nil {
			return robot.Robot{}, fmt.Errorf("robot: parse last_scan_at: %w", err)
		}
		rb.LastScanAt = &t
	}
	if deletedAt.Valid {
		t, err := time.Parse(rfc3339Nano, deletedAt.String)
		if err != nil {
			return robot.Robot{}, fmt.Errorf("robot: parse robot deleted_at: %w", err)
		}
		rb.DeletedAt = &t
	}
	return rb, nil
}

func validateRobotRequest(req *robot.CreateRobotRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.FleetID = strings.TrimSpace(req.FleetID)

	if req.FleetID == "" {
		return robot.ErrRobotEmptyFleet
	}
	if req.Name == "" {
		return robot.ErrRobotEmptyName
	}
	if len(req.Name) > maxRobotNameLen {
		return robot.ErrRobotNameTooLong
	}
	if req.Host == "" {
		return robot.ErrRobotEmptyHost
	}
	if req.Port == 0 {
		req.Port = 22
	}
	if req.Port < 1 || req.Port > 65535 {
		return robot.ErrRobotInvalidPort
	}
	if req.AuthType == "" {
		req.AuthType = robot.AuthTypePrivateKey
	}
	if req.AuthType != robot.AuthTypePassword && req.AuthType != robot.AuthTypePrivateKey {
		return robot.ErrRobotInvalidAuthType
	}
	// Material.Type과 AuthType 일치 — 사용자가 password AuthType을 골랐는데 material에 PrivateKey면 거부.
	if req.AuthType == robot.AuthTypePassword && req.Material.Type != robot.CredentialTypePassword {
		return robot.ErrRobotInvalidAuthType
	}
	if req.AuthType == robot.AuthTypePrivateKey && req.Material.Type != robot.CredentialTypePrivateKey {
		return robot.ErrRobotInvalidAuthType
	}
	if req.Criticality == "" {
		req.Criticality = robot.CriticalityMedium
	}
	switch req.Criticality {
	case robot.CriticalityLow, robot.CriticalityMedium, robot.CriticalityHigh, robot.CriticalityCritical:
		// OK
	default:
		return robot.ErrRobotInvalidCritical
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	return nil
}

func assertFleetActive(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fleetID string) error {
	row := tx.QueryRow(ctx, `
SELECT 1 FROM fleets
 WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`,
		fleetID, string(tenantID))
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return robot.ErrFleetNotFound
		}
		return fmt.Errorf("robot: lookup fleet: %w", err)
	}
	return nil
}

func mapRobotInsertErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "UNIQUE constraint failed") &&
		!strings.Contains(msg, "constraint failed: UNIQUE") {
		return fmt.Errorf("robot: insert robot: %w", err)
	}
	switch {
	case strings.Contains(msg, "robots.host") || strings.Contains(msg, "robots.port") ||
		strings.Contains(msg, "robots_tenant_host_port_active"):
		return robot.ErrRobotHostPortConflict
	default:
		// 이름 충돌이 기본값 — partial unique on (tenant_id, fleet_id, name).
		return robot.ErrRobotNameDuplicate
	}
}

func mapMaterialAuthType(t robot.CredentialType) string {
	switch t {
	case robot.CredentialTypePassword:
		return string(robot.AuthTypePassword)
	case robot.CredentialTypePrivateKey:
		return string(robot.AuthTypePrivateKey)
	default:
		return ""
	}
}
