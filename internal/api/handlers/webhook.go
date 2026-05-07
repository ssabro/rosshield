package handlers

// webhook.go — E23-C Webhook HTTP CRUD 표면.
//
// 6개 endpoint:
//   POST   /api/v1/webhooks                          → CreateEndpoint  → 201
//   GET    /api/v1/webhooks                          → ListEndpoints   → 200
//   GET    /api/v1/webhooks/{endpointId}             → GetEndpoint     → 200/404
//   PUT    /api/v1/webhooks/{endpointId}             → UpdateEndpoint  → 200/404
//   DELETE /api/v1/webhooks/{endpointId}             → DeleteEndpoint  → 204/404
//   GET    /api/v1/webhooks/{endpointId}/deliveries  → ListDeliveries  → 200
//
// 모든 endpoint는 AuthMiddleware로 보호되며 tenant scope에서 동작합니다 (storage.TenantIDFromContext).
// Secret은 응답에서 마스킹(마지막 4자만 노출) — UI는 "rotation 후 1회만 평문 표시" 패턴이 별도 필요시 후속 stage.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// webhookEndpointResponse는 응답에 포함되는 endpoint 메타입니다.
//
// Secret은 평문 노출 금지 — secretLast4(마지막 4자)만 클라이언트에 제공.
// 운영자가 회전 시 새 secret 본문을 별도 채널(secret manager 또는 KMS)로 관리해야 함.
type webhookEndpointResponse struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenantId"`
	URL         string   `json:"url"`
	SecretLast4 string   `json:"secretLast4"`
	Events      []string `json:"events"`
	Format      string   `json:"format"`
	Enabled     bool     `json:"enabled"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// listWebhookEndpointsResponse는 GET /api/v1/webhooks 응답 본문입니다.
type listWebhookEndpointsResponse struct {
	Endpoints []webhookEndpointResponse `json:"endpoints"`
}

// webhookDeliveryResponse는 응답에 포함되는 delivery 1건입니다.
//
// payload는 base64 인코딩 — 클라이언트가 raw bytes로 디코드 가능. 운영 UI는 head만 표시.
type webhookDeliveryResponse struct {
	ID                 string `json:"id"`
	EndpointID         string `json:"endpointId"`
	TenantID           string `json:"tenantId"`
	EventType          string `json:"eventType"`
	EventID            string `json:"eventId"`
	PayloadBase64      string `json:"payloadBase64,omitempty"`
	AttemptCount       int    `json:"attemptCount"`
	LastAttemptedAt    string `json:"lastAttemptedAt,omitempty"`
	NextAttemptAt      string `json:"nextAttemptAt"`
	Succeeded          bool   `json:"succeeded"`
	LastResponseStatus int    `json:"lastResponseStatus"`
	LastError          string `json:"lastError,omitempty"`
	CreatedAt          string `json:"createdAt"`
}

// listWebhookDeliveriesResponse는 GET /api/v1/webhooks/{id}/deliveries 응답입니다.
type listWebhookDeliveriesResponse struct {
	Deliveries []webhookDeliveryResponse `json:"deliveries"`
}

// createWebhookEndpointRequest는 POST /api/v1/webhooks 요청 본문입니다.
type createWebhookEndpointRequest struct {
	URL     string   `json:"url"`
	Secret  string   `json:"secret"`
	Events  []string `json:"events,omitempty"`
	Format  string   `json:"format,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"` // 미지정 → true.
}

// updateWebhookEndpointRequest는 PUT /api/v1/webhooks/{id} 요청 본문입니다.
type updateWebhookEndpointRequest struct {
	URL     string   `json:"url"`
	Secret  string   `json:"secret"`
	Events  []string `json:"events,omitempty"`
	Format  string   `json:"format,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"` // 미지정 → 기존 값 유지 X — 본 stage는 enabled를 항상 명시.
}

// CreateWebhookEndpoint는 POST /api/v1/webhooks 핸들러입니다 (E23-C).
//
// 검증:
//   - tenant 미주입 → 401
//   - JSON parse 실패 → 400
//   - 도메인 sentinel(ErrInvalidURL/ErrInvalidEvent/ErrUnknownFormat/ErrEmptySecret) → 400
//
// 응답 201 + endpoint 메타.
func (h *Handlers) CreateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Webhook == nil {
		writeError(w, http.StatusServiceUnavailable, "webhook service not configured")
		return
	}

	var req createWebhookEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	ep := webhook.WebhookEndpoint{
		URL:     req.URL,
		Secret:  req.Secret,
		Events:  toEventTypes(req.Events),
		Format:  webhook.Format(req.Format),
		Enabled: enabled,
	}

	var created webhook.WebhookEndpoint
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Webhook.CreateEndpoint(ctx, tx, ep)
		if e != nil {
			return e
		}
		created = out
		return nil
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWebhookEndpointResponse(created))
}

// ListWebhookEndpoints는 GET /api/v1/webhooks 핸들러입니다.
func (h *Handlers) ListWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Webhook == nil {
		writeJSON(w, http.StatusOK, listWebhookEndpointsResponse{Endpoints: []webhookEndpointResponse{}})
		return
	}

	var endpoints []webhook.WebhookEndpoint
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Webhook.ListEndpoints(ctx, tx)
		if e != nil {
			return e
		}
		endpoints = out
		return nil
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}

	out := listWebhookEndpointsResponse{Endpoints: make([]webhookEndpointResponse, 0, len(endpoints))}
	for _, ep := range endpoints {
		out.Endpoints = append(out.Endpoints, toWebhookEndpointResponse(ep))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetWebhookEndpoint는 GET /api/v1/webhooks/{endpointId} 핸들러입니다.
func (h *Handlers) GetWebhookEndpoint(w http.ResponseWriter, r *http.Request, endpointID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Webhook == nil {
		writeError(w, http.StatusServiceUnavailable, "webhook service not configured")
		return
	}

	var ep webhook.WebhookEndpoint
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Webhook.GetEndpoint(ctx, tx, endpointID)
		if e != nil {
			return e
		}
		ep = out
		return nil
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toWebhookEndpointResponse(ep))
}

// UpdateWebhookEndpoint는 PUT /api/v1/webhooks/{endpointId} 핸들러입니다.
func (h *Handlers) UpdateWebhookEndpoint(w http.ResponseWriter, r *http.Request, endpointID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Webhook == nil {
		writeError(w, http.StatusServiceUnavailable, "webhook service not configured")
		return
	}

	var req updateWebhookEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	ep := webhook.WebhookEndpoint{
		ID:      endpointID,
		URL:     req.URL,
		Secret:  req.Secret,
		Events:  toEventTypes(req.Events),
		Format:  webhook.Format(req.Format),
		Enabled: enabled,
	}

	var updated webhook.WebhookEndpoint
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Webhook.UpdateEndpoint(ctx, tx, ep)
		if e != nil {
			return e
		}
		updated = out
		return nil
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toWebhookEndpointResponse(updated))
}

// DeleteWebhookEndpoint는 DELETE /api/v1/webhooks/{endpointId} 핸들러입니다.
//
// delivery는 보존(append-only) — endpoint 메타만 제거.
func (h *Handlers) DeleteWebhookEndpoint(w http.ResponseWriter, r *http.Request, endpointID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Webhook == nil {
		writeError(w, http.StatusServiceUnavailable, "webhook service not configured")
		return
	}

	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		return h.deps.Webhook.DeleteEndpoint(ctx, tx, endpointID)
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListWebhookDeliveries는 GET /api/v1/webhooks/{endpointId}/deliveries 핸들러입니다.
//
// query: limit (선택, 1~500). 정렬은 created_at DESC.
func (h *Handlers) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request, endpointID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Webhook == nil {
		writeJSON(w, http.StatusOK, listWebhookDeliveriesResponse{Deliveries: []webhookDeliveryResponse{}})
		return
	}

	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	var deliveries []webhook.WebhookDelivery
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Webhook.ListDeliveries(ctx, tx, endpointID, limit)
		if e != nil {
			return e
		}
		deliveries = out
		return nil
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}

	out := listWebhookDeliveriesResponse{Deliveries: make([]webhookDeliveryResponse, 0, len(deliveries))}
	for _, d := range deliveries {
		out.Deliveries = append(out.Deliveries, toWebhookDeliveryResponse(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// === helpers ===

// toEventTypes는 string slice를 webhook.EventType slice로 변환합니다.
//
// 빈 slice는 nil이 아닌 빈 slice로 정규화 — 도메인 ValidateEvents가 nil도 허용하지만
// 상태 표현 일관성을 위해 빈 slice 유지.
func toEventTypes(in []string) []webhook.EventType {
	if len(in) == 0 {
		return nil
	}
	out := make([]webhook.EventType, 0, len(in))
	for _, s := range in {
		out = append(out, webhook.EventType(s))
	}
	return out
}

// toWebhookEndpointResponse는 도메인 endpoint를 응답 DTO로 변환합니다.
//
// Secret은 마지막 4자만 노출. Events nil은 빈 slice로 정규화.
func toWebhookEndpointResponse(ep webhook.WebhookEndpoint) webhookEndpointResponse {
	events := make([]string, 0, len(ep.Events))
	for _, e := range ep.Events {
		events = append(events, string(e))
	}
	return webhookEndpointResponse{
		ID:          ep.ID,
		TenantID:    string(ep.TenantID),
		URL:         ep.URL,
		SecretLast4: maskSecret(ep.Secret),
		Events:      events,
		Format:      string(ep.Format),
		Enabled:     ep.Enabled,
		CreatedAt:   ep.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   ep.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// toWebhookDeliveryResponse는 도메인 delivery를 응답 DTO로 변환합니다.
//
// Payload는 base64 — UI는 raw 또는 디코드 후 표시. nil/빈 slice는 빈 문자열.
func toWebhookDeliveryResponse(d webhook.WebhookDelivery) webhookDeliveryResponse {
	resp := webhookDeliveryResponse{
		ID:                 d.ID,
		EndpointID:         d.EndpointID,
		TenantID:           string(d.TenantID),
		EventType:          string(d.EventType),
		EventID:            d.EventID,
		AttemptCount:       d.AttemptCount,
		NextAttemptAt:      d.NextAttemptAt.UTC().Format("2006-01-02T15:04:05Z"),
		Succeeded:          d.Succeeded,
		LastResponseStatus: d.LastResponseStatus,
		LastError:          d.LastError,
		CreatedAt:          d.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if len(d.Payload) > 0 {
		resp.PayloadBase64 = encodeBase64(d.Payload)
	}
	if d.LastAttemptedAt != nil {
		resp.LastAttemptedAt = d.LastAttemptedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return resp
}

// maskSecret은 secret의 마지막 4자만 노출합니다.
//
// secret 길이 < 4 → 전체 별표 4개.
// secret 길이 >= 4 → "****<last4>".
func maskSecret(secret string) string {
	if len(secret) < 4 {
		return "****"
	}
	return secret[len(secret)-4:]
}

// encodeBase64는 stdlib base64 (RFC 4648 standard)으로 인코딩합니다.
//
// 별도 함수로 둠 — 후속 stage에서 url-safe variant로 교체할 수 있도록 wrapper 유지.
func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// writeWebhookError는 webhook 도메인 sentinel을 HTTP status로 매핑하여 응답합니다.
//
// ErrEndpointNotFound/ErrDeliveryNotFound → 404
// ErrInvalidURL/ErrInvalidEvent/ErrUnknownFormat/ErrEmptySecret → 400
// 그 외 → errorStatusFor (500 fallback).
func writeWebhookError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webhook.ErrEndpointNotFound),
		errors.Is(err, webhook.ErrDeliveryNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, webhook.ErrInvalidURL),
		errors.Is(err, webhook.ErrInvalidEvent),
		errors.Is(err, webhook.ErrUnknownFormat),
		errors.Is(err, webhook.ErrEmptySecret):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, errorStatusFor(err), err.Error())
	}
}

// === chi 어댑터 (Mount 헬퍼) ===
//
// Mount.go가 직접 chi.URLParam을 받아 핸들러 본체에 전달 — robot.go·sso.go와 동일 패턴.

// getWebhookEndpointFromChi는 chi router용 어댑터입니다.
func (h *Handlers) getWebhookEndpointFromChi(w http.ResponseWriter, r *http.Request) {
	h.GetWebhookEndpoint(w, r, chi.URLParam(r, "endpointId"))
}

// updateWebhookEndpointFromChi는 chi router용 어댑터입니다.
func (h *Handlers) updateWebhookEndpointFromChi(w http.ResponseWriter, r *http.Request) {
	h.UpdateWebhookEndpoint(w, r, chi.URLParam(r, "endpointId"))
}

// deleteWebhookEndpointFromChi는 chi router용 어댑터입니다.
func (h *Handlers) deleteWebhookEndpointFromChi(w http.ResponseWriter, r *http.Request) {
	h.DeleteWebhookEndpoint(w, r, chi.URLParam(r, "endpointId"))
}

// listWebhookDeliveriesFromChi는 chi router용 어댑터입니다.
func (h *Handlers) listWebhookDeliveriesFromChi(w http.ResponseWriter, r *http.Request) {
	h.ListWebhookDeliveries(w, r, chi.URLParam(r, "endpointId"))
}
