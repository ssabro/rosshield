package handlers_test

// handlers_test.go — E9 Stage B 통합 테스트.
//
// 시나리오: SQLite + 도메인 서비스 결선 → tenant.Service.Create로 admin 시드 →
// httptest.Server에 chi router mount → http.Client로 5개 endpoint 회귀 검증.
//
// 핸들러 패키지 외부에서 black-box 테스트 (handlers_test 패키지) — 공개 API만 사용.

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
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/api/handlers"
	"github.com/ssabro/rosshield/internal/app/advisorrun"
	"github.com/ssabro/rosshield/internal/domain/advisor"
	advisorrepo "github.com/ssabro/rosshield/internal/domain/advisor/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/compliance"
	compliancerepo "github.com/ssabro/rosshield/internal/domain/compliance/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/insight"
	insightrepo "github.com/ssabro/rosshield/internal/domain/insight/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	reportingrepo "github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	robotrepo "github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/scan"
	scanrepo "github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	eventbusinproc "github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	llmnoop "github.com/ssabro/rosshield/internal/platform/llm/noop"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === 테스트 harness ===

// testFixture는 한 테스트의 모든 결선 자원입니다.
type testFixture struct {
	server     *httptest.Server
	storage    storage.Storage
	tenant     tenant.Service
	robot      robot.Service
	scan       scan.Service
	auditSvc   audit.Service
	insight    insight.Service    // E17 Phase 2
	compliance compliance.Service // E17 Phase 2
	bus        eventbus.Bus       // C1 — WebSocket scan progress 테스트용
	tenantID   storage.TenantID
	userID     string
	email      string
	password   string
	closeFn    func()
}

// newFixture는 SQLite + 도메인 서비스 + handlers + httptest.Server를 결선합니다.
func newFixture(t *testing.T) *testFixture {
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

	// JWT key — 테스트 격리를 위해 매번 새로.
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

	// KEK 32B 임시 파일 (0600).
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
		Clock: clk,
		IDGen: ids,
		Audit: &nullAuditEmitter{},
		KEK:   robotKEK,
	})

	scanSvc := scanrepo.New(scanrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: &nullAuditEmitter{},
	})

	reportingSvc := reportingrepo.New(reportingrepo.Deps{
		Clock:   clk,
		IDGen:   ids,
		Audit:   &nullAuditEmitter{},
		Builder: &fakeBuilder{},
	})

	// E17 — audit 실서비스 + insight/compliance 결선.
	// audit는 compliance.AuditReader.Head 호출에 실 결과 필요 (chain head anchor).
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	insightSvc := insightrepo.New(insightrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: &nullAuditEmitter{},
		Scan:  &testInsightScanAdapter{svc: scanSvc},
	})
	complianceSvc := compliancerepo.New(compliancerepo.Deps{
		Clock:       clk,
		IDGen:       ids,
		Audit:       &nullAuditEmitter{},
		ScanReader:  &testComplianceScanAdapter{svc: scanSvc},
		AuditReader: &testComplianceAuditReaderAdapter{svc: auditSvc},
	})

	// E16/E19-3 — Advisor 결선 (옵트인 — noop LLM이라 모든 Ask는 ErrAdvisorDisabled).
	advisorRepoSvc := advisorrepo.New(advisorrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: &nullAuditEmitter{},
	})
	// dispatcher는 LLM이 noop이라 호출되지 않음 — evidence는 nil 허용.
	advisorDispatcher := advisorrun.NewDispatcher(scanSvc, nil, clk)
	advisorLLMClient := advisorrun.NewLLMClient(llmnoop.New())
	advisorSvc := advisorrun.NewOrchestrator(advisorrun.OrchestratorDeps{
		Repo:       advisorRepoSvc,
		LLM:        advisorLLMClient,
		Dispatcher: advisorDispatcher,
	})

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

	// EventBus — C1 WebSocket 테스트용 인proc.
	bus := eventbusinproc.New(eventbusinproc.Deps{
		Clock:  clk,
		IDGen:  ids,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})

	// handlers 결선.
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
		EventBus:   bus,
	})

	router := chi.NewRouter()
	h.Mount(router)

	server := httptest.NewServer(router)

	return &testFixture{
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
}

// loginAndGetToken은 admin 자격으로 login → accessToken 반환.
func (f *testFixture) loginAndGetToken(t *testing.T) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email":    f.email,
		"password": f.password,
	})
	resp, err := http.Post(f.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("login decode: %v", err)
	}
	if out.AccessToken == "" {
		t.Fatalf("login: empty accessToken")
	}
	return out.AccessToken
}

// doRequest는 method/path/body로 요청 + Authorization 헤더 옵션 부착.
func (f *testFixture) doRequest(t *testing.T, method, path, token string, body []byte) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, f.server.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// === 5개 endpoint 회귀 테스트 ===

func TestLoginReturnsAccessTokenForValidCreds(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	body, _ := json.Marshal(map[string]string{
		"email":    f.email,
		"password": f.password,
	})
	resp, err := http.Post(f.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}

	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		User         struct {
			ID       string `json:"id"`
			Email    string `json:"email"`
			TenantID string `json:"tenantId"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Fatalf("empty tokens: %+v", out)
	}
	if out.User.Email != f.email {
		t.Errorf("user.email=%q, want %q", out.User.Email, f.email)
	}
	if out.User.TenantID != string(f.tenantID) {
		t.Errorf("user.tenantId=%q, want %q", out.User.TenantID, string(f.tenantID))
	}
}

func TestLoginReturns401ForInvalidEmail(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	body, _ := json.Marshal(map[string]string{
		"email":    "nonexistent@example.com",
		"password": f.password,
	})
	resp, err := http.Post(f.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestLoginReturns401ForInvalidPassword(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	body, _ := json.Marshal(map[string]string{
		"email":    f.email,
		"password": "wrongpassword12",
	})
	resp, err := http.Post(f.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestLoginReturns400ForInvalidJSON(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	resp, err := http.Post(f.server.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestMeReturnsCurrentUserWithBearerToken(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/auth/me", token, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		TenantID string `json:"tenantId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ID != f.userID {
		t.Errorf("id=%q, want %q", out.ID, f.userID)
	}
	if out.Email != f.email {
		t.Errorf("email=%q, want %q", out.Email, f.email)
	}
}

func TestMeReturns401WithoutBearerToken(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	resp := f.doRequest(t, "GET", "/api/v1/auth/me", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestMeReturns401WithInvalidToken(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	resp := f.doRequest(t, "GET", "/api/v1/auth/me", "not.a.valid.token", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestMeReturns401WithMalformedAuthHeader(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	req, _ := http.NewRequest("GET", f.server.URL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Token abc") // Bearer 누락
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestListRobotsReturnsTenantScopedRobots(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	fleetID := seedFleetAndRobot(t, f, "fleet-1", "robot-1", "10.0.0.1")

	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/robots", token, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Robots []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Host    string `json:"host"`
			FleetID string `json:"fleetId"`
		} `json:"robots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Robots) != 1 {
		t.Fatalf("len(robots)=%d, want 1: %+v", len(out.Robots), out)
	}
	if out.Robots[0].FleetID != fleetID {
		t.Errorf("fleetId=%q, want %q", out.Robots[0].FleetID, fleetID)
	}
	if out.Robots[0].Name != "robot-1" {
		t.Errorf("name=%q, want robot-1", out.Robots[0].Name)
	}
}

func TestListRobots401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	resp := f.doRequest(t, "GET", "/api/v1/robots", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestStartScanReturns201(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	fleetID := seedFleetAndRobot(t, f, "fleet-scan", "rb-scan", "10.0.0.2")
	packID := seedPack(t, f, "pk_TEST")

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId": fleetID,
		"packId":  packID,
		"trigger": "manual",
	})
	resp := f.doRequest(t, "POST", "/api/v1/scans", token, body)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(out.SessionID, "scan_") {
		t.Errorf("sessionId=%q, want prefix scan_", out.SessionID)
	}
	if out.Status != "pending" {
		t.Errorf("status=%q, want pending", out.Status)
	}
}

func TestStartScan400ForMissingFleetId(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"packId":  "pk_X",
		"trigger": "manual",
	})
	resp := f.doRequest(t, "POST", "/api/v1/scans", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
}

func TestStartScan401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	body, _ := json.Marshal(map[string]any{"fleetId": "x", "packId": "y"})
	resp := f.doRequest(t, "POST", "/api/v1/scans", "", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestListReportsReturnsTenantScoped(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/reports", token, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Reports []map[string]any `json:"reports"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 시드된 report 없음 → 빈 배열 반환.
	if len(out.Reports) != 0 {
		t.Errorf("expected 0 reports, got %d", len(out.Reports))
	}
}

func TestUnimplementedEndpointReturns501(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	// /api/v1/audit/head는 gen.Unimplemented가 자동 501.
	resp := f.doRequest(t, "GET", "/api/v1/audit/head", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 501", resp.StatusCode, string(raw))
	}
}

func TestAuthMiddlewareInjectsTenantContext(t *testing.T) {
	// /api/v1/auth/me는 미들웨어가 ctx에 TenantID를 주입한 후 Tx 진입 — TenantID가 없으면 401.
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/auth/me", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		TenantID string `json:"tenantId"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.TenantID != string(f.tenantID) {
		t.Errorf("tenantId=%q, want %q", out.TenantID, string(f.tenantID))
	}
}

// === helpers ===

// seedFleetAndRobot은 fleet + robot을 도메인 Service로 생성합니다.
func seedFleetAndRobot(t *testing.T, f *testFixture, fleetName, robotName, host string) string {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), f.tenantID)
	var fleetID string
	if err := f.storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		fl, e := f.robot.CreateFleet(ctx, tx, robot.CreateFleetRequest{Name: fleetName})
		if e != nil {
			return e
		}
		fleetID = fl.ID
		_, e = f.robot.CreateRobot(ctx, tx, robot.CreateRobotRequest{
			FleetID:  fl.ID,
			Name:     robotName,
			Host:     host,
			Port:     22,
			AuthType: robot.AuthTypePassword,
			Material: robot.CredentialMaterial{
				Type:     robot.CredentialTypePassword,
				Username: "user",
				Password: "pass",
			},
		})
		return e
	}); err != nil {
		t.Fatalf("seedFleetAndRobot: %v", err)
	}
	return fleetID
}

// seedPack은 packs 테이블에 raw INSERT — Service에 Pack create가 없으므로 직접 SQL.
func seedPack(t *testing.T, f *testFixture, packID string) string {
	t.Helper()
	ctx := context.Background()
	if err := f.storage.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, e := tx.Exec(ctx,
			`INSERT INTO packs (id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at)
VALUES (?, ?, 'cis', '1.0', 'CIS', ?, x'00', 'key_test', ?)`,
			packID, string(f.tenantID), packID+"-key", now)
		return e
	}); err != nil {
		t.Fatalf("seedPack: %v", err)
	}
	return packID
}

// === fakes ===

// nullAuditEmitter는 모든 emit을 no-op으로 처리하는 테스트용 어댑터입니다.
type nullAuditEmitter struct{}

func (n *nullAuditEmitter) EmitTenantCreated(_ context.Context, _ storage.Tx, _ tenant.Tenant, _ tenant.User) error {
	return nil
}

func (n *nullAuditEmitter) EmitFleetCreated(_ context.Context, _ storage.Tx, _ robot.Fleet) error {
	return nil
}

func (n *nullAuditEmitter) EmitRobotCreated(_ context.Context, _ storage.Tx, _ robot.Robot, _ string) error {
	return nil
}

func (n *nullAuditEmitter) EmitRobotDeleted(_ context.Context, _ storage.Tx, _ string, _ storage.TenantID) error {
	return nil
}

func (n *nullAuditEmitter) EmitCredentialRotated(_ context.Context, _ storage.Tx, _, _, _ string, _ storage.TenantID) error {
	return nil
}

func (n *nullAuditEmitter) EmitScanStarted(_ context.Context, _ storage.Tx, _ scan.ScanSession) error {
	return nil
}

func (n *nullAuditEmitter) EmitScanCompleted(_ context.Context, _ storage.Tx, _ scan.ScanSession) error {
	return nil
}

func (n *nullAuditEmitter) EmitScanFailed(_ context.Context, _ storage.Tx, _ scan.ScanSession, _ string) error {
	return nil
}

func (n *nullAuditEmitter) EmitScanCancelled(_ context.Context, _ storage.Tx, _ scan.ScanSession, _ string) error {
	return nil
}

func (n *nullAuditEmitter) EmitReportGenerated(_ context.Context, _ storage.Tx, _ reporting.Report) error {
	return nil
}

func (n *nullAuditEmitter) EmitReportSigned(_ context.Context, _ storage.Tx, _ reporting.Report) error {
	return nil
}

// E18 — framework 리포트 audit emitter (no-op).
func (n *nullAuditEmitter) EmitFrameworkReportGenerated(_ context.Context, _ storage.Tx, _ reporting.FrameworkReport) error {
	return nil
}
func (n *nullAuditEmitter) EmitFrameworkReportSigned(_ context.Context, _ storage.Tx, _ reporting.FrameworkReport) error {
	return nil
}

// E17 — insight·compliance audit emitter 메서드 (no-op).
func (n *nullAuditEmitter) EmitInsightCreated(_ context.Context, _ storage.Tx, _ insight.Insight) error {
	return nil
}
func (n *nullAuditEmitter) EmitInsightDismissed(_ context.Context, _ storage.Tx, _ insight.Insight, _ string) error {
	return nil
}
func (n *nullAuditEmitter) EmitProfileCreated(_ context.Context, _ storage.Tx, _ compliance.ComplianceProfile) error {
	return nil
}
func (n *nullAuditEmitter) EmitSnapshotGenerated(_ context.Context, _ storage.Tx, _ compliance.FrameworkSnapshot) error {
	return nil
}
func (n *nullAuditEmitter) EmitSuggestionCreated(_ context.Context, _ storage.Tx, _ compliance.MappingSuggestion) error {
	return nil
}
func (n *nullAuditEmitter) EmitSuggestionDecided(_ context.Context, _ storage.Tx, _ compliance.MappingSuggestion) error {
	return nil
}

// E16 — advisor 도메인 audit emitter (no-op).
func (n *nullAuditEmitter) EmitConversationStarted(_ context.Context, _ storage.Tx, _ advisor.Conversation) error {
	return nil
}
func (n *nullAuditEmitter) EmitToolCalled(_ context.Context, _ storage.Tx, _ advisor.ToolCall) error {
	return nil
}
func (n *nullAuditEmitter) EmitAdvisorResponded(_ context.Context, _ storage.Tx, _ advisor.Turn) error {
	return nil
}

// fakeBuilder는 reporting Service Phase 1 Stage B는 reporting 호출 없음 — 의존성 충족용 dummy.
type fakeBuilder struct{}

func (f *fakeBuilder) Build(_ reporting.PDFInput) ([]byte, error) {
	return []byte("%PDF-1.4 fake\n"), nil
}

// === E17 어댑터 — bootstrap.go 패턴 복사 (P5: domain이 scan/audit 직접 import 안 함). ===

type testInsightScanAdapter struct{ svc scan.Service }

func (a *testInsightScanAdapter) ListRecentSessions(ctx context.Context, tx storage.Tx, fleetID string, limit int) ([]insight.ScanSessionView, error) {
	sessions, err := a.svc.ListSessions(ctx, tx, scan.ListSessionsFilter{
		FleetID: fleetID,
		Status:  scan.StatusCompleted,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]insight.ScanSessionView, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, insight.ScanSessionView{
			ID: s.ID, TenantID: s.TenantID, FleetID: s.FleetID,
			Status: string(s.Status), CompletedAt: s.CompletedAt,
		})
	}
	return out, nil
}

func (a *testInsightScanAdapter) ListResultsForSession(ctx context.Context, tx storage.Tx, sessionID string) ([]insight.ScanResultView, error) {
	results, err := a.svc.ListResults(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]insight.ScanResultView, 0, len(results))
	for _, r := range results {
		out = append(out, insight.ScanResultView{
			ID: r.ID, SessionID: r.SessionID, RobotID: r.RobotID,
			CheckID: r.CheckID, Outcome: string(r.Outcome), DurationMs: r.DurationMs,
		})
	}
	return out, nil
}

type testComplianceScanAdapter struct{ svc scan.Service }

func (a *testComplianceScanAdapter) ListResultsForSession(ctx context.Context, tx storage.Tx, sessionID string) ([]compliance.ScanResultView, error) {
	results, err := a.svc.ListResults(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]compliance.ScanResultView, 0, len(results))
	for _, r := range results {
		out = append(out, compliance.ScanResultView{CheckID: r.CheckID, Outcome: string(r.Outcome)})
	}
	return out, nil
}

type testComplianceAuditReaderAdapter struct{ svc audit.Service }

func (a *testComplianceAuditReaderAdapter) Head(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (compliance.HeadView, error) {
	head, err := a.svc.Head(ctx, tx, tenantID)
	if err != nil {
		return compliance.HeadView{}, err
	}
	return compliance.HeadView{Seq: head.Seq, Hash: hex.EncodeToString(head.Hash[:])}, nil
}
