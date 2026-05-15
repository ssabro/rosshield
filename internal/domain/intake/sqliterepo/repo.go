// Package sqliterepo는 intake.Service의 SQLite 어댑터입니다 (R1 Stage 1).
//
// 책임:
//
//	CreateIntake → customer_intakes INSERT (status='pending')
//	GetIntake    → customer_intakes SELECT
//	ListIntakes  → customer_intakes SELECT (status filter + LIMIT)
//	AcceptIntake → customer_intakes UPDATE (status='accepted', accepted_at, accepted_by_user_id, tenant_id)
//	RejectIntake → customer_intakes UPDATE (status='rejected', rejected_at, rejection_reason)
//
// 도메인 경계 (P5):
//
//	본 패키지는 intake 패키지만 import. tenant·audit·license는 cmd/* bootstrap이 결선
//	(R1 Stage 2~4 — handler·auto-provisioning 시점에 같은 Tx로 묶음).
//
// 본 stage는 Tx의 TenantID를 강제하지 않습니다 — intake row는 *tenant 생성 전* 단계
// 데이터로, Bootstrap Tx로 진입 가능 (운영자 admin 전역 권한 가정). Stage 2 handler에서
// RBAC permission 'customer:intake:read'/'customer:intake:write' 강제 예정.
package sqliterepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/domain/intake"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const rfc3339Nano = time.RFC3339Nano
const defaultListLimit = 50

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock clock.Clock
	IDGen idgen.IDGen
}

// Repo는 intake.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// CreateIntake는 새 intake row를 status='pending'으로 INSERT 합니다.
//
// validation 위반 시 intake.Validate* sentinel 반환.
// PrimaryContactEmail은 lowercase normalize 후 저장.
func (r *Repo) CreateIntake(ctx context.Context, tx storage.Tx, req intake.CreateIntakeRequest) (intake.CustomerIntake, error) {
	if err := intake.ValidateCreateRequest(req); err != nil {
		return intake.CustomerIntake{}, err
	}

	now := r.deps.Clock.Now().UTC()
	row := intake.CustomerIntake{
		ID:                  r.deps.IDGen.New("ci"),
		OrganizationName:    req.OrganizationName,
		PrimaryContactEmail: intake.NormalizeEmail(req.PrimaryContactEmail),
		PrimaryContactName:  req.PrimaryContactName,
		PlanRequest:         req.PlanRequest,
		IntendedUse:         req.IntendedUse,
		Status:              intake.StatusPending,
		CreatedAt:           now,
	}

	if _, err := tx.Exec(ctx, `INSERT INTO customer_intakes (
    id, tenant_id, organization_name, primary_contact_email, primary_contact_name,
    plan_request, intended_use, status, created_at,
    accepted_at, accepted_by_user_id, rejected_at, rejection_reason
) VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL)`,
		row.ID, row.OrganizationName, row.PrimaryContactEmail, row.PrimaryContactName,
		string(row.PlanRequest), row.IntendedUse, string(row.Status), row.CreatedAt.Format(rfc3339Nano),
	); err != nil {
		return intake.CustomerIntake{}, fmt.Errorf("intake: insert: %w", err)
	}
	return row, nil
}

// GetIntake는 intakeID로 row를 조회합니다.
func (r *Repo) GetIntake(ctx context.Context, tx storage.Tx, intakeID string) (intake.CustomerIntake, error) {
	row := tx.QueryRow(ctx, selectColumns+` FROM customer_intakes WHERE id = ?`, intakeID)
	out, err := scanIntake(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return intake.CustomerIntake{}, intake.ErrIntakeNotFound
		}
		return intake.CustomerIntake{}, err
	}
	return out, nil
}

// ListIntakes는 filter 조건의 row를 created_at DESC로 반환합니다.
func (r *Repo) ListIntakes(ctx context.Context, tx storage.Tx, filter intake.ListIntakesFilter) ([]intake.CustomerIntake, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}

	var (
		rows *sql.Rows
		err  error
	)
	if filter.Status == "" {
		rows, err = tx.Query(ctx, selectColumns+` FROM customer_intakes
ORDER BY created_at DESC LIMIT ?`, limit)
	} else {
		if !intake.IsValidStatus(filter.Status) {
			return nil, fmt.Errorf("intake: invalid status filter %q", filter.Status)
		}
		rows, err = tx.Query(ctx, selectColumns+` FROM customer_intakes
WHERE status = ?
ORDER BY created_at DESC LIMIT ?`, string(filter.Status), limit)
	}
	if err != nil {
		return nil, fmt.Errorf("intake: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []intake.CustomerIntake
	for rows.Next() {
		row, err := scanIntake(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// AcceptIntake는 status=pending인 row를 accepted로 전환합니다.
//
// 같은 Tx에 tenant.Create 등이 묶일 수 있음 (Stage 3 결선).
// 본 stage는 intake row UPDATE만.
func (r *Repo) AcceptIntake(ctx context.Context, tx storage.Tx, req intake.AcceptIntakeRequest) (intake.CustomerIntake, error) {
	existing, err := r.GetIntake(ctx, tx, req.IntakeID)
	if err != nil {
		return intake.CustomerIntake{}, err
	}
	if existing.Status != intake.StatusPending {
		return intake.CustomerIntake{}, intake.ErrIntakeNotPending
	}

	now := r.deps.Clock.Now().UTC()
	tenantIDArg := nullIfEmpty(string(req.TenantID))
	acceptedByArg := nullIfEmpty(req.AcceptedByUserID)

	if _, err := tx.Exec(ctx, `UPDATE customer_intakes SET
    status = ?, accepted_at = ?, accepted_by_user_id = ?, tenant_id = ?
WHERE id = ? AND status = 'pending'`,
		string(intake.StatusAccepted), now.Format(rfc3339Nano), acceptedByArg, tenantIDArg, req.IntakeID,
	); err != nil {
		return intake.CustomerIntake{}, fmt.Errorf("intake: accept: %w", err)
	}

	existing.Status = intake.StatusAccepted
	existing.AcceptedAt = &now
	if req.AcceptedByUserID != "" {
		userID := req.AcceptedByUserID
		existing.AcceptedByUserID = &userID
	}
	if req.TenantID != "" {
		existing.TenantID = req.TenantID
	}
	return existing, nil
}

// RejectIntake는 status=pending인 row를 rejected로 전환합니다.
func (r *Repo) RejectIntake(ctx context.Context, tx storage.Tx, req intake.RejectIntakeRequest) (intake.CustomerIntake, error) {
	if req.RejectionReason == "" {
		return intake.CustomerIntake{}, intake.ErrEmptyRejectionReason
	}
	existing, err := r.GetIntake(ctx, tx, req.IntakeID)
	if err != nil {
		return intake.CustomerIntake{}, err
	}
	if existing.Status != intake.StatusPending {
		return intake.CustomerIntake{}, intake.ErrIntakeNotPending
	}

	now := r.deps.Clock.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE customer_intakes SET
    status = ?, rejected_at = ?, rejection_reason = ?
WHERE id = ? AND status = 'pending'`,
		string(intake.StatusRejected), now.Format(rfc3339Nano), req.RejectionReason, req.IntakeID,
	); err != nil {
		return intake.CustomerIntake{}, fmt.Errorf("intake: reject: %w", err)
	}

	existing.Status = intake.StatusRejected
	existing.RejectedAt = &now
	reason := req.RejectionReason
	existing.RejectionReason = &reason
	return existing, nil
}

// === scanner helpers ===

const selectColumns = `SELECT
    id, tenant_id, organization_name, primary_contact_email, primary_contact_name,
    plan_request, intended_use, status, created_at,
    accepted_at, accepted_by_user_id, rejected_at, rejection_reason`

// rowScanner는 *sql.Row와 *sql.Rows를 같은 인터페이스로 처리합니다.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanIntake(s rowScanner) (intake.CustomerIntake, error) {
	var (
		id, orgName, contactEmail, contactName, planReq, intendedUse, status, createdStr string
		tenantID, acceptedBy, rejectionReason                                            sql.NullString
		acceptedAt, rejectedAt                                                           sql.NullString
	)
	if err := s.Scan(&id, &tenantID, &orgName, &contactEmail, &contactName,
		&planReq, &intendedUse, &status, &createdStr,
		&acceptedAt, &acceptedBy, &rejectedAt, &rejectionReason,
	); err != nil {
		return intake.CustomerIntake{}, err
	}
	createdAt, _ := time.Parse(rfc3339Nano, createdStr)
	row := intake.CustomerIntake{
		ID:                  id,
		OrganizationName:    orgName,
		PrimaryContactEmail: contactEmail,
		PrimaryContactName:  contactName,
		PlanRequest:         intake.PlanRequest(planReq),
		IntendedUse:         intendedUse,
		Status:              intake.IntakeStatus(status),
		CreatedAt:           createdAt,
	}
	if tenantID.Valid {
		row.TenantID = storage.TenantID(tenantID.String)
	}
	if acceptedAt.Valid {
		t, _ := time.Parse(rfc3339Nano, acceptedAt.String)
		row.AcceptedAt = &t
	}
	if acceptedBy.Valid {
		v := acceptedBy.String
		row.AcceptedByUserID = &v
	}
	if rejectedAt.Valid {
		t, _ := time.Parse(rfc3339Nano, rejectedAt.String)
		row.RejectedAt = &t
	}
	if rejectionReason.Valid {
		v := rejectionReason.String
		row.RejectionReason = &v
	}
	return row, nil
}

// nullIfEmpty는 빈 문자열을 sql.NullString{Valid:false}로, 비어있지 않으면 Valid:true로
// 변환합니다 — DB에 NULL 또는 string 저장 분기.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
