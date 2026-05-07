// Package sqliterepo는 webhook.Service의 SQLite 어댑터입니다 (E23 Phase 3).
//
// 책임:
//
//	CreateEndpoint        → webhook_endpoints INSERT
//	UpdateEndpoint        → webhook_endpoints UPDATE (URL·Secret·Events·Format·Enabled)
//	DeleteEndpoint        → webhook_endpoints DELETE (deliveries는 보존 — append-only)
//	GetEndpoint/List      → webhook_endpoints SELECT (tenant scope 격리)
//	Enqueue               → 구독 endpoint들에 대해 webhook_deliveries 일괄 INSERT
//	GetDelivery/List      → webhook_deliveries SELECT
//
// 도메인 결합 (P5):
//
//	본 패키지는 영속만. HTTP 호출·재시도 worker는 후속 stage(E23-B)에서 추가.
//	audit emit은 본 stage에서 결선 안 함 — bootstrap이 EventBus 구독 어댑터로 처리.
package sqliterepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
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

// Repo는 webhook.Service의 SQLite 구현입니다 (영속 전용).
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// CreateEndpoint는 새 endpoint를 INSERT합니다.
//
// URL·Events·Format 검증 + tenant scope 강제. Secret 빈 값이면 ErrEmptySecret.
func (r *Repo) CreateEndpoint(ctx context.Context, tx storage.Tx, ep webhook.WebhookEndpoint) (webhook.WebhookEndpoint, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return webhook.WebhookEndpoint{}, storage.ErrTenantMissing
	}
	if err := webhook.ValidateURL(ep.URL); err != nil {
		return webhook.WebhookEndpoint{}, err
	}
	if strings.TrimSpace(ep.Secret) == "" {
		return webhook.WebhookEndpoint{}, webhook.ErrEmptySecret
	}
	if err := webhook.ValidateEvents(ep.Events); err != nil {
		return webhook.WebhookEndpoint{}, err
	}
	if err := webhook.ValidateFormat(ep.Format); err != nil {
		return webhook.WebhookEndpoint{}, err
	}
	if ep.Format == "" {
		ep.Format = webhook.PayloadFormatJSON
	}

	now := r.deps.Clock.Now().UTC()
	ep.ID = r.deps.IDGen.New("wh")
	ep.TenantID = tenantID
	ep.CreatedAt = now
	ep.UpdatedAt = now

	eventsJSON, err := marshalEvents(ep.Events)
	if err != nil {
		return webhook.WebhookEndpoint{}, fmt.Errorf("webhook: marshal events: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO webhook_endpoints (
    id, tenant_id, url, secret, events, format, enabled, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ep.ID, string(ep.TenantID), ep.URL, ep.Secret, eventsJSON, string(ep.Format),
		boolToInt(ep.Enabled), ep.CreatedAt.Format(rfc3339Nano), ep.UpdatedAt.Format(rfc3339Nano),
	); err != nil {
		return webhook.WebhookEndpoint{}, fmt.Errorf("webhook: insert endpoint: %w", err)
	}
	return ep, nil
}

// UpdateEndpoint는 기존 endpoint를 갱신합니다.
//
// CreatedAt·TenantID는 무시 — 항상 DB의 기존 값 유지.
// 미존재 또는 cross-tenant면 ErrEndpointNotFound.
func (r *Repo) UpdateEndpoint(ctx context.Context, tx storage.Tx, ep webhook.WebhookEndpoint) (webhook.WebhookEndpoint, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return webhook.WebhookEndpoint{}, storage.ErrTenantMissing
	}
	existing, err := r.GetEndpoint(ctx, tx, ep.ID)
	if err != nil {
		return webhook.WebhookEndpoint{}, err
	}
	if err := webhook.ValidateURL(ep.URL); err != nil {
		return webhook.WebhookEndpoint{}, err
	}
	if strings.TrimSpace(ep.Secret) == "" {
		return webhook.WebhookEndpoint{}, webhook.ErrEmptySecret
	}
	if err := webhook.ValidateEvents(ep.Events); err != nil {
		return webhook.WebhookEndpoint{}, err
	}
	if err := webhook.ValidateFormat(ep.Format); err != nil {
		return webhook.WebhookEndpoint{}, err
	}
	if ep.Format == "" {
		ep.Format = webhook.PayloadFormatJSON
	}

	now := r.deps.Clock.Now().UTC()
	eventsJSON, err := marshalEvents(ep.Events)
	if err != nil {
		return webhook.WebhookEndpoint{}, fmt.Errorf("webhook: marshal events: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE webhook_endpoints SET
    url = ?, secret = ?, events = ?, format = ?, enabled = ?, updated_at = ?
WHERE id = ? AND tenant_id = ?`,
		ep.URL, ep.Secret, eventsJSON, string(ep.Format), boolToInt(ep.Enabled),
		now.Format(rfc3339Nano), ep.ID, string(tenantID),
	); err != nil {
		return webhook.WebhookEndpoint{}, fmt.Errorf("webhook: update endpoint: %w", err)
	}

	ep.TenantID = existing.TenantID
	ep.CreatedAt = existing.CreatedAt
	ep.UpdatedAt = now
	return ep, nil
}

// DeleteEndpoint는 endpoint를 제거합니다 (delivery는 보존).
func (r *Repo) DeleteEndpoint(ctx context.Context, tx storage.Tx, endpointID string) error {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return storage.ErrTenantMissing
	}
	res, err := tx.Exec(ctx, `DELETE FROM webhook_endpoints WHERE id = ? AND tenant_id = ?`,
		endpointID, string(tenantID))
	if err != nil {
		return fmt.Errorf("webhook: delete endpoint: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("webhook: rows affected: %w", err)
	}
	if rows == 0 {
		return webhook.ErrEndpointNotFound
	}
	return nil
}

// GetEndpoint는 endpoint를 ID로 조회합니다 (cross-tenant는 ErrEndpointNotFound).
func (r *Repo) GetEndpoint(ctx context.Context, tx storage.Tx, endpointID string) (webhook.WebhookEndpoint, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return webhook.WebhookEndpoint{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, `SELECT id, tenant_id, url, secret, events, format, enabled, created_at, updated_at
FROM webhook_endpoints WHERE id = ?`, endpointID)
	ep, err := scanEndpoint(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return webhook.WebhookEndpoint{}, webhook.ErrEndpointNotFound
		}
		return webhook.WebhookEndpoint{}, err
	}
	if ep.TenantID != tenantID {
		return webhook.WebhookEndpoint{}, webhook.ErrEndpointNotFound
	}
	return ep, nil
}

// ListEndpoints는 tenant scope의 endpoint를 created_at DESC로 반환합니다.
func (r *Repo) ListEndpoints(ctx context.Context, tx storage.Tx) ([]webhook.WebhookEndpoint, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, url, secret, events, format, enabled, created_at, updated_at
FROM webhook_endpoints WHERE tenant_id = ?
ORDER BY created_at DESC`, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("webhook: list endpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []webhook.WebhookEndpoint
	for rows.Next() {
		ep, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

// Enqueue는 도메인 이벤트 1건을 받아, 구독 중인 모든 endpoint에 delivery 1건씩 INSERT합니다.
//
// 흐름:
//
//  1. tenant scope의 모든 endpoint 회수.
//  2. enabled=true + Events 필터 통과인 endpoint만 선별.
//  3. 각 endpoint에 대해 webhook_deliveries INSERT (next_attempt_at = now → 즉시 송출 대기).
//  4. payload 직렬화는 호출자가 evt.Payload에 채워 전달 — 본 메서드는 그대로 저장.
func (r *Repo) Enqueue(ctx context.Context, tx storage.Tx, evt webhook.DomainEvent) ([]webhook.WebhookDelivery, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if evt.TenantID != "" && evt.TenantID != tenantID {
		return nil, fmt.Errorf("webhook: evt.TenantID mismatch tx.TenantID")
	}
	endpoints, err := r.ListEndpoints(ctx, tx)
	if err != nil {
		return nil, err
	}
	now := r.deps.Clock.Now().UTC()

	var deliveries []webhook.WebhookDelivery
	for _, ep := range endpoints {
		if !ep.Enabled {
			continue
		}
		if !webhook.EndpointSubscribesTo(ep, evt.Type) {
			continue
		}
		// SQLite는 nil []byte를 NULL로 취급해 NOT NULL 위반 — 빈 슬라이스로 정규화.
		payload := evt.Payload
		if payload == nil {
			payload = []byte{}
		}
		d := webhook.WebhookDelivery{
			ID:            r.deps.IDGen.New("whd"),
			EndpointID:    ep.ID,
			TenantID:      tenantID,
			EventType:     evt.Type,
			EventID:       evt.EventID,
			Payload:       payload,
			AttemptCount:  0,
			NextAttemptAt: now,
			CreatedAt:     now,
		}
		if _, err := tx.Exec(ctx, `INSERT INTO webhook_deliveries (
    id, endpoint_id, tenant_id, event_type, event_id, payload,
    attempt_count, last_attempted_at, next_attempt_at,
    succeeded, last_response_status, last_error, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, 0, 0, '', ?)`,
			d.ID, d.EndpointID, string(d.TenantID), string(d.EventType), d.EventID, d.Payload,
			d.AttemptCount, d.NextAttemptAt.Format(rfc3339Nano), d.CreatedAt.Format(rfc3339Nano),
		); err != nil {
			return nil, fmt.Errorf("webhook: insert delivery: %w", err)
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, nil
}

// GetDelivery는 delivery를 ID로 조회합니다.
func (r *Repo) GetDelivery(ctx context.Context, tx storage.Tx, deliveryID string) (webhook.WebhookDelivery, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return webhook.WebhookDelivery{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, `SELECT id, endpoint_id, tenant_id, event_type, event_id, payload,
       attempt_count, last_attempted_at, next_attempt_at,
       succeeded, last_response_status, last_error, created_at
FROM webhook_deliveries WHERE id = ?`, deliveryID)
	d, err := scanDelivery(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return webhook.WebhookDelivery{}, webhook.ErrDeliveryNotFound
		}
		return webhook.WebhookDelivery{}, err
	}
	if d.TenantID != tenantID {
		return webhook.WebhookDelivery{}, webhook.ErrDeliveryNotFound
	}
	return d, nil
}

// ListDeliveries는 endpoint별 delivery를 created_at DESC로 반환합니다.
//
// limit <= 0이면 default 50.
func (r *Repo) ListDeliveries(ctx context.Context, tx storage.Tx, endpointID string, limit int) ([]webhook.WebhookDelivery, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	rows, err := tx.Query(ctx, `SELECT id, endpoint_id, tenant_id, event_type, event_id, payload,
       attempt_count, last_attempted_at, next_attempt_at,
       succeeded, last_response_status, last_error, created_at
FROM webhook_deliveries
WHERE endpoint_id = ? AND tenant_id = ?
ORDER BY created_at DESC LIMIT ?`,
		endpointID, string(tenantID), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook: list deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []webhook.WebhookDelivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// === scanner helpers ===

// rowScanner는 *sql.Row와 *sql.Rows를 같은 인터페이스로 처리합니다.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEndpoint(s rowScanner) (webhook.WebhookEndpoint, error) {
	var (
		id, tid, urlStr, secret, eventsJSON, format string
		enabledInt                                  int
		createdStr, updatedStr                      string
	)
	if err := s.Scan(&id, &tid, &urlStr, &secret, &eventsJSON, &format,
		&enabledInt, &createdStr, &updatedStr,
	); err != nil {
		return webhook.WebhookEndpoint{}, err
	}
	events, err := unmarshalEvents(eventsJSON)
	if err != nil {
		return webhook.WebhookEndpoint{}, fmt.Errorf("webhook: unmarshal events: %w", err)
	}
	createdAt, _ := time.Parse(rfc3339Nano, createdStr)
	updatedAt, _ := time.Parse(rfc3339Nano, updatedStr)
	return webhook.WebhookEndpoint{
		ID:        id,
		TenantID:  storage.TenantID(tid),
		URL:       urlStr,
		Secret:    secret,
		Events:    events,
		Format:    webhook.Format(format),
		Enabled:   enabledInt != 0,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func scanDelivery(s rowScanner) (webhook.WebhookDelivery, error) {
	var (
		id, epID, tid, etype, eventID string
		payload                       []byte
		attempt                       int
		lastAttempted                 sql.NullString
		nextAttemptStr                string
		succeededInt                  int
		lastStatus                    int
		lastErr                       string
		createdStr                    string
	)
	if err := s.Scan(&id, &epID, &tid, &etype, &eventID, &payload,
		&attempt, &lastAttempted, &nextAttemptStr,
		&succeededInt, &lastStatus, &lastErr, &createdStr,
	); err != nil {
		return webhook.WebhookDelivery{}, err
	}
	nextAt, _ := time.Parse(rfc3339Nano, nextAttemptStr)
	createdAt, _ := time.Parse(rfc3339Nano, createdStr)
	d := webhook.WebhookDelivery{
		ID:                 id,
		EndpointID:         epID,
		TenantID:           storage.TenantID(tid),
		EventType:          webhook.EventType(etype),
		EventID:            eventID,
		Payload:            payload,
		AttemptCount:       attempt,
		NextAttemptAt:      nextAt,
		Succeeded:          succeededInt != 0,
		LastResponseStatus: lastStatus,
		LastError:          lastErr,
		CreatedAt:          createdAt,
	}
	if lastAttempted.Valid {
		t, _ := time.Parse(rfc3339Nano, lastAttempted.String)
		d.LastAttemptedAt = &t
	}
	return d, nil
}

func marshalEvents(events []webhook.EventType) (string, error) {
	if len(events) == 0 {
		return "[]", nil
	}
	strs := make([]string, len(events))
	for i, e := range events {
		strs[i] = string(e)
	}
	b, err := json.Marshal(strs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalEvents(raw string) ([]webhook.EventType, error) {
	if strings.TrimSpace(raw) == "" || raw == "[]" {
		return nil, nil
	}
	var strs []string
	if err := json.Unmarshal([]byte(raw), &strs); err != nil {
		return nil, err
	}
	out := make([]webhook.EventType, len(strs))
	for i, s := range strs {
		out[i] = webhook.EventType(s)
	}
	return out, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
