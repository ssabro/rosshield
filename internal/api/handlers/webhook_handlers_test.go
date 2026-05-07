package handlers_test

// webhook_handlers_test.go — E23-C 통합 테스트 (Webhook CRUD HTTP 표면).
//
// 시나리오:
//  1. CRUD 전 흐름 — Create → Get → List → Update → Delete + 사후 List 비어있음.
//  2. 401 인증 누락 (모든 webhook endpoint).
//  3. 400 — 잘못된 URL.
//  4. 400 — 빈 secret.
//  5. 400 — 알 수 없는 event type.
//  6. 404 — 미존재 endpoint Get/Update/Delete.
//  7. ListDeliveries — 신규 endpoint는 빈 배열.
//  8. tenant 격리 — 다른 tenant가 만든 endpoint는 보이지 않음.
//
// 본 테스트는 newFixture와 별도로 webhook 결선이 필요하므로 newWebhookFixture를
// 추가해 webhook svc까지 결선한 fixture를 사용합니다.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/api/handlers"
	"github.com/ssabro/rosshield/internal/app/advisorrun"
	advisorrepo "github.com/ssabro/rosshield/internal/domain/advisor/sqliterepo"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	compliancerepo "github.com/ssabro/rosshield/internal/domain/compliance/sqliterepo"
	insightrepo "github.com/ssabro/rosshield/internal/domain/insight/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	webhookrepo "github.com/ssabro/rosshield/internal/domain/integration/webhook/sqliterepo"
	reportingrepo "github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	robotrepo "github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	scanrepo "github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	eventbusinproc "github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	llmnoop "github.com/ssabro/rosshield/internal/platform/llm/noop"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// webhookFixture는 testFixture 위에 webhook svc를 결선한 확장 fixture입니다.
type webhookFixture struct {
	*testFixture
	webhook webhook.Service
}

// newWebhookFixture는 testFixture에 webhook svc를 추가하고 handler를 다시 결선합니다.
//
// newFixture()가 webhook을 결선하지 않으므로 별도 fixture가 필요합니다.
// admin은 동일 tenant·동일 자격으로 시드.
func newWebhookFixture(t *testing.T) *webhookFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")

	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	clk := clock.System()
	ids := idgen.NewULID()

	_, jwtPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		_ = store.Close()
		t.Fatalf("GenerateKey: %v", err)
	}
	jwtPub := jwtPriv.Public().(ed25519.PublicKey)

	tenantSvc := tenantrepo.New(tenantrepo.Deps{
		Clock:         clk,
		IDGen:         ids,
		Audit:         &nullAuditEmitter{},
		JWTPrivateKey: jwtPriv,
		JWTPublicKey:  jwtPub,
	})

	kekPath := filepath.Join(dir, "kek")
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i + 1)
	}
	if err := os.WriteFile(kekPath, kek, 0o600); err != nil {
		_ = store.Close()
		t.Fatalf("write KEK: %v", err)
	}
	robotKEK, err := robot.LoadOrCreateKEK(kekPath)
	if err != nil {
		_ = store.Close()
		t.Fatalf("LoadOrCreateKEK: %v", err)
	}

	robotSvc := robotrepo.New(robotrepo.Deps{
		Clock: clk, IDGen: ids, Audit: &nullAuditEmitter{}, KEK: robotKEK,
	})
	scanSvc := scanrepo.New(scanrepo.Deps{
		Clock: clk, IDGen: ids, Audit: &nullAuditEmitter{},
	})
	reportingSvc := reportingrepo.New(reportingrepo.Deps{
		Clock: clk, IDGen: ids, Audit: &nullAuditEmitter{}, Builder: &fakeBuilder{},
	})
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	insightSvc := insightrepo.New(insightrepo.Deps{
		Clock: clk, IDGen: ids, Audit: &nullAuditEmitter{},
		Scan: &testInsightScanAdapter{svc: scanSvc},
	})
	complianceSvc := compliancerepo.New(compliancerepo.Deps{
		Clock: clk, IDGen: ids, Audit: &nullAuditEmitter{},
		ScanReader:  &testComplianceScanAdapter{svc: scanSvc},
		AuditReader: &testComplianceAuditReaderAdapter{svc: auditSvc},
	})
	advisorRepoSvc := advisorrepo.New(advisorrepo.Deps{
		Clock: clk, IDGen: ids, Audit: &nullAuditEmitter{},
	})
	advisorDispatcher := advisorrun.NewDispatcher(scanSvc, nil, clk)
	advisorLLMClient := advisorrun.NewLLMClient(llmnoop.New())
	advisorSvc := advisorrun.NewOrchestrator(advisorrun.OrchestratorDeps{
		Repo: advisorRepoSvc, LLM: advisorLLMClient, Dispatcher: advisorDispatcher,
	})

	// E23 — webhook svc 결선.
	webhookSvc := webhookrepo.New(webhookrepo.Deps{Clock: clk, IDGen: ids})

	// admin 시드.
	const (
		email = "admin@example.com"
		pw    = "verylongpassword123"
	)
	var createResult tenant.CreateResult
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, e := tenantSvc.Create(ctx, tx, tenant.CreateRequest{
			Name:             "Test Tenant",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       email,
			AdminPassword:    pw,
			AdminDisplayName: "Test Admin",
		})
		if e != nil {
			return e
		}
		createResult = r
		return nil
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("seed admin: %v", err)
	}

	bus := eventbusinproc.New(eventbusinproc.Deps{
		Clock: clk, IDGen: ids,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})

	h := handlers.New(handlers.Deps{
		Storage:    store,
		Clock:      clk,
		Tenant:     tenantSvc,
		Robot:      robotSvc,
		Scan:       scanSvc,
		Reporting:  reportingSvc,
		Insight:    insightSvc,
		Compliance: complianceSvc,
		Advisor:    advisorSvc,
		Audit:      auditSvc,
		EventBus:   bus,
		Webhook:    webhookSvc,
	})

	router := chi.NewRouter()
	h.Mount(router)
	server := httptest.NewServer(router)

	tf := &testFixture{
		server:     server,
		storage:    store,
		tenant:     tenantSvc,
		robot:      robotSvc,
		scan:       scanSvc,
		auditSvc:   auditSvc,
		insight:    insightSvc,
		compliance: complianceSvc,
		bus:        bus,
		tenantID:   createResult.Tenant.ID,
		userID:     createResult.Admin.ID,
		email:      email,
		password:   pw,
		closeFn: func() {
			server.Close()
			ctxClose, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
			_ = bus.Close(ctxClose)
			cancelClose()
			_ = store.Close()
		},
	}
	return &webhookFixture{testFixture: tf, webhook: webhookSvc}
}

// === 1. CRUD 전 흐름 ===

func TestWebhookEndpointCreateGetListUpdateDelete(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	// Create.
	createBody, _ := json.Marshal(map[string]any{
		"url":     "https://siem.example.com/hook",
		"secret":  "shared-secret-1234",
		"events":  []string{"scan.completed"},
		"format":  "json",
		"enabled": true,
	})
	resp := f.doRequest(t, "POST", "/api/v1/webhooks", token, createBody)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create status=%d body=%s", resp.StatusCode, string(raw))
	}
	var created struct {
		ID          string   `json:"id"`
		URL         string   `json:"url"`
		SecretLast4 string   `json:"secretLast4"`
		Events      []string `json:"events"`
		Enabled     bool     `json:"enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode create: %v", err)
	}
	_ = resp.Body.Close()
	if created.ID == "" || len(created.ID) < 4 || created.ID[:3] != "wh_" {
		t.Errorf("id=%q, want prefix wh_", created.ID)
	}
	if created.SecretLast4 != "1234" {
		t.Errorf("secretLast4=%q, want 1234", created.SecretLast4)
	}
	if len(created.Events) != 1 || created.Events[0] != "scan.completed" {
		t.Errorf("events=%+v, want [scan.completed]", created.Events)
	}

	// Get.
	resp = f.doRequest(t, "GET", "/api/v1/webhooks/"+created.ID, token, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("get status=%d, want 200", resp.StatusCode)
	}
	var got struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	_ = resp.Body.Close()
	if got.ID != created.ID {
		t.Errorf("get id=%q, want %q", got.ID, created.ID)
	}

	// List — 1건.
	resp = f.doRequest(t, "GET", "/api/v1/webhooks", token, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("list status=%d, want 200", resp.StatusCode)
	}
	var listed struct {
		Endpoints []struct {
			ID string `json:"id"`
		} `json:"endpoints"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listed)
	_ = resp.Body.Close()
	if len(listed.Endpoints) != 1 || listed.Endpoints[0].ID != created.ID {
		t.Errorf("list=%+v, want 1 endpoint with id %q", listed.Endpoints, created.ID)
	}

	// Update — URL + format 변경.
	updateBody, _ := json.Marshal(map[string]any{
		"url":     "https://siem.example.com/hook-v2",
		"secret":  "shared-secret-WXYZ",
		"events":  []string{"scan.completed", "insight.created"},
		"format":  "cef",
		"enabled": false,
	})
	resp = f.doRequest(t, "PUT", "/api/v1/webhooks/"+created.ID, token, updateBody)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("update status=%d body=%s", resp.StatusCode, string(raw))
	}
	var updated struct {
		URL         string   `json:"url"`
		Format      string   `json:"format"`
		SecretLast4 string   `json:"secretLast4"`
		Events      []string `json:"events"`
		Enabled     bool     `json:"enabled"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&updated)
	_ = resp.Body.Close()
	if updated.URL != "https://siem.example.com/hook-v2" {
		t.Errorf("url=%q, want hook-v2", updated.URL)
	}
	if updated.Format != "cef" {
		t.Errorf("format=%q, want cef", updated.Format)
	}
	if updated.SecretLast4 != "WXYZ" {
		t.Errorf("secretLast4=%q, want WXYZ", updated.SecretLast4)
	}
	if len(updated.Events) != 2 {
		t.Errorf("events=%+v, want 2 events", updated.Events)
	}
	if updated.Enabled {
		t.Errorf("enabled=true, want false")
	}

	// Delete — 204.
	resp = f.doRequest(t, "DELETE", "/api/v1/webhooks/"+created.ID, token, nil)
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("delete status=%d body=%s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	// 사후 List — 빈 배열.
	resp = f.doRequest(t, "GET", "/api/v1/webhooks", token, nil)
	var afterList struct {
		Endpoints []any `json:"endpoints"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&afterList)
	_ = resp.Body.Close()
	if len(afterList.Endpoints) != 0 {
		t.Errorf("after delete list=%+v, want empty", afterList.Endpoints)
	}
}

// === 2. 401 인증 누락 ===

func TestWebhook401WithoutAuth(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()

	cases := []struct {
		method, path string
		body         []byte
	}{
		{"GET", "/api/v1/webhooks", nil},
		{"POST", "/api/v1/webhooks", []byte(`{}`)},
		{"GET", "/api/v1/webhooks/wh_X", nil},
		{"PUT", "/api/v1/webhooks/wh_X", []byte(`{}`)},
		{"DELETE", "/api/v1/webhooks/wh_X", nil},
		{"GET", "/api/v1/webhooks/wh_X/deliveries", nil},
	}
	for _, tc := range cases {
		resp := f.doRequest(t, tc.method, tc.path, "", tc.body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s status=%d, want 401", tc.method, tc.path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// === 3. 400 — 잘못된 URL ===

func TestWebhookCreate400ForInvalidURL(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"url":    "not-a-url",
		"secret": "shared-secret-1234",
	})
	resp := f.doRequest(t, "POST", "/api/v1/webhooks", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 400", resp.StatusCode, string(raw))
	}
}

// === 4. 400 — 빈 secret ===

func TestWebhookCreate400ForEmptySecret(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"url":    "https://siem.example.com/hook",
		"secret": "",
	})
	resp := f.doRequest(t, "POST", "/api/v1/webhooks", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

// === 5. 400 — 알 수 없는 event type ===

func TestWebhookCreate400ForUnknownEvent(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"url":    "https://siem.example.com/hook",
		"secret": "shared-secret-1234",
		"events": []string{"unknown.event"},
	})
	resp := f.doRequest(t, "POST", "/api/v1/webhooks", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

// === 6. 404 — 미존재 endpoint ===

func TestWebhookGet404ForUnknownID(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	resp := f.doRequest(t, "GET", "/api/v1/webhooks/wh_DOES_NOT_EXIST", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestWebhookUpdate404ForUnknownID(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"url":    "https://siem.example.com/hook",
		"secret": "shared-secret-1234",
	})
	resp := f.doRequest(t, "PUT", "/api/v1/webhooks/wh_DOES_NOT_EXIST", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestWebhookDelete404ForUnknownID(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	resp := f.doRequest(t, "DELETE", "/api/v1/webhooks/wh_DOES_NOT_EXIST", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

// === 7. ListDeliveries — 신규 endpoint는 빈 배열 + Enqueue 후 1건 ===

func TestWebhookListDeliveriesEmptyThenOne(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	// 먼저 endpoint 생성.
	createBody, _ := json.Marshal(map[string]any{
		"url":     "https://siem.example.com/hook",
		"secret":  "shared-secret-1234",
		"events":  []string{}, // empty → 모든 known event 구독
		"enabled": true,
	})
	resp := f.doRequest(t, "POST", "/api/v1/webhooks", token, createBody)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create status=%d body=%s", resp.StatusCode, string(raw))
	}
	var ep struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ep)
	_ = resp.Body.Close()

	// ListDeliveries — 빈 배열.
	resp = f.doRequest(t, "GET", "/api/v1/webhooks/"+ep.ID+"/deliveries", token, nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("list status=%d body=%s", resp.StatusCode, string(raw))
	}
	var ld struct {
		Deliveries []any `json:"deliveries"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ld)
	_ = resp.Body.Close()
	if len(ld.Deliveries) != 0 {
		t.Errorf("len(deliveries)=%d, want 0", len(ld.Deliveries))
	}

	// 도메인 service로 직접 Enqueue → delivery 1건 생성 후 list 재확인.
	ctx := storage.WithTenantID(context.Background(), f.tenantID)
	if err := f.storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := f.webhook.Enqueue(ctx, tx, webhook.DomainEvent{
			EventID:  "evt_test_1",
			TenantID: f.tenantID,
			Type:     webhook.EventScanCompleted,
			Payload:  []byte(`{"hello":"world"}`),
		})
		return e
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	resp = f.doRequest(t, "GET", "/api/v1/webhooks/"+ep.ID+"/deliveries", token, nil)
	defer func() { _ = resp.Body.Close() }()
	var ld2 struct {
		Deliveries []struct {
			ID            string `json:"id"`
			EventType     string `json:"eventType"`
			EventID       string `json:"eventId"`
			Succeeded     bool   `json:"succeeded"`
			AttemptCount  int    `json:"attemptCount"`
			PayloadBase64 string `json:"payloadBase64"`
		} `json:"deliveries"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ld2)
	if len(ld2.Deliveries) != 1 {
		t.Fatalf("len(deliveries)=%d, want 1", len(ld2.Deliveries))
	}
	d := ld2.Deliveries[0]
	if d.EventType != "scan.completed" || d.EventID != "evt_test_1" {
		t.Errorf("delivery=%+v", d)
	}
	if d.Succeeded {
		t.Errorf("delivery.succeeded=true; want false (not yet dispatched)")
	}
	if d.AttemptCount != 0 {
		t.Errorf("delivery.attemptCount=%d, want 0", d.AttemptCount)
	}
	if d.PayloadBase64 == "" {
		t.Errorf("payloadBase64 empty; want non-empty (base64 of {hello:world})")
	}
}

// === 8. tenant 격리 — 다른 tenant가 만든 endpoint는 보이지 않음 ===

func TestWebhookTenantIsolation(t *testing.T) {
	f := newWebhookFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	// admin tenant로 endpoint 1건 생성.
	createBody, _ := json.Marshal(map[string]any{
		"url":    "https://siem.example.com/hook",
		"secret": "shared-secret-1234",
	})
	resp := f.doRequest(t, "POST", "/api/v1/webhooks", token, createBody)
	if resp.StatusCode != http.StatusCreated {
		_ = resp.Body.Close()
		t.Fatalf("create status=%d", resp.StatusCode)
	}
	var ep struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ep)
	_ = resp.Body.Close()

	// 다른 tenant 시드 (Bootstrap 사용 — admin 자격 별도 발급).
	const (
		otherEmail = "other-admin@example.com"
		otherPw    = "verylongpassword999"
	)
	ctx := context.Background()
	var otherCreate tenant.CreateResult
	if err := f.storage.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, e := f.tenant.Create(ctx, tx, tenant.CreateRequest{
			Name:             "Other Tenant",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       otherEmail,
			AdminPassword:    otherPw,
			AdminDisplayName: "Other Admin",
		})
		if e != nil {
			return e
		}
		otherCreate = r
		return nil
	}); err != nil {
		t.Fatalf("seed other tenant: %v", err)
	}
	if otherCreate.Tenant.ID == f.tenantID {
		t.Fatalf("other tenant id collision with admin tenant")
	}

	// 다른 tenant로 login → token.
	loginBody, _ := json.Marshal(map[string]string{
		"email":    otherEmail,
		"password": otherPw,
	})
	loginResp, err := http.Post(f.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("other login: %v", err)
	}
	if loginResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(loginResp.Body)
		_ = loginResp.Body.Close()
		t.Fatalf("other login status=%d body=%s", loginResp.StatusCode, string(raw))
	}
	var lo struct {
		AccessToken string `json:"accessToken"`
	}
	_ = json.NewDecoder(loginResp.Body).Decode(&lo)
	_ = loginResp.Body.Close()
	if lo.AccessToken == "" {
		t.Fatalf("other login: empty accessToken")
	}

	// 다른 tenant가 List → 0건.
	resp = f.doRequest(t, "GET", "/api/v1/webhooks", lo.AccessToken, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("other list status=%d", resp.StatusCode)
	}
	var listed struct {
		Endpoints []any `json:"endpoints"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listed)
	_ = resp.Body.Close()
	if len(listed.Endpoints) != 0 {
		t.Errorf("other tenant list=%d, want 0", len(listed.Endpoints))
	}

	// 다른 tenant가 admin의 endpoint Get → 404.
	resp = f.doRequest(t, "GET", "/api/v1/webhooks/"+ep.ID, lo.AccessToken, nil)
	if resp.StatusCode != http.StatusNotFound {
		_ = resp.Body.Close()
		t.Fatalf("other get status=%d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 다른 tenant가 Delete → 404 (cross-tenant write 차단).
	resp = f.doRequest(t, "DELETE", "/api/v1/webhooks/"+ep.ID, lo.AccessToken, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other delete status=%d, want 404", resp.StatusCode)
	}
}

// === Mute lint warning ===

// _ 변수로 unused import 회피 (hex는 hooks/test 다른 곳에서 사용 가능성을 위해 silence).
var _ = hex.EncodeToString
