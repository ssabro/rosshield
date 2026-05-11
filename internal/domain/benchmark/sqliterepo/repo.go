// Package sqliterepo는 benchmark.Service의 SQLite 어댑터입니다.
//
// InstallPack은 단일 트랜잭션 안에서:
//  1. LoadPackFromTar (tar.gz 검증·파싱 — 도메인 함수)
//  2. INSERT packs (UNIQUE(tenant_id, pack_key))
//  3. INSERT pack_checks (각 check 1 row)
//  4. INSERT pack_lifecycle (state=installed, transitioned_at=now)
//  5. AuditEmitter.EmitPackInstalled
//
// TransitionPack도 동일 패턴 (Transition 검증 → INSERT lifecycle → audit emit).
package sqliterepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock clock.Clock
	IDGen idgen.IDGen
	Audit benchmark.AuditEmitter
	// SignerKeyID는 InstallPack 호출 시 signerKeyID 인자가 비어있을 때 fallback.
	// (서명을 누가 했는지 운영 식별 용도 — 검증은 publicKey로 별도 수행)
	DefaultSignerKeyID string
}

// Repo는 benchmark.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// InstallPack은 benchmark.Service.InstallPack 구현입니다.
func (r *Repo) InstallPack(ctx context.Context, tx storage.Tx, tenantID storage.TenantID,
	tarGzBytes []byte, publicKey []byte, signerKeyID, actorID string,
) (benchmark.Pack, error) {
	if tenantID == "" {
		return benchmark.Pack{}, storage.ErrTenantMissing
	}
	if tx.TenantID() != "" && tx.TenantID() != tenantID {
		return benchmark.Pack{}, fmt.Errorf("benchmark: tx.TenantID()=%q != %q", tx.TenantID(), tenantID)
	}

	pack, err := benchmark.LoadPackFromTar(tarGzBytes, publicKey)
	if err != nil {
		return benchmark.Pack{}, err
	}

	now := r.deps.Clock.Now().UTC()
	pack.ID = r.deps.IDGen.New("pk")
	pack.TenantID = tenantID
	pack.InstalledAt = now
	if signerKeyID != "" {
		pack.SignerKeyID = signerKeyID
	} else {
		pack.SignerKeyID = r.deps.DefaultSignerKeyID
	}

	if err := insertPack(ctx, tx, pack); err != nil {
		if isUniqueViolation(err) {
			return benchmark.Pack{}, benchmark.ErrPackAlreadyInstalled
		}
		return benchmark.Pack{}, err
	}

	for i := range pack.Checks {
		pack.Checks[i].ID = r.deps.IDGen.New("ck")
		pack.Checks[i].PackID = pack.ID
		if err := insertCheck(ctx, tx, pack.Checks[i]); err != nil {
			return benchmark.Pack{}, err
		}
	}

	if err := insertLifecycle(ctx, tx, pack.ID, benchmark.StateInstalled, now, actorID, "installed"); err != nil {
		return benchmark.Pack{}, err
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitPackInstalled(ctx, tx, pack, actorID); err != nil {
			return benchmark.Pack{}, fmt.Errorf("benchmark: emit audit: %w", err)
		}
	}
	return pack, nil
}

// GetPackByKey는 tenant + pack_key로 Pack 메타+체크를 조회합니다.
func (r *Repo) GetPackByKey(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, packKey string) (benchmark.Pack, error) {
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at
  FROM packs
 WHERE tenant_id = ? AND pack_key = ?`,
		string(tenantID), packKey)
	pack, err := scanPackRow(row)
	if err != nil {
		return benchmark.Pack{}, err
	}
	checks, err := r.loadChecks(ctx, tx, pack.ID)
	if err != nil {
		return benchmark.Pack{}, err
	}
	pack.Checks = checks
	return pack, nil
}

// GetPackByID는 packID(pk_<ULID>)로 Pack 메타+체크를 조회합니다.
//
// tenant scope 무시 — caller가 권한 검증 책임. scanrun 결선 시 cross-tenant
// builtin pack도 가져올 수 있어야 함.
func (r *Repo) GetPackByID(ctx context.Context, tx storage.Tx, packID string) (benchmark.Pack, error) {
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at
  FROM packs
 WHERE id = ?`,
		packID)
	pack, err := scanPackRow(row)
	if err != nil {
		return benchmark.Pack{}, err
	}
	checks, err := r.loadChecks(ctx, tx, pack.ID)
	if err != nil {
		return benchmark.Pack{}, err
	}
	pack.Checks = checks
	return pack, nil
}

// ListPacks는 tenant의 모든 Pack 메타(체크 미포함)를 반환합니다.
func (r *Repo) ListPacks(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) ([]benchmark.Pack, error) {
	rows, err := tx.Query(ctx, `
SELECT id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at
  FROM packs
 WHERE tenant_id = ?
 ORDER BY installed_at DESC`,
		string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("benchmark: query packs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []benchmark.Pack
	for rows.Next() {
		pack, err := scanPackRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pack)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("benchmark: iter packs: %w", err)
	}
	return out, nil
}

// CurrentState는 packID의 가장 최근 lifecycle 상태를 반환합니다.
func (r *Repo) CurrentState(ctx context.Context, tx storage.Tx, packID string) (benchmark.State, error) {
	row := tx.QueryRow(ctx, `
SELECT state
  FROM pack_lifecycle
 WHERE pack_id = ?
 ORDER BY transitioned_at DESC
 LIMIT 1`, packID)

	var state string
	err := row.Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", storage.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("benchmark: read current state: %w", err)
	}
	return benchmark.State(state), nil
}

// TransitionPack은 현재 state 조회 → Transition 검증 → INSERT pack_lifecycle + audit emit.
func (r *Repo) TransitionPack(ctx context.Context, tx storage.Tx, packID string, to benchmark.State, actorID, reason string) error {
	from, err := r.CurrentState(ctx, tx, packID)
	if err != nil {
		return err
	}
	if _, err := benchmark.Transition(from, to); err != nil {
		return err
	}
	now := r.deps.Clock.Now().UTC()
	if err := insertLifecycle(ctx, tx, packID, to, now, actorID, reason); err != nil {
		return err
	}
	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitPackLifecycleChanged(ctx, tx, packID, from, to, actorID, reason); err != nil {
			return fmt.Errorf("benchmark: emit lifecycle audit: %w", err)
		}
	}
	return nil
}

// ----- 내부 helpers -----

func insertPack(ctx context.Context, tx storage.Tx, p benchmark.Pack) error {
	_, err := tx.Exec(ctx, `
INSERT INTO packs (
    id, tenant_id, name, version, vendor, pack_key,
    manifest_hash, signer_key_id, installed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, string(p.TenantID), p.Name, p.Version, p.Vendor, p.PackKey,
		p.ManifestHash[:], p.SignerKeyID, p.InstalledAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("benchmark: insert pack: %w", err)
	}
	return nil
}

func insertCheck(ctx context.Context, tx storage.Tx, c benchmark.Check) error {
	var (
		desc, audit, rationale, fix *string
	)
	if c.Description != "" {
		desc = &c.Description
	}
	if c.AuditCommand != "" {
		audit = &c.AuditCommand
	}
	if c.Rationale != "" {
		rationale = &c.Rationale
	}
	if c.FixGuidance != "" {
		fix = &c.FixGuidance
	}
	rule := []byte("null")
	if len(c.EvaluationRule) > 0 {
		rule = c.EvaluationRule
	}

	_, err := tx.Exec(ctx, `
INSERT INTO pack_checks (
    id, pack_id, check_id, title, description, severity,
    audit_command, evaluation_rule, rationale, fix_guidance
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.PackID, c.CheckID, c.Title, desc, string(c.Severity),
		audit, string(rule), rationale, fix)
	if err != nil {
		return fmt.Errorf("benchmark: insert check %q: %w", c.CheckID, err)
	}
	return nil
}

func insertLifecycle(ctx context.Context, tx storage.Tx, packID string, state benchmark.State, t time.Time, actor, reason string) error {
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	_, err := tx.Exec(ctx, `
INSERT INTO pack_lifecycle (pack_id, state, transitioned_at, actor_id, reason)
VALUES (?, ?, ?, ?, ?)`,
		packID, string(state), t.Format(time.RFC3339Nano), actor, reasonPtr)
	if err != nil {
		return fmt.Errorf("benchmark: insert lifecycle: %w", err)
	}
	return nil
}

func scanPackRow(row *sql.Row) (benchmark.Pack, error) {
	var s packScan
	err := row.Scan(&s.id, &s.tenantID, &s.name, &s.version, &s.vendor, &s.packKey,
		&s.manifestHash, &s.signerKeyID, &s.installedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return benchmark.Pack{}, storage.ErrNotFound
	}
	if err != nil {
		return benchmark.Pack{}, fmt.Errorf("benchmark: scan pack: %w", err)
	}
	return assemblePack(s)
}

func scanPackRows(rows *sql.Rows) (benchmark.Pack, error) {
	var s packScan
	if err := rows.Scan(&s.id, &s.tenantID, &s.name, &s.version, &s.vendor, &s.packKey,
		&s.manifestHash, &s.signerKeyID, &s.installedAt); err != nil {
		return benchmark.Pack{}, fmt.Errorf("benchmark: scan pack row: %w", err)
	}
	return assemblePack(s)
}

type packScan struct {
	id, tenantID, name, version, vendor, packKey, signerKeyID, installedAt string
	manifestHash                                                           []byte
}

func assemblePack(s packScan) (benchmark.Pack, error) {
	if len(s.manifestHash) != benchmark.HashSize {
		return benchmark.Pack{}, fmt.Errorf("benchmark: manifest_hash size = %d, want %d", len(s.manifestHash), benchmark.HashSize)
	}
	t, err := time.Parse(time.RFC3339Nano, s.installedAt)
	if err != nil {
		return benchmark.Pack{}, fmt.Errorf("benchmark: parse installed_at: %w", err)
	}
	pack := benchmark.Pack{
		ID:          s.id,
		TenantID:    storage.TenantID(s.tenantID),
		Name:        s.name,
		Version:     s.version,
		Vendor:      s.vendor,
		PackKey:     s.packKey,
		SignerKeyID: s.signerKeyID,
		InstalledAt: t,
	}
	copy(pack.ManifestHash[:], s.manifestHash)
	return pack, nil
}

func (r *Repo) loadChecks(ctx context.Context, tx storage.Tx, packID string) ([]benchmark.Check, error) {
	rows, err := tx.Query(ctx, `
SELECT id, pack_id, check_id, title, description, severity,
       audit_command, evaluation_rule, rationale, fix_guidance
  FROM pack_checks
 WHERE pack_id = ?
 ORDER BY check_id ASC`, packID)
	if err != nil {
		return nil, fmt.Errorf("benchmark: query checks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []benchmark.Check
	for rows.Next() {
		var (
			id, pid, cid, title, severity string
			desc, audit, rationale, fix   sql.NullString
			rule                          string
		)
		if err := rows.Scan(&id, &pid, &cid, &title, &desc, &severity,
			&audit, &rule, &rationale, &fix); err != nil {
			return nil, fmt.Errorf("benchmark: scan check: %w", err)
		}
		out = append(out, benchmark.Check{
			ID:             id,
			PackID:         pid,
			CheckID:        cid,
			Title:          title,
			Description:    desc.String,
			Severity:       benchmark.Severity(severity),
			AuditCommand:   audit.String,
			EvaluationRule: []byte(rule),
			Rationale:      rationale.String,
			FixGuidance:    fix.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("benchmark: iter checks: %w", err)
	}
	return out, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
