package handlers_test

// sso_handlers_test.go — E20-D 통합 테스트 (SSO Provider CRUD HTTP 표면).
//
// 시나리오:
//  1. CRUD 전 흐름 — Create → Get → List → Update → Delete + 사후 List 비어있음.
//  2. 401 인증 누락 (모든 sso provider endpoint).
//  3. 400 — 잘못된 type.
//  4. 400 — 빈 name.
//  5. 400 — 빈 config.
//  6. 404 — 미존재 provider Get/Update/Delete.
//  7. 409 — 같은 (tenant, name) 중복 Create.
//  8. tenant 격리 — 다른 tenant의 provider는 보이지 않음.

import (
	"bytes"
	"context"
	"crypto/ed25519"
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
	reportingrepo "github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	robotrepo "github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	scanrepo "github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	ssorepo "github.com/ssabro/rosshield/internal/domain/tenant/sso/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	eventbusinproc "github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	llmnoop "github.com/ssabro/rosshield/internal/platform/llm/noop"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// ssoFixture는 testFixture 위에 sso svc를 결선한 확장 fixture입니다.
type ssoFixture struct {
	*testFixture
	sso sso.Service
}

// newSSOFixture는 testFixture에 sso svc를 추가하고 handler를 다시 결선합니다.
//
// webhookFixture와 동일 패턴 — newFixture가 sso를 결선하지 않으므로 별도 fixture.
func newSSOFixture(t *testing.T) *ssoFixture {
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

	// E20-D — sso svc 결선 (OIDC client는 nil — Provider CRUD만 검증).
	ssoSvc := ssorepo.New(ssorepo.Deps{Clock: clk, IDGen: ids, Audit: &nullSSOAudit{}})

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
		SSO:        ssoSvc,
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
	return &ssoFixture{testFixture: tf, sso: ssoSvc}
}

// nullSSOAudit는 sso.AuditEmitter no-op 구현입니다.
type nullSSOAudit struct{}

func (nullSSOAudit) EmitProviderChanged(ctx context.Context, tx storage.Tx, p sso.Provider, action string) error {
	return nil
}
func (nullSSOAudit) EmitLoginStarted(ctx context.Context, tx storage.Tx, a sso.LoginAttempt) error {
	return nil
}
func (nullSSOAudit) EmitLoginCompleted(ctx context.Context, tx storage.Tx, a sso.LoginAttempt, identity sso.ExternalIdentity, ok bool) error {
	return nil
}

// === 1. CRUD 전 흐름 ===

func TestSSOProviderCreateGetListUpdateDelete(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	createBody, _ := json.Marshal(map[string]any{
		"type":    "oidc",
		"name":    "Google Workspace",
		"enabled": true,
		"config": map[string]any{
			"issuer":      "https://accounts.google.com",
			"clientId":    "abc.apps.googleusercontent.com",
			"redirectUri": "https://app/callback",
			"scopes":      []string{"openid", "email", "profile"},
		},
	})
	resp := f.doRequest(t, "POST", "/api/v1/sso/providers", token, createBody)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create status=%d body=%s", resp.StatusCode, string(raw))
	}
	var created struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode create: %v", err)
	}
	_ = resp.Body.Close()
	if created.ID == "" || len(created.ID) < 5 || created.ID[:5] != "ssop_" {
		t.Errorf("id=%q, want prefix ssop_", created.ID)
	}
	if created.Type != "oidc" {
		t.Errorf("type=%q, want oidc", created.Type)
	}

	// Get.
	resp = f.doRequest(t, "GET", "/api/v1/sso/providers/"+created.ID, token, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("get status=%d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// List — 1건.
	resp = f.doRequest(t, "GET", "/api/v1/sso/providers", token, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("list status=%d, want 200", resp.StatusCode)
	}
	var listed struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listed)
	_ = resp.Body.Close()
	if len(listed.Providers) != 1 || listed.Providers[0].ID != created.ID {
		t.Errorf("list=%+v, want 1 provider with id %q", listed.Providers, created.ID)
	}

	// Update — name + enabled 변경.
	updateBody, _ := json.Marshal(map[string]any{
		"name":    "Google (renamed)",
		"enabled": false,
	})
	resp = f.doRequest(t, "PUT", "/api/v1/sso/providers/"+created.ID, token, updateBody)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("update status=%d body=%s", resp.StatusCode, string(raw))
	}
	var updated struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&updated)
	_ = resp.Body.Close()
	if updated.Name != "Google (renamed)" {
		t.Errorf("name=%q, want renamed", updated.Name)
	}
	if updated.Enabled {
		t.Errorf("enabled=true, want false")
	}

	// Delete.
	resp = f.doRequest(t, "DELETE", "/api/v1/sso/providers/"+created.ID, token, nil)
	if resp.StatusCode != http.StatusNoContent {
		_ = resp.Body.Close()
		t.Fatalf("delete status=%d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 사후 List — 빈.
	resp = f.doRequest(t, "GET", "/api/v1/sso/providers", token, nil)
	var afterList struct {
		Providers []any `json:"providers"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&afterList)
	_ = resp.Body.Close()
	if len(afterList.Providers) != 0 {
		t.Errorf("after delete list=%+v, want empty", afterList.Providers)
	}
}

// === 2. 401 인증 누락 ===

func TestSSOProvider401WithoutAuth(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()

	cases := []struct {
		method, path string
		body         []byte
	}{
		{"GET", "/api/v1/sso/providers", nil},
		{"POST", "/api/v1/sso/providers", []byte(`{}`)},
		{"GET", "/api/v1/sso/providers/ssop_X", nil},
		{"PUT", "/api/v1/sso/providers/ssop_X", []byte(`{}`)},
		{"DELETE", "/api/v1/sso/providers/ssop_X", nil},
	}
	for _, tc := range cases {
		resp := f.doRequest(t, tc.method, tc.path, "", tc.body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s status=%d, want 401", tc.method, tc.path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// === 3. 400 — 잘못된 type ===

func TestSSOProviderCreate400ForInvalidType(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"type":    "ldap", // 미지원
		"name":    "Bad",
		"enabled": true,
		"config":  json.RawMessage(`{}`),
	})
	resp := f.doRequest(t, "POST", "/api/v1/sso/providers", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

// === 4. 400 — 빈 name ===

func TestSSOProviderCreate400ForEmptyName(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"type":    "oidc",
		"name":    "",
		"enabled": true,
		"config":  json.RawMessage(`{"issuer":"https://x"}`),
	})
	resp := f.doRequest(t, "POST", "/api/v1/sso/providers", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

// === 5. 404 — 미존재 provider ===

func TestSSOProvider404ForUnknownID(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/sso/providers/ssop_unknown"},
		{"PUT", "/api/v1/sso/providers/ssop_unknown"},
		{"DELETE", "/api/v1/sso/providers/ssop_unknown"},
	} {
		var body []byte
		if tc.method == "PUT" {
			body, _ = json.Marshal(map[string]any{"name": "x"})
		}
		resp := f.doRequest(t, tc.method, tc.path, token, body)
		if resp.StatusCode != http.StatusNotFound {
			raw, _ := io.ReadAll(resp.Body)
			t.Errorf("%s %s status=%d body=%s, want 404", tc.method, tc.path, resp.StatusCode, string(raw))
		}
		_ = resp.Body.Close()
	}
}

// === 6. 409 — 중복 name ===

func TestSSOProviderCreate409ForDuplicateName(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"type":    "oidc",
		"name":    "Dup",
		"enabled": true,
		"config":  json.RawMessage(`{"issuer":"https://x"}`),
	})
	resp := f.doRequest(t, "POST", "/api/v1/sso/providers", token, bytes.Clone(body))
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("first create status=%d body=%s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	resp = f.doRequest(t, "POST", "/api/v1/sso/providers", token, bytes.Clone(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("dup status=%d body=%s, want 409", resp.StatusCode, string(raw))
	}
}
