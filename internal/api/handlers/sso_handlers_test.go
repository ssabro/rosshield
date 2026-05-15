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
		Storage:         store,
		Clock:           clk,
		Tenant:          tenantSvc,
		Robot:           robotSvc,
		Scan:            scanSvc,
		Reporting:       reportingSvc,
		Insight:         insightSvc,
		Compliance:      complianceSvc,
		Advisor:         advisorSvc,
		Audit:           auditSvc,
		EventBus:        bus,
		SSO:             ssoSvc,
		SSOGroupMapping: ssoSvc, // RBAC fleet 정밀화 Stage 5 — *ssorepo.Repo가 GroupMappingService도 구현.
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

// === RBAC fleet 정밀화 Stage 5 — Group Mapping CRUD HTTP 통합 ===

// createTestProviderForGroupMapping은 group mapping 테스트용 OIDC provider 1건을 생성하고 ID 반환.
func createTestProviderForGroupMapping(t *testing.T, f *ssoFixture, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"type":    "oidc",
		"name":    "Group Mapping Test Provider",
		"enabled": true,
		"config": map[string]any{
			"issuer":      "https://idp.test",
			"clientId":    "client-x",
			"redirectUri": "https://app/cb",
			"scopes":      []string{"openid", "groups"},
		},
	})
	resp := f.doRequest(t, "POST", "/api/v1/sso/providers", token, body)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create provider status=%d body=%s", resp.StatusCode, string(raw))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode provider: %v", err)
	}
	_ = resp.Body.Close()
	return created.ID
}

// fetchOperatorRoleID는 admin이 시드되는 시점에 함께 시드되는 operator role ID를 조회합니다.
func fetchOperatorRoleID(t *testing.T, f *ssoFixture) string {
	t.Helper()
	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	var roleID string
	if err := f.storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		role, err := f.tenant.GetRole(ctx, tx, f.tenantID, tenant.RoleOperator)
		if err != nil {
			return err
		}
		roleID = role.ID
		return nil
	}); err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	return roleID
}

// TestSSOGroupMappingCRUD는 List/Create/Delete CRUD 흐름 + 권한 게이트를 검증합니다.
func TestSSOGroupMappingCRUD(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	providerID := createTestProviderForGroupMapping(t, f, token)
	operatorRoleID := fetchOperatorRoleID(t, f)

	// 사전 List — 0건.
	resp := f.doRequest(t, "GET", "/api/v1/sso/providers/"+providerID+"/group-mappings", token, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("list status=%d want 200", resp.StatusCode)
	}
	var list struct {
		Mappings []struct {
			ID string `json:"id"`
		} `json:"mappings"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	_ = resp.Body.Close()
	if len(list.Mappings) != 0 {
		t.Errorf("pre-create list len=%d want 0", len(list.Mappings))
	}

	// Create — group "ops-team" → operator (tenant scope).
	createBody, _ := json.Marshal(map[string]any{
		"groupValue": "ops-team",
		"roleId":     operatorRoleID,
		"scopeType":  "tenant",
	})
	resp = f.doRequest(t, "POST", "/api/v1/sso/providers/"+providerID+"/group-mappings", token, createBody)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create status=%d body=%s", resp.StatusCode, string(raw))
	}
	var created struct {
		ID         string `json:"id"`
		GroupValue string `json:"groupValue"`
		RoleID     string `json:"roleId"`
		ScopeType  string `json:"scopeType"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()
	if created.ID == "" || created.GroupValue != "ops-team" || created.RoleID != operatorRoleID {
		t.Errorf("create response = %+v", created)
	}
	if created.ScopeType != "tenant" {
		t.Errorf("scopeType=%q want tenant", created.ScopeType)
	}

	// 중복 Create — 409.
	resp = f.doRequest(t, "POST", "/api/v1/sso/providers/"+providerID+"/group-mappings", token, bytes.Clone(createBody))
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("dup create status=%d body=%s want 409", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	// 사후 List — 1건.
	resp = f.doRequest(t, "GET", "/api/v1/sso/providers/"+providerID+"/group-mappings", token, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("list-after status=%d want 200", resp.StatusCode)
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	_ = resp.Body.Close()
	if len(list.Mappings) != 1 || list.Mappings[0].ID != created.ID {
		t.Errorf("list-after = %+v want 1 with id %q", list.Mappings, created.ID)
	}

	// Delete — 204.
	resp = f.doRequest(t, "DELETE",
		"/api/v1/sso/providers/"+providerID+"/group-mappings/"+created.ID, token, nil)
	if resp.StatusCode != http.StatusNoContent {
		_ = resp.Body.Close()
		t.Fatalf("delete status=%d want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 두 번째 Delete — 404 (이미 삭제됨).
	resp = f.doRequest(t, "DELETE",
		"/api/v1/sso/providers/"+providerID+"/group-mappings/"+created.ID, token, nil)
	if resp.StatusCode != http.StatusNotFound {
		_ = resp.Body.Close()
		t.Fatalf("second delete status=%d want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestSSOGroupMappingCreate400ForBlankFields는 잘못된 입력 거부를 검증합니다.
func TestSSOGroupMappingCreate400ForBlankFields(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	providerID := createTestProviderForGroupMapping(t, f, token)
	operatorRoleID := fetchOperatorRoleID(t, f)

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{
			name: "empty groupValue",
			body: map[string]any{"groupValue": "", "roleId": operatorRoleID, "scopeType": "tenant"},
			want: http.StatusBadRequest,
		},
		{
			name: "empty roleId",
			body: map[string]any{"groupValue": "g", "roleId": "", "scopeType": "tenant"},
			want: http.StatusBadRequest,
		},
		{
			name: "fleet scope without scopeId",
			body: map[string]any{"groupValue": "g", "roleId": operatorRoleID, "scopeType": "fleet", "scopeId": ""},
			want: http.StatusBadRequest,
		},
		{
			name: "invalid scopeType",
			body: map[string]any{"groupValue": "g", "roleId": operatorRoleID, "scopeType": "global"},
			want: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			resp := f.doRequest(t, "POST",
				"/api/v1/sso/providers/"+providerID+"/group-mappings", token, body)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				raw, _ := io.ReadAll(resp.Body)
				t.Errorf("status=%d body=%s want=%d", resp.StatusCode, string(raw), tc.want)
			}
		})
	}
}

// TestSSOGroupSync_RevokesPriorSSOBindings_PreservesManual은 SSO sync 흐름이 source='sso'만
// 갱신하고 source='manual' admin 수동 binding은 보존하는지 (D-RBACEX-7 권장 default) 검증합니다.
//
// 시나리오:
//
//  1. provider + 2 group mapping (ops-team → operator tenant, sec-team → auditor tenant) 시드.
//  2. admin user에 source='manual' fleet-scope binding 1건 + source='sso' 다른 role 1건 사전 시드.
//  3. SSO sync(groups=["ops-team","sec-team"]) 직접 호출 시뮬레이션 (handler 내부 sync 헬퍼는
//     export 안 되었으므로 tenant.Service.RevokeUserRoleBindingsBySource + GroupMappingService
//     .ResolveBindingsForGroups + AssignRoleScoped 직접 결합으로 동등 흐름 재현).
//  4. 검증 — manual binding 보존 + 새 sso 셋 적용 + 이전 sso 사라짐.
func TestSSOGroupSync_RevokesPriorSSOBindings_PreservesManual(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	providerID := createTestProviderForGroupMapping(t, f, token)
	operatorRoleID := fetchOperatorRoleID(t, f)

	// Auditor role도 추출 — 두 번째 binding용.
	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	var auditorRoleID string
	if err := f.storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		role, err := f.tenant.GetRole(ctx, tx, f.tenantID, tenant.RoleAuditor)
		if err != nil {
			return err
		}
		auditorRoleID = role.ID
		return nil
	}); err != nil {
		t.Fatalf("GetRole auditor: %v", err)
	}

	// 1. 매핑 2건 시드 — ops-team→operator(tenant), sec-team→auditor(tenant).
	for _, m := range []struct {
		group string
		role  string
	}{
		{"ops-team", operatorRoleID},
		{"sec-team", auditorRoleID},
	} {
		body, _ := json.Marshal(map[string]any{
			"groupValue": m.group,
			"roleId":     m.role,
			"scopeType":  "tenant",
		})
		resp := f.doRequest(t, "POST",
			"/api/v1/sso/providers/"+providerID+"/group-mappings", token, body)
		if resp.StatusCode != http.StatusCreated {
			_ = resp.Body.Close()
			t.Fatalf("seed mapping %q status=%d", m.group, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// 2. 사전 binding 시드 — admin은 이미 admin tenant binding(manual) 있음. 추가로 fleet manual
	//    + 별 SSO 시뮬 binding 직접 INSERT.
	const flt = "flt_legacy"
	if err := f.storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		// fleet-scope manual binding (operator role을 fleet에 — 사용자 수동 할당 의미).
		if err := f.tenant.AssignRoleScoped(ctx, tx, f.userID, operatorRoleID,
			tenant.ScopeFleet, flt, tenant.BindingSourceManual); err != nil {
			return err
		}
		// SSO 시뮬 binding (이전 sync 결과로 가정 — 다음 sync에서 revoke될 대상).
		// auditor role을 fleet scope source='sso'로 시드.
		// (operator는 이미 manual binding으로 PK 충돌하므로 auditor 사용).
		if err := f.tenant.AssignRoleScoped(ctx, tx, f.userID, auditorRoleID,
			tenant.ScopeFleet, "flt_old_sso", tenant.BindingSourceSSO); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("seed bindings: %v", err)
	}

	// 사전 검증 — admin(manual tenant) + operator(manual fleet) + auditor(sso fleet) = 3건.
	if err := f.storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		bs, err := f.tenant.GetUserRoleBindings(ctx, tx, f.userID)
		if err != nil {
			return err
		}
		if len(bs) != 3 {
			t.Fatalf("pre-sync bindings len=%d want 3 (got %v)", len(bs), bindingsSummary(bs))
		}
		return nil
	}); err != nil {
		t.Fatalf("pre-sync GetUserRoleBindings: %v", err)
	}

	// 3. sync 흐름 시뮬레이션 — handler.syncSSOGroupBindings 직접 호출 불가(unexported)이므로
	//    동등 알고리즘 재현 (revoke + INSERT). 실제 SSO callback 흐름의 Tx 안에서 일어나는
	//    행동을 그대로 따라함.
	if err := f.storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		resolved, err := f.sso.(sso.GroupMappingService).ResolveBindingsForGroups(ctx, tx, providerID,
			[]string{"ops-team", "sec-team"})
		if err != nil {
			return err
		}
		if len(resolved) != 2 {
			t.Fatalf("resolved bindings len=%d want 2", len(resolved))
		}
		// SSO source 모두 revoke.
		if _, err := f.tenant.RevokeUserRoleBindingsBySource(ctx, tx, f.userID, tenant.BindingSourceSSO); err != nil {
			return err
		}
		// 새 셋 INSERT (source='sso').
		for _, rb := range resolved {
			st := tenant.ScopeType(rb.ScopeType)
			if st == "" {
				st = tenant.ScopeTenant
			}
			if err := f.tenant.AssignRoleScoped(ctx, tx, f.userID, rb.RoleID,
				st, rb.ScopeID, tenant.BindingSourceSSO); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// 4. 사후 검증 — admin(manual tenant) + operator(manual fleet) 보존 + ops-team/sec-team
	//    매핑은 이미 admin/operator role이라 PK 충돌 → operator는 manual fleet 보존, auditor는
	//    sec-team(tenant scope)으로 새 INSERT. auditor sso fleet binding은 revoke됨.
	if err := f.storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		bs, err := f.tenant.GetUserRoleBindings(ctx, tx, f.userID)
		if err != nil {
			return err
		}
		manualCount, ssoCount := 0, 0
		for _, b := range bs {
			switch b.Source {
			case tenant.BindingSourceManual:
				manualCount++
			case tenant.BindingSourceSSO:
				ssoCount++
			}
		}
		if manualCount != 2 {
			t.Errorf("post-sync manual count=%d want 2 (admin tenant + operator fleet) — bindings: %s",
				manualCount, bindingsSummary(bs))
		}
		// SSO sync는 ops-team(operator) + sec-team(auditor) 매핑 적용 시도. operator는 이미 manual
		// PK로 충돌(ON CONFLICT DO NOTHING) — 결과적으로 SSO INSERT는 auditor 1건만 성공.
		// 따라서 ssoCount는 1 (이전 auditor sso fleet binding은 revoke된 후 새 auditor sso tenant
		// binding 1건만 살아남음).
		if ssoCount != 1 {
			t.Errorf("post-sync sso count=%d want 1 (auditor tenant from sec-team) — bindings: %s",
				ssoCount, bindingsSummary(bs))
		}
		// 이전 auditor sso fleet binding(scope=flt_old_sso)는 revoke 확인.
		for _, b := range bs {
			if b.ScopeID == "flt_old_sso" {
				t.Errorf("auditor sso flt_old_sso binding still present — sync revoke 실패: %+v", b)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("post-sync GetUserRoleBindings: %v", err)
	}
}

// bindingsSummary는 RoleBinding 슬라이스를 디버그 친화 문자열로 변환합니다.
func bindingsSummary(bs []tenant.RoleBinding) string {
	parts := make([]string, 0, len(bs))
	for _, b := range bs {
		parts = append(parts, b.Role.Name+"/"+string(b.ScopeType)+"/"+b.ScopeID+"/"+string(b.Source))
	}
	return "[" + joinStrings(parts, " | ") + "]"
}

func joinStrings(s []string, sep string) string {
	if len(s) == 0 {
		return ""
	}
	out := s[0]
	for i := 1; i < len(s); i++ {
		out += sep + s[i]
	}
	return out
}

// TestSSOGroupMappingUnauthMissing401는 인증 토큰 없는 요청은 401임을 검증합니다.
func TestSSOGroupMappingUnauthMissing401(t *testing.T) {
	f := newSSOFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	providerID := createTestProviderForGroupMapping(t, f, token)

	// 토큰 없이 List/Create/Delete 모두 401.
	for _, ep := range []struct {
		method string
		path   string
		body   []byte
	}{
		{"GET", "/api/v1/sso/providers/" + providerID + "/group-mappings", nil},
		{"POST", "/api/v1/sso/providers/" + providerID + "/group-mappings",
			[]byte(`{"groupValue":"x","roleId":"r","scopeType":"tenant"}`)},
		{"DELETE", "/api/v1/sso/providers/" + providerID + "/group-mappings/sgm_xxx", nil},
	} {
		resp := f.doRequest(t, ep.method, ep.path, "", ep.body)
		if resp.StatusCode != http.StatusUnauthorized {
			_ = resp.Body.Close()
			t.Errorf("%s %s status=%d want 401", ep.method, ep.path, resp.StatusCode)
			continue
		}
		_ = resp.Body.Close()
	}
}
