package webhook_test

// webhook_test.go — E23 webhook 도메인 순수 단위 테스트.
//
// HMAC 서명·재시도 backoff·CEF 형식·ECS 형식·검증 helper를 검증합니다.
// sqliterepo 통합 테스트는 sqliterepo/repo_test.go 참조.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
)

// E23.T1 — TestWebhookSignedWithHMAC.
//
// SignPayload는 "sha256=<hex>" 형식이어야 하고, 같은 secret·body는 항상 같은 결과,
// secret 다르면 결과가 다르며, 빈 secret은 ErrEmptySecret.
func TestSignPayloadHMACSHA256Format(t *testing.T) {
	t.Parallel()

	body := []byte(`{"event":"scan.completed","scan_id":"ss_X"}`)

	sig, err := webhook.SignPayload("topsecret", body)
	if err != nil {
		t.Fatalf("SignPayload: %v", err)
	}
	if !strings.HasPrefix(sig, webhook.SignaturePrefix) {
		t.Errorf("sig prefix = %q, want %q", sig, webhook.SignaturePrefix)
	}
	// sha256 hex는 64자 — prefix("sha256=") 7자 제외하면 64자.
	hexPart := strings.TrimPrefix(sig, webhook.SignaturePrefix)
	if len(hexPart) != 64 {
		t.Errorf("hex length = %d, want 64", len(hexPart))
	}

	// 같은 입력은 같은 결과 (deterministic).
	sig2, _ := webhook.SignPayload("topsecret", body)
	if sig != sig2 {
		t.Errorf("not deterministic: %q vs %q", sig, sig2)
	}

	// 다른 secret은 다른 결과.
	sigOther, _ := webhook.SignPayload("different", body)
	if sig == sigOther {
		t.Errorf("different secret produced same signature")
	}

	// 빈 secret → ErrEmptySecret.
	_, err = webhook.SignPayload("", body)
	if !errors.Is(err, webhook.ErrEmptySecret) {
		t.Errorf("empty secret err = %v, want ErrEmptySecret", err)
	}
}

// VerifySignature는 SignPayload의 결과를 받아 const-time 비교로 일치 여부 반환.
func TestVerifySignatureRoundTrip(t *testing.T) {
	t.Parallel()

	body := []byte("hello rosshield")
	secret := "shared-key"

	sig, _ := webhook.SignPayload(secret, body)
	if !webhook.VerifySignature(sig, secret, body) {
		t.Errorf("verify roundtrip failed")
	}
	// 다른 secret은 fail.
	if webhook.VerifySignature(sig, "wrong", body) {
		t.Errorf("verify with wrong secret should fail")
	}
	// 다른 body는 fail.
	if webhook.VerifySignature(sig, secret, []byte("tampered")) {
		t.Errorf("verify with tampered body should fail")
	}
	// prefix 없는 헤더는 fail.
	if webhook.VerifySignature("nopref-abc", secret, body) {
		t.Errorf("verify without sha256= prefix should fail")
	}
}

// E23.T2 — TestRetryWithExponentialBackoff.
//
// retryDelays = 1m, 5m, 15m, 1h, 24h. attemptCount 1~5만 valid, 그 외는 ok=false.
func TestNextRetryDelayPolicy(t *testing.T) {
	t.Parallel()

	expected := []struct {
		attempt int
		delay   time.Duration
	}{
		{1, 1 * time.Minute},
		{2, 5 * time.Minute},
		{3, 15 * time.Minute},
		{4, 1 * time.Hour},
		{5, 24 * time.Hour},
	}
	for _, c := range expected {
		got, ok := webhook.NextRetryDelay(c.attempt)
		if !ok {
			t.Errorf("attempt=%d: ok=false, want true", c.attempt)
		}
		if got != c.delay {
			t.Errorf("attempt=%d: delay = %v, want %v", c.attempt, got, c.delay)
		}
	}

	// 0 → 무효.
	if _, ok := webhook.NextRetryDelay(0); ok {
		t.Errorf("attempt=0: ok=true, want false")
	}
	// 6 → dead-letter.
	if _, ok := webhook.NextRetryDelay(6); ok {
		t.Errorf("attempt=6: ok=true, want false (dead-letter)")
	}
	if webhook.MaxRetryAttempts != 5 {
		t.Errorf("MaxRetryAttempts = %d, want 5", webhook.MaxRetryAttempts)
	}
}

// E23.T3 — TestEventTypeFilterByTenantConfig.
//
// EndpointSubscribesTo는 Events 빈 배열이면 모든 known event 구독,
// 채워진 배열이면 정확히 일치하는 EventType만 매치.
func TestEndpointSubscribesTo(t *testing.T) {
	t.Parallel()

	// 빈 Events → 모든 known event 구독.
	epAll := webhook.WebhookEndpoint{Events: nil}
	for _, e := range webhook.KnownEventTypes {
		if !webhook.EndpointSubscribesTo(epAll, e) {
			t.Errorf("empty Events should subscribe to %q", e)
		}
	}

	// 단일 필터 — scan.completed만.
	epScanOnly := webhook.WebhookEndpoint{
		Events: []webhook.EventType{webhook.EventScanCompleted},
	}
	if !webhook.EndpointSubscribesTo(epScanOnly, webhook.EventScanCompleted) {
		t.Errorf("scan-only should match scan.completed")
	}
	if webhook.EndpointSubscribesTo(epScanOnly, webhook.EventInsightCreated) {
		t.Errorf("scan-only should NOT match insight.created")
	}
	if webhook.EndpointSubscribesTo(epScanOnly, webhook.EventAuditCheckpoint) {
		t.Errorf("scan-only should NOT match audit.checkpoint")
	}
}

// E23.T4 — TestCEFFormatPassesSplunkSanityScan.
//
// CEF는 "CEF:0|<vendor>|<product>|<version>|<sigID>|<name>|<sev>|<ext>" 단일 라인.
// rosshield vendor·product 필수, severity 매핑, key=value extension 포함.
func TestFormatCEFContainsRequiredFields(t *testing.T) {
	t.Parallel()

	evt := webhook.DomainEvent{
		EventID:       "evt_01H8X",
		TenantID:      "tn_acme",
		Type:          webhook.EventInsightCreated,
		OccurredAt:    time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Severity:      "high",
		AggregateType: "Insight",
		AggregateID:   "ins_01H8Y",
	}

	cef := webhook.FormatCEF(evt)

	// 단일 라인.
	if strings.Contains(cef, "\n") {
		t.Errorf("CEF should be single line, got %q", cef)
	}
	// 필수 prefix.
	if !strings.HasPrefix(cef, "CEF:0|") {
		t.Errorf("CEF should start with CEF:0|, got %q", cef)
	}
	// vendor/product = rosshield.
	if !strings.Contains(cef, "|rosshield|rosshield|") {
		t.Errorf("CEF missing vendor/product rosshield: %q", cef)
	}
	// signatureID = event type.
	if !strings.Contains(cef, "|insight.created|") {
		t.Errorf("CEF missing signatureID insight.created: %q", cef)
	}
	// severity high → 7.
	if !strings.Contains(cef, "|7|") {
		t.Errorf("CEF severity high should map to 7: %q", cef)
	}
	// extension key=value.
	if !strings.Contains(cef, "tenant=tn_acme") {
		t.Errorf("CEF missing tenant ext: %q", cef)
	}
	if !strings.Contains(cef, "event_id=evt_01H8X") {
		t.Errorf("CEF missing event_id ext: %q", cef)
	}
	if !strings.Contains(cef, "aggregate_type=Insight") {
		t.Errorf("CEF missing aggregate_type ext: %q", cef)
	}
}

// CEF severity mapping — info=2, low=3, medium=5, high=7, critical=10.
func TestFormatCEFSeverityMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sev  string
		want string // "|<n>|" snippet
	}{
		{"info", "|2|"},
		{"low", "|3|"},
		{"medium", "|5|"},
		{"high", "|7|"},
		{"critical", "|10|"},
		{"", "|5|"}, // default medium.
	}
	for _, c := range cases {
		evt := webhook.DomainEvent{
			Type:       webhook.EventInsightCreated,
			Severity:   c.sev,
			OccurredAt: time.Now(),
		}
		cef := webhook.FormatCEF(evt)
		if !strings.Contains(cef, c.want) {
			t.Errorf("sev=%q: CEF missing %q in %q", c.sev, c.want, cef)
		}
	}
}

// CEF escape — pipe·equals·backslash가 backslash-prefix.
func TestFormatCEFEscapesSpecialChars(t *testing.T) {
	t.Parallel()

	evt := webhook.DomainEvent{
		EventID:       "evt|with|pipes",
		TenantID:      "tn=acme",
		Type:          webhook.EventScanCompleted,
		OccurredAt:    time.Now(),
		AggregateType: `path\with\back`,
	}
	cef := webhook.FormatCEF(evt)
	if !strings.Contains(cef, `event_id=evt\|with\|pipes`) {
		t.Errorf("pipe not escaped: %q", cef)
	}
	if !strings.Contains(cef, `tenant=tn\=acme`) {
		t.Errorf("equals not escaped: %q", cef)
	}
	if !strings.Contains(cef, `aggregate_type=path\\with\\back`) {
		t.Errorf("backslash not escaped: %q", cef)
	}
}

// ECS — JSON 호환, 핵심 ECS 필드 포함.
func TestFormatECSStructure(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{"scan_id":"ss_X","outcome":"pass"}`)
	evt := webhook.DomainEvent{
		EventID:       "evt_01",
		TenantID:      "tn_acme",
		Type:          webhook.EventScanCompleted,
		OccurredAt:    time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Severity:      "info",
		AggregateType: "ScanSession",
		AggregateID:   "ss_X",
		Payload:       payload,
	}

	ecs := webhook.FormatECS(evt)

	// JSON 직렬화 가능해야 함.
	if _, err := json.Marshal(ecs); err != nil {
		t.Fatalf("ECS not JSON serializable: %v", err)
	}
	// @timestamp.
	ts, ok := ecs["@timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("@timestamp missing")
	}
	// event.dataset = rosshield.<event.type>.
	event, _ := ecs["event"].(map[string]any)
	if event == nil {
		t.Fatalf("event field missing")
	}
	if event["dataset"] != "rosshield.scan.completed" {
		t.Errorf("event.dataset = %v, want rosshield.scan.completed", event["dataset"])
	}
	if event["action"] != "scan.completed" {
		t.Errorf("event.action = %v, want scan.completed", event["action"])
	}
	// severity info → 0.
	if event["severity"] != 0 {
		t.Errorf("event.severity = %v, want 0", event["severity"])
	}
	// organization.id = tenant.
	org, _ := ecs["organization"].(map[string]any)
	if org["id"] != "tn_acme" {
		t.Errorf("organization.id = %v, want tn_acme", org["id"])
	}
	// rosshield.payload nested.
	ros, _ := ecs["rosshield"].(map[string]any)
	if ros == nil {
		t.Fatalf("rosshield field missing")
	}
	pay, _ := ros["payload"].(map[string]any)
	if pay["scan_id"] != "ss_X" {
		t.Errorf("rosshield.payload.scan_id = %v, want ss_X", pay["scan_id"])
	}
}

// ECS severity 매핑 — info=0, low=1, medium=2, high/critical=3.
func TestFormatECSSeverityMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sev  string
		want int
	}{
		{"info", 0},
		{"low", 1},
		{"medium", 2},
		{"high", 3},
		{"critical", 3},
		{"", 2}, // default medium.
	}
	for _, c := range cases {
		evt := webhook.DomainEvent{
			Type:       webhook.EventScanCompleted,
			Severity:   c.sev,
			OccurredAt: time.Now(),
		}
		ecs := webhook.FormatECS(evt)
		event, _ := ecs["event"].(map[string]any)
		if event["severity"] != c.want {
			t.Errorf("sev=%q: event.severity = %v, want %d", c.sev, event["severity"], c.want)
		}
	}
}

// ValidateURL — absolute http/https만 허용.
func TestValidateURL(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://siem.example.com/webhook",
		"http://localhost:8080/in",
		"https://example.com:443/path?q=1",
	}
	for _, u := range valid {
		if err := webhook.ValidateURL(u); err != nil {
			t.Errorf("valid %q failed: %v", u, err)
		}
	}

	invalid := []string{
		"",
		"ftp://example.com",
		"not-a-url",
		"/relative/path",
		"javascript:alert(1)",
	}
	for _, u := range invalid {
		if err := webhook.ValidateURL(u); !errors.Is(err, webhook.ErrInvalidURL) {
			t.Errorf("invalid %q: err = %v, want ErrInvalidURL", u, err)
		}
	}
}

// ValidateEvents — known만 허용, 빈 배열 OK, unknown은 ErrInvalidEvent.
func TestValidateEvents(t *testing.T) {
	t.Parallel()

	if err := webhook.ValidateEvents(nil); err != nil {
		t.Errorf("nil events: %v", err)
	}
	if err := webhook.ValidateEvents([]webhook.EventType{webhook.EventScanCompleted}); err != nil {
		t.Errorf("known event: %v", err)
	}
	if err := webhook.ValidateEvents([]webhook.EventType{"unknown.thing"}); !errors.Is(err, webhook.ErrInvalidEvent) {
		t.Errorf("unknown event: err = %v, want ErrInvalidEvent", err)
	}
}

// ValidateFormat — 빈 값·json·cef·ecs OK, 그 외 ErrUnknownFormat.
func TestValidateFormat(t *testing.T) {
	t.Parallel()

	for _, f := range []webhook.Format{"", webhook.PayloadFormatJSON, webhook.PayloadFormatCEF, webhook.PayloadFormatECS} {
		if err := webhook.ValidateFormat(f); err != nil {
			t.Errorf("valid %q: %v", f, err)
		}
	}
	if err := webhook.ValidateFormat("xml"); !errors.Is(err, webhook.ErrUnknownFormat) {
		t.Errorf("xml: err = %v, want ErrUnknownFormat", err)
	}
}
