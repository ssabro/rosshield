// Package sqliterepo는 compliance.Service의 SQLite 어댑터입니다 (E15 Phase 2).
//
// 책임:
//
//	CreateProfile     → compliance_profiles INSERT + audit emit (compliance.profile.created)
//	GenerateSnapshot  → ScanReader → outcomes 집계 → AggregateControlStatuses
//	                    → AuditReader.Head 캡처 → framework_snapshots INSERT + audit emit
//	ListProfiles      → tenant 단위 SELECT (created_at ASC)
//	ListSnapshots     → profile 단위 SELECT (created_at DESC, LIMIT)
//
// 도메인 결합 (P5):
//
//	compliance/sqliterepo는 audit·scan 패키지를 직접 import하지 않고, AuditEmitter·ScanReader·
//	AuditReader interface로 주입받습니다. cmd/* bootstrap이 audit.Service·scan.Service를 어댑팅.
package sqliterepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const rfc3339Nano = time.RFC3339Nano
const defaultListLimit = 50

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock       clock.Clock
	IDGen       idgen.IDGen
	Audit       compliance.AuditEmitter // bootstrap에서 audit.Service 어댑터 주입.
	ScanReader  compliance.ScanReader   // bootstrap에서 scan.Service 어댑터 주입.
	AuditReader compliance.AuditReader  // bootstrap에서 audit.Service 어댑터 주입 (Head 조회).
	Suggester   compliance.LLMSuggester // E17 — SuggestMappings에서 사용. 미주입(nil)이면 ErrLLMSuggesterUnavailable.
}

// Repo는 compliance.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// CreateProfile은 새 ComplianceProfile을 INSERT하고 audit emit합니다.
//
// (tenant_id, framework) UNIQUE 위반 시 ErrProfileExists.
// FrameworkVersion이 embed YAML과 다르면 ErrFrameworkVersionMismatch.
func (r *Repo) CreateProfile(ctx context.Context, tx storage.Tx, req compliance.CreateProfileRequest) (compliance.ComplianceProfile, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return compliance.ComplianceProfile{}, storage.ErrTenantMissing
	}
	if err := compliance.ValidateFramework(req.Framework); err != nil {
		return compliance.ComplianceProfile{}, err
	}
	// embed YAML과 version 정합성 검증.
	_, yamlVersion, err := compliance.LoadFramework(req.Framework)
	if err != nil {
		return compliance.ComplianceProfile{}, err
	}
	if strings.TrimSpace(req.FrameworkVersion) != yamlVersion {
		return compliance.ComplianceProfile{}, fmt.Errorf("%w: requested=%q, embedded=%q",
			compliance.ErrFrameworkVersionMismatch, req.FrameworkVersion, yamlVersion)
	}

	now := r.deps.Clock.Now().UTC()
	profile := compliance.ComplianceProfile{
		ID:                 r.deps.IDGen.New("cp"),
		TenantID:           tenantID,
		Framework:          req.Framework,
		FrameworkVersion:   yamlVersion,
		Enabled:            req.Enabled,
		CustomizationsJSON: []byte("[]"),
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	enabledInt := 0
	if profile.Enabled {
		enabledInt = 1
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO compliance_profiles (
    id, tenant_id, framework, framework_version, enabled,
    customizations_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.ID, string(profile.TenantID), string(profile.Framework), profile.FrameworkVersion,
		enabledInt, string(profile.CustomizationsJSON),
		profile.CreatedAt.Format(rfc3339Nano), profile.UpdatedAt.Format(rfc3339Nano),
	); err != nil {
		if isUniqueViolation(err) {
			return compliance.ComplianceProfile{}, compliance.ErrProfileExists
		}
		return compliance.ComplianceProfile{}, fmt.Errorf("compliance: insert profile: %w", err)
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitProfileCreated(ctx, tx, profile); err != nil {
			return compliance.ComplianceProfile{}, fmt.Errorf("compliance: emit profile.created: %w", err)
		}
	}
	return profile, nil
}

// GenerateSnapshot은 sessionID 기준 control status 집계 + audit anchor 캡처 + INSERT.
//
// 흐름:
//  1. profileID로 profile 조회 (tenant 격리)
//  2. ScanReader.ListResultsForSession으로 outcomes 수집
//  3. LoadFramework로 통제 정의 로드
//  4. AggregateControlStatuses + ScoreFromStatuses + CountStatuses
//  5. AuditReader.Head로 chain anchor 캡처
//  6. framework_snapshots INSERT + audit emit
func (r *Repo) GenerateSnapshot(ctx context.Context, tx storage.Tx, profileID, sessionID string) (compliance.FrameworkSnapshot, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return compliance.FrameworkSnapshot{}, storage.ErrTenantMissing
	}

	profile, err := r.getProfile(ctx, tx, profileID)
	if err != nil {
		return compliance.FrameworkSnapshot{}, err
	}

	if r.deps.ScanReader == nil {
		return compliance.FrameworkSnapshot{}, errors.New("compliance: ScanReader dependency missing")
	}
	results, err := r.deps.ScanReader.ListResultsForSession(ctx, tx, sessionID)
	if err != nil {
		return compliance.FrameworkSnapshot{}, fmt.Errorf("compliance: list scan results: %w", err)
	}

	controls, _, err := compliance.LoadFramework(profile.Framework)
	if err != nil {
		return compliance.FrameworkSnapshot{}, err
	}

	statuses := compliance.AggregateControlStatuses(controls, results)
	score := compliance.ScoreFromStatuses(statuses)
	pass, fail, partial, na, unmapped := compliance.CountStatuses(statuses)

	if r.deps.AuditReader == nil {
		return compliance.FrameworkSnapshot{}, errors.New("compliance: AuditReader dependency missing")
	}
	head, err := r.deps.AuditReader.Head(ctx, tx, tenantID)
	if err != nil {
		return compliance.FrameworkSnapshot{}, fmt.Errorf("compliance: read audit head: %w", err)
	}

	statusesJSON, err := json.Marshal(statuses)
	if err != nil {
		return compliance.FrameworkSnapshot{}, fmt.Errorf("compliance: marshal statuses: %w", err)
	}

	now := r.deps.Clock.Now().UTC()
	snapshot := compliance.FrameworkSnapshot{
		ID:                 r.deps.IDGen.New("fs"),
		TenantID:           tenantID,
		ProfileID:          profileID,
		SessionID:          sessionID,
		OverallScore:       score,
		PassCount:          pass,
		FailCount:          fail,
		PartialCount:       partial,
		NotApplicableCount: na,
		UnmappedCount:      unmapped,
		ChainHeadSeq:       head.Seq,
		ChainHeadHash:      head.Hash,
		Statuses:           statuses,
		CreatedAt:          now,
	}

	var sessionArg any
	if sessionID == "" {
		sessionArg = nil
	} else {
		sessionArg = sessionID
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO framework_snapshots (
    id, tenant_id, profile_id, session_id,
    overall_score, pass_count, fail_count, partial_count,
    not_applicable_count, unmapped_count,
    chain_head_seq, chain_head_hash, statuses_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.ID, string(snapshot.TenantID), snapshot.ProfileID, sessionArg,
		snapshot.OverallScore, snapshot.PassCount, snapshot.FailCount, snapshot.PartialCount,
		snapshot.NotApplicableCount, snapshot.UnmappedCount,
		snapshot.ChainHeadSeq, snapshot.ChainHeadHash, string(statusesJSON),
		snapshot.CreatedAt.Format(rfc3339Nano),
	); err != nil {
		return compliance.FrameworkSnapshot{}, fmt.Errorf("compliance: insert snapshot: %w", err)
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitSnapshotGenerated(ctx, tx, snapshot); err != nil {
			return compliance.FrameworkSnapshot{}, fmt.Errorf("compliance: emit snapshot.generated: %w", err)
		}
	}
	return snapshot, nil
}

// ListProfiles는 tenant의 모든 profile을 created_at ASC로 반환합니다.
func (r *Repo) ListProfiles(ctx context.Context, tx storage.Tx) ([]compliance.ComplianceProfile, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, profileSelectColumns+`
  FROM compliance_profiles
 WHERE tenant_id = ?
 ORDER BY created_at ASC`,
		string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("compliance: list profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []compliance.ComplianceProfile
	for rows.Next() {
		p, err := scanProfileRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("compliance: list profiles iterate: %w", err)
	}
	return out, nil
}

// ListSnapshots는 profile의 snapshot을 created_at DESC로 반환합니다.
func (r *Repo) ListSnapshots(ctx context.Context, tx storage.Tx, profileID string, limit int) ([]compliance.FrameworkSnapshot, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	rows, err := tx.Query(ctx, snapshotSelectColumns+`
  FROM framework_snapshots
 WHERE tenant_id = ? AND profile_id = ?
 ORDER BY created_at DESC
 LIMIT ?`,
		string(tenantID), profileID, limit)
	if err != nil {
		return nil, fmt.Errorf("compliance: list snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []compliance.FrameworkSnapshot
	for rows.Next() {
		s, err := scanSnapshotRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("compliance: list snapshots iterate: %w", err)
	}
	return out, nil
}

// --- private helpers ---

func (r *Repo) getProfile(ctx context.Context, tx storage.Tx, id string) (compliance.ComplianceProfile, error) {
	tenantID := tx.TenantID()
	row := tx.QueryRow(ctx, profileSelectColumns+`
  FROM compliance_profiles
 WHERE id = ? AND tenant_id = ?`,
		id, string(tenantID))
	p, err := scanProfileRow(row.Scan)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return compliance.ComplianceProfile{}, compliance.ErrProfileNotFound
		}
		return compliance.ComplianceProfile{}, err
	}
	return p, nil
}

const profileSelectColumns = `
SELECT id, tenant_id, framework, framework_version, enabled,
       customizations_json, created_at, updated_at`

const snapshotSelectColumns = `
SELECT id, tenant_id, profile_id, session_id,
       overall_score, pass_count, fail_count, partial_count,
       not_applicable_count, unmapped_count,
       chain_head_seq, chain_head_hash, statuses_json, created_at`

func scanProfileRow(scanFn func(...any) error) (compliance.ComplianceProfile, error) {
	var (
		id, tenantID, framework, version string
		customizations                   string
		enabled                          int
		createdAt, updatedAt             string
	)
	if err := scanFn(&id, &tenantID, &framework, &version, &enabled,
		&customizations, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return compliance.ComplianceProfile{}, storage.ErrNotFound
		}
		return compliance.ComplianceProfile{}, fmt.Errorf("compliance: scan profile row: %w", err)
	}
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return compliance.ComplianceProfile{}, fmt.Errorf("compliance: parse created_at: %w", err)
	}
	updated, err := time.Parse(rfc3339Nano, updatedAt)
	if err != nil {
		return compliance.ComplianceProfile{}, fmt.Errorf("compliance: parse updated_at: %w", err)
	}
	return compliance.ComplianceProfile{
		ID:                 id,
		TenantID:           storage.TenantID(tenantID),
		Framework:          compliance.Framework(framework),
		FrameworkVersion:   version,
		Enabled:            enabled != 0,
		CustomizationsJSON: []byte(customizations),
		CreatedAt:          created,
		UpdatedAt:          updated,
	}, nil
}

func scanSnapshotRow(scanFn func(...any) error) (compliance.FrameworkSnapshot, error) {
	var (
		id, tenantID, profileID                                    string
		sessionID                                                  sql.NullString
		overallScore                                               float64
		passCount, failCount, partialCount, naCount, unmappedCount int
		chainHeadSeq                                               int64
		chainHeadHash, statusesJSON, createdAt                     string
	)
	if err := scanFn(&id, &tenantID, &profileID, &sessionID,
		&overallScore, &passCount, &failCount, &partialCount,
		&naCount, &unmappedCount,
		&chainHeadSeq, &chainHeadHash, &statusesJSON, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return compliance.FrameworkSnapshot{}, storage.ErrNotFound
		}
		return compliance.FrameworkSnapshot{}, fmt.Errorf("compliance: scan snapshot row: %w", err)
	}
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return compliance.FrameworkSnapshot{}, fmt.Errorf("compliance: parse snapshot created_at: %w", err)
	}
	var statuses []compliance.ControlStatus
	if statusesJSON != "" {
		if err := json.Unmarshal([]byte(statusesJSON), &statuses); err != nil {
			return compliance.FrameworkSnapshot{}, fmt.Errorf("compliance: unmarshal statuses: %w", err)
		}
	}
	snap := compliance.FrameworkSnapshot{
		ID:                 id,
		TenantID:           storage.TenantID(tenantID),
		ProfileID:          profileID,
		OverallScore:       overallScore,
		PassCount:          passCount,
		FailCount:          failCount,
		PartialCount:       partialCount,
		NotApplicableCount: naCount,
		UnmappedCount:      unmappedCount,
		ChainHeadSeq:       chainHeadSeq,
		ChainHeadHash:      chainHeadHash,
		Statuses:           statuses,
		CreatedAt:          created,
	}
	if sessionID.Valid {
		snap.SessionID = sessionID.String
	}
	return snap, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
