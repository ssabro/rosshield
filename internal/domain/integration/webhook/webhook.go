// Package webhook은 외부 SIEM·통합 시스템으로 이벤트를 송출하는 도메인입니다 (E23 Phase 3).
//
// 책임:
//
//   - WebhookEndpoint CRUD: 테넌트별 등록 가능한 송출 대상 (URL + secret + event 필터).
//   - WebhookDelivery 영속: 모든 송출 시도가 append-only 큐에 기록됨 (P9 불변성).
//   - 재시도 정책: 실패 delivery는 1m·5m·15m·1h·24h 5회 재시도 후 dead-letter.
//   - HMAC-SHA256 payload 서명: 수신자가 공유 secret으로 위·변조 검증.
//   - SIEM 형식 변환: CEF (ArcSight 호환) + ECS (Elastic 호환).
//
// 도메인 결합 규칙 (P5):
//
//	webhook 도메인은 audit·scan·insight 패키지를 직접 import하지 않습니다.
//	bootstrap이 EventBus 구독자를 결선하여 도메인 이벤트를 webhook.Service.Enqueue로 라우팅.
//	Enqueue는 영속만 — 실제 HTTP 호출은 후속 stage(Process worker)에서 수행.
//
// 외부 의존성 0:
//
//	stdlib `crypto/hmac`/`crypto/sha256`/`encoding/hex`만 사용. SIEM 라이브러리 도입 X.
//
// 결정: phase3-backlog.md E23 (1주 추정). 본 stage는 도메인 + sqliterepo + 마이그레이션까지.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// EventType은 webhook이 구독 가능한 도메인 이벤트 종류입니다.
//
// 본 stage는 3종 — Phase 3 후속 epic에서 확장 가능 (관리자 동의 후 enum 추가).
type EventType string

const (
	EventScanCompleted   EventType = "scan.completed"
	EventInsightCreated  EventType = "insight.created"
	EventAuditCheckpoint EventType = "audit.checkpoint"
)

// KnownEventTypes는 현재 시점에 webhook이 송출 가능한 모든 event 종류입니다.
//
// Endpoint.Events 등록 시 검증에 사용 (UnknownEventType은 ErrInvalidEvent).
var KnownEventTypes = []EventType{
	EventScanCompleted,
	EventInsightCreated,
	EventAuditCheckpoint,
}

// Format은 SIEM 호환 직렬화 형식입니다 (siem.go).
//
// 함수 이름과 충돌 방지를 위해 const는 PayloadFormat* 접두사 사용
// (FormatCEF/FormatECS는 siem.go의 변환 함수).
type Format string

const (
	PayloadFormatCEF  Format = "cef"  // ArcSight Common Event Format (Splunk OOTB).
	PayloadFormatECS  Format = "ecs"  // Elastic Common Schema (JSON).
	PayloadFormatJSON Format = "json" // raw rosshield JSON (default — 클라이언트가 파싱).
)

// WebhookEndpoint는 테넌트가 등록한 외부 송출 대상 1건입니다.
//
// Secret은 HMAC-SHA256 서명 키 (수신자와 공유). 평문 보관 — 본 stage는 KMS 통합 없음.
// Format은 송출 시 payload 직렬화 형식 (default JSON, SIEM은 CEF/ECS).
type WebhookEndpoint struct {
	ID        string
	TenantID  storage.TenantID
	URL       string
	Secret    string      // HMAC 키. UI는 마지막 4자만 표시.
	Events    []EventType // 구독할 event 종류 (빈 배열이면 모든 known event).
	Format    Format      // payload 직렬화 형식.
	Enabled   bool        // false면 Enqueue가 skip — 기존 delivery 보존.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// WebhookDelivery는 단일 송출 시도 1건입니다 (append-only — UPDATE는 attempt 갱신만).
//
// 본 stage는 INSERT만 — 재시도 worker는 후속 stage에서 attempt_count·next_attempt_at 갱신.
type WebhookDelivery struct {
	ID                 string
	EndpointID         string
	TenantID           storage.TenantID
	EventType          EventType
	EventID            string // 원천 EventBus.Event.ID — cross-reference용.
	Payload            []byte // 직렬화된 본문 (CEF/ECS/JSON 중 endpoint.Format에 따른).
	AttemptCount       int    // 0=대기, 1~5=시도 횟수.
	LastAttemptedAt    *time.Time
	NextAttemptAt      time.Time // 0=즉시 송출, 미래시각=재시도 대기.
	Succeeded          bool      // true면 더 이상 시도 안 함.
	LastResponseStatus int       // HTTP status (0=시도 전).
	LastError          string    // 빈 값=시도 전 또는 성공.
	CreatedAt          time.Time
}

// Service는 webhook 도메인 진입점입니다 (E23).
//
// 본 stage는 영속만 정의. Process worker는 후속 stage(E23-B)에서 추가.
type Service interface {
	// CreateEndpoint는 새 endpoint를 INSERT합니다.
	// URL 형식 검증, Events enum 검증 — 위반 시 ErrInvalidURL/ErrInvalidEvent.
	CreateEndpoint(ctx context.Context, tx storage.Tx, ep WebhookEndpoint) (WebhookEndpoint, error)

	// UpdateEndpoint는 기존 endpoint를 갱신합니다 (URL·Secret·Events·Format·Enabled).
	// CreatedAt·TenantID는 무시. 미존재 시 ErrEndpointNotFound.
	UpdateEndpoint(ctx context.Context, tx storage.Tx, ep WebhookEndpoint) (WebhookEndpoint, error)

	// DeleteEndpoint는 endpoint를 제거합니다 (delivery는 append-only — 보존).
	// 미존재 시 ErrEndpointNotFound.
	DeleteEndpoint(ctx context.Context, tx storage.Tx, endpointID string) error

	// GetEndpoint는 endpoint를 ID로 조회합니다 (cross-tenant는 ErrEndpointNotFound).
	GetEndpoint(ctx context.Context, tx storage.Tx, endpointID string) (WebhookEndpoint, error)

	// ListEndpoints는 tenant scope의 모든 endpoint를 created_at DESC로 반환합니다.
	ListEndpoints(ctx context.Context, tx storage.Tx) ([]WebhookEndpoint, error)

	// Enqueue는 도메인 이벤트 1건을 받아, 구독 중인 모든 endpoint에 대해
	// WebhookDelivery 1건씩을 INSERT합니다 (즉시 송출 대기 큐).
	//
	// endpoint.Enabled=false 또는 Events 필터 mismatch면 skip.
	// 본 stage는 영속만 — 실제 HTTP 호출은 Process worker에서.
	Enqueue(ctx context.Context, tx storage.Tx, evt DomainEvent) ([]WebhookDelivery, error)

	// GetDelivery는 delivery를 ID로 조회합니다.
	GetDelivery(ctx context.Context, tx storage.Tx, deliveryID string) (WebhookDelivery, error)

	// ListDeliveries는 endpoint별 delivery를 created_at DESC로 반환합니다.
	// limit <= 0이면 default 50.
	ListDeliveries(ctx context.Context, tx storage.Tx, endpointID string, limit int) ([]WebhookDelivery, error)
}

// DomainEvent는 webhook이 받는 도메인 이벤트의 minimal DTO입니다 (P5 — 원천 도메인 직접 import 회피).
//
// bootstrap이 EventBus 구독 어댑터를 결선해 audit/scan/insight 이벤트를 본 DTO로 매핑.
// EventID는 EventBus.Event.ID를 그대로 전달 — cross-reference용.
type DomainEvent struct {
	EventID    string
	TenantID   storage.TenantID
	Type       EventType
	OccurredAt time.Time

	// Payload는 도메인이 정의한 JSON 본문입니다 (이미 직렬화됨).
	// SIEM 변환은 siem.go가 본 raw 본문을 입력으로 사용.
	Payload []byte

	// Severity는 옵션 — insight.created 등에서만 의미 있음. CEF severity 매핑에 사용.
	Severity string

	// Aggregate는 옵션 — UI cross-link용 (예: "ScanSession", "ss_...").
	AggregateType string
	AggregateID   string
}

// 공통 sentinel error.
var (
	ErrEndpointNotFound = errors.New("webhook: endpoint not found")
	ErrDeliveryNotFound = errors.New("webhook: delivery not found")
	ErrInvalidURL       = errors.New("webhook: invalid URL (require absolute http/https)")
	ErrInvalidEvent     = errors.New("webhook: unknown event type")
	ErrUnknownFormat    = errors.New("webhook: unknown payload format")
	ErrEmptySecret      = errors.New("webhook: secret is required for HMAC signing")
)

// 재시도 정책 (R23-1 — 5회·1m·5m·15m·1h·24h).
//
// AttemptCount=0은 enqueue 직후, 1~5가 실제 시도 횟수.
// retryDelays[i]는 i번째 시도 실패 후 대기 시간 (i=0이면 첫 시도 실패 후 1m 후 재시도).
var retryDelays = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	1 * time.Hour,
	24 * time.Hour,
}

// MaxRetryAttempts는 dead-letter 진입 전 최대 시도 횟수입니다.
const MaxRetryAttempts = 5

// NextRetryDelay는 attemptCount에 대한 다음 재시도 대기 시간을 반환합니다.
//
// attemptCount=1 → 1m (첫 시도 실패 후 1m 대기).
// attemptCount=5 → 24h.
// attemptCount>=MaxRetryAttempts → 0, false (dead-letter — 더 이상 시도 안 함).
func NextRetryDelay(attemptCount int) (time.Duration, bool) {
	if attemptCount < 1 || attemptCount > MaxRetryAttempts {
		return 0, false
	}
	return retryDelays[attemptCount-1], true
}

// SignaturePrefix는 X-Rosshield-Signature 헤더의 알고리즘 prefix입니다.
const SignaturePrefix = "sha256="

// SignatureHeader는 HMAC 서명을 담는 HTTP 헤더 이름입니다.
const SignatureHeader = "X-Rosshield-Signature"

// EventTypeHeader는 EventType을 담는 HTTP 헤더 이름입니다 (수신자가 dispatch하기 쉽도록).
const EventTypeHeader = "X-Rosshield-Event"

// DeliveryIDHeader는 WebhookDelivery.ID를 담는 헤더 이름입니다 (수신자 idempotency 키).
const DeliveryIDHeader = "X-Rosshield-Delivery"

// SignPayload는 secret으로 body의 HMAC-SHA256을 계산하고 헤더 값 형식으로 반환합니다.
//
// 결과 형식: "sha256=<hex>" — 수신자는 prefix를 분리한 후 hex.DecodeString으로 비교.
// secret이 빈 값이면 ErrEmptySecret.
func SignPayload(secret string, body []byte) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sum := mac.Sum(nil)
	return SignaturePrefix + hex.EncodeToString(sum), nil
}

// VerifySignature는 수신자 측 검증 보조 함수입니다 (수신자 SDK가 import해서 사용 가능).
//
// 헤더 값과 secret·body를 받아 일치 여부를 const-time 비교로 반환합니다.
// 입력 헤더가 prefix mismatch 또는 hex decode 실패면 false.
func VerifySignature(headerValue, secret string, body []byte) bool {
	if !strings.HasPrefix(headerValue, SignaturePrefix) {
		return false
	}
	expected, err := SignPayload(secret, body)
	if err != nil {
		return false
	}
	// const-time string 비교.
	return hmac.Equal([]byte(headerValue), []byte(expected))
}

// ValidateURL은 endpoint URL을 검증합니다.
//
// 요구: absolute + scheme=http|https + host 있음.
// 위반 시 ErrInvalidURL.
func ValidateURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return ErrInvalidURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if !u.IsAbs() {
		return fmt.Errorf("%w: must be absolute", ErrInvalidURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	return nil
}

// ValidateEvents는 endpoint Events 필드를 검증합니다.
//
// 빈 배열은 허용 — bootstrap이 KnownEventTypes 모두 구독으로 해석.
// 알 수 없는 EventType이 포함되면 ErrInvalidEvent.
func ValidateEvents(events []EventType) error {
	known := make(map[EventType]struct{}, len(KnownEventTypes))
	for _, e := range KnownEventTypes {
		known[e] = struct{}{}
	}
	for _, e := range events {
		if _, ok := known[e]; !ok {
			return fmt.Errorf("%w: %q", ErrInvalidEvent, e)
		}
	}
	return nil
}

// ValidateFormat은 endpoint Format 필드를 검증합니다.
//
// 빈 값은 PayloadFormatJSON으로 normalized — 호출자가 처리.
func ValidateFormat(f Format) error {
	switch f {
	case "", PayloadFormatJSON, PayloadFormatCEF, PayloadFormatECS:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, f)
	}
}

// EndpointSubscribesTo는 endpoint가 특정 EventType을 구독하는지 반환합니다.
//
// Events 빈 배열이면 모든 known event 구독 (default behavior).
func EndpointSubscribesTo(ep WebhookEndpoint, t EventType) bool {
	if len(ep.Events) == 0 {
		return true
	}
	for _, e := range ep.Events {
		if e == t {
			return true
		}
	}
	return false
}
