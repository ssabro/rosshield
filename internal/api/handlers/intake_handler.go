package handlers

// intake_handler.go — Customer onboarding R1 Stage 2 HTTP handler.
//
// design doc `docs/design/notes/customer-onboarding-design.md` §7 R1 Stage 2 산출.
//
// endpoint:
//   - POST   /api/v1/customers/intake               (운영자 admin — 자가 등록 yaml→JSON)
//   - GET    /api/v1/customers/intakes              (list + status query filter)
//   - GET    /api/v1/customers/intakes/{intakeId}   (단건 조회)
//   - POST   /api/v1/customers/intakes/{id}:accept  (pending→accepted, AcceptedByUserID = claims.Subject)
//   - POST   /api/v1/customers/intakes/{id}:reject  (pending→rejected + reason 필수)
//
// RBAC: handler 자체는 claims 추출 + Subject만 활용. mount는 RequirePermission 또는 admin 그룹.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/intake"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// CreateCustomerIntake: POST /api/v1/customers/intake.
//
// body: {organizationName, primaryContactEmail, primaryContactName, planRequest, intendedUse}
// 응답: 201 + {id, status, createdAt}.
func (h *Handlers) CreateCustomerIntake(w http.ResponseWriter, r *http.Request) {
	if h.deps.Intake == nil {
		writeError(w, http.StatusServiceUnavailable, "intake: service not configured")
		return
	}
	var body struct {
		OrganizationName    string `json:"organizationName"`
		PrimaryContactEmail string `json:"primaryContactEmail"`
		PrimaryContactName  string `json:"primaryContactName"`
		PlanRequest         string `json:"planRequest"`
		IntendedUse         string `json:"intendedUse"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	req := intake.CreateIntakeRequest{
		OrganizationName:    body.OrganizationName,
		PrimaryContactEmail: body.PrimaryContactEmail,
		PrimaryContactName:  body.PrimaryContactName,
		PlanRequest:         intake.PlanRequest(body.PlanRequest),
		IntendedUse:         body.IntendedUse,
	}

	var created intake.CustomerIntake
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		c, e := h.deps.Intake.CreateIntake(ctx, tx, req)
		if e != nil {
			return e
		}
		created = c
		return nil
	})
	if err != nil {
		writeError(w, intakeErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toIntakeView(created))
}

// ListCustomerIntakes: GET /api/v1/customers/intakes?status=pending.
//
// query: status (옵션). 잘못된 status 값 → 400.
func (h *Handlers) ListCustomerIntakes(w http.ResponseWriter, r *http.Request) {
	if h.deps.Intake == nil {
		writeError(w, http.StatusServiceUnavailable, "intake: service not configured")
		return
	}
	filter := intake.ListIntakesFilter{}
	if s := r.URL.Query().Get("status"); s != "" {
		st := intake.IntakeStatus(s)
		if !intake.IsValidStatus(st) {
			writeError(w, http.StatusBadRequest, "invalid status query")
			return
		}
		filter.Status = st
	}

	var rows []intake.CustomerIntake
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Intake.ListIntakes(ctx, tx, filter)
		if e != nil {
			return e
		}
		rows = out
		return nil
	})
	if err != nil {
		writeError(w, intakeErrorStatus(err), err.Error())
		return
	}
	views := make([]intakeView, 0, len(rows))
	for _, row := range rows {
		views = append(views, toIntakeView(row))
	}
	writeJSON(w, http.StatusOK, struct {
		Intakes []intakeView `json:"intakes"`
	}{views})
}

// GetCustomerIntake: GET /api/v1/customers/intakes/{intakeId}.
//
// 404 if not found.
func (h *Handlers) GetCustomerIntake(w http.ResponseWriter, r *http.Request) {
	if h.deps.Intake == nil {
		writeError(w, http.StatusServiceUnavailable, "intake: service not configured")
		return
	}
	id := chi.URLParam(r, "intakeId")
	if id == "" {
		writeError(w, http.StatusBadRequest, "intakeId required")
		return
	}
	var row intake.CustomerIntake
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		c, e := h.deps.Intake.GetIntake(ctx, tx, id)
		if e != nil {
			return e
		}
		row = c
		return nil
	})
	if err != nil {
		writeError(w, intakeErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toIntakeView(row))
}

// AcceptCustomerIntake: POST /api/v1/customers/intakes/{intakeId}:accept.
//
// pending → accepted. AcceptedByUserID = claims.Subject. 이미 terminal 시 409.
func (h *Handlers) AcceptCustomerIntake(w http.ResponseWriter, r *http.Request) {
	if h.deps.Intake == nil {
		writeError(w, http.StatusServiceUnavailable, "intake: service not configured")
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no auth claims in context")
		return
	}
	id := chi.URLParam(r, "intakeId")
	if id == "" {
		writeError(w, http.StatusBadRequest, "intakeId required")
		return
	}
	var row intake.CustomerIntake
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		c, e := h.deps.Intake.AcceptIntake(ctx, tx, intake.AcceptIntakeRequest{
			IntakeID:         id,
			AcceptedByUserID: claims.Subject,
		})
		if e != nil {
			return e
		}
		row = c
		return nil
	})
	if err != nil {
		writeError(w, intakeErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toIntakeView(row))
}

// RejectCustomerIntake: POST /api/v1/customers/intakes/{intakeId}:reject.
//
// body: {reason}. pending → rejected. 이미 terminal 시 409.
func (h *Handlers) RejectCustomerIntake(w http.ResponseWriter, r *http.Request) {
	if h.deps.Intake == nil {
		writeError(w, http.StatusServiceUnavailable, "intake: service not configured")
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no auth claims in context")
		return
	}
	id := chi.URLParam(r, "intakeId")
	if id == "" {
		writeError(w, http.StatusBadRequest, "intakeId required")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	var row intake.CustomerIntake
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		c, e := h.deps.Intake.RejectIntake(ctx, tx, intake.RejectIntakeRequest{
			IntakeID:         id,
			RejectedByUserID: claims.Subject,
			RejectionReason:  body.Reason,
		})
		if e != nil {
			return e
		}
		row = c
		return nil
	})
	if err != nil {
		writeError(w, intakeErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toIntakeView(row))
}

// intakeView는 응답 JSON 형식입니다.
//
// AcceptedAt/RejectedAt/AcceptedByUserID/RejectionReason은 nil이면 omitempty.
// TenantID는 빈 string이면 omitempty (pending intake는 tenant 미생성).
type intakeView struct {
	ID                  string  `json:"id"`
	TenantID            string  `json:"tenantId,omitempty"`
	OrganizationName    string  `json:"organizationName"`
	PrimaryContactEmail string  `json:"primaryContactEmail"`
	PrimaryContactName  string  `json:"primaryContactName"`
	PlanRequest         string  `json:"planRequest"`
	IntendedUse         string  `json:"intendedUse"`
	Status              string  `json:"status"`
	CreatedAt           string  `json:"createdAt"`
	AcceptedAt          *string `json:"acceptedAt,omitempty"`
	AcceptedByUserID    *string `json:"acceptedByUserId,omitempty"`
	RejectedAt          *string `json:"rejectedAt,omitempty"`
	RejectionReason     *string `json:"rejectionReason,omitempty"`
}

func toIntakeView(c intake.CustomerIntake) intakeView {
	v := intakeView{
		ID:                  c.ID,
		TenantID:            string(c.TenantID),
		OrganizationName:    c.OrganizationName,
		PrimaryContactEmail: c.PrimaryContactEmail,
		PrimaryContactName:  c.PrimaryContactName,
		PlanRequest:         string(c.PlanRequest),
		IntendedUse:         c.IntendedUse,
		Status:              string(c.Status),
		CreatedAt:           c.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
	if c.AcceptedAt != nil {
		s := c.AcceptedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
		v.AcceptedAt = &s
	}
	if c.AcceptedByUserID != nil {
		v.AcceptedByUserID = c.AcceptedByUserID
	}
	if c.RejectedAt != nil {
		s := c.RejectedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
		v.RejectedAt = &s
	}
	if c.RejectionReason != nil {
		v.RejectionReason = c.RejectionReason
	}
	return v
}

// intakeErrorStatus는 intake 도메인 sentinel 에러를 HTTP status code로 매핑합니다.
func intakeErrorStatus(err error) int {
	switch {
	case errors.Is(err, intake.ErrIntakeNotFound):
		return http.StatusNotFound
	case errors.Is(err, intake.ErrIntakeNotPending):
		return http.StatusConflict
	case errors.Is(err, intake.ErrEmptyOrganization),
		errors.Is(err, intake.ErrInvalidEmail),
		errors.Is(err, intake.ErrEmptyContactName),
		errors.Is(err, intake.ErrInvalidPlanRequest),
		errors.Is(err, intake.ErrEmptyIntendedUse),
		errors.Is(err, intake.ErrEmptyRejectionReason):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
