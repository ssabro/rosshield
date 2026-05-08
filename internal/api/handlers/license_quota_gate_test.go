package handlers_test

// license_quota_gate_test.go — E24-D 라이선스 quota gate HTTP 통합 테스트.
//
// 시나리오:
//  1. CreateRobot — robots_max=1 한도, 두 번째 robot 시도 → 402 + field=robots_max.
//  2. CreateScan — scans_per_day=1 한도, 두 번째 scan 시도 → 402 + field=scans_per_day.
//  3. AskAdvisor — llm_tokens_per_day 한도 도달 시 → 402 + field=llm_tokens_per_day.
//  4. License 만료 → 모든 quota check 거부 (CreateRobot 케이스로 대표).
//  5. Community(nil enforcer) → quota 게이트 우회, 정상 흐름 (handlers_test의 newFixture가 검증).

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
	reportingrepo "github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	robotrepo "github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	scanrepo "github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	eventbusinproc "github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/license"
	llmnoop "github.com/ssabro/rosshield/internal/platform/llm/noop"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// licQuotaFixture는 license.Enforcer가 결선된 testFixture입니다.
//
// 동일한 service stack을 만들되 handlers.Deps.License에 enforcer 주입.
type licQuotaFixture struct {
	server   *httptest.Server
	storage  storage.Storage
	robotSvc robot.Service
	tenantID storage.TenantID
	adminID  string // FK 만족용 (advisor_conversations.user_id)
	email    string
	password string
	closeFn  func()
}

// newLicQuotaFixture는 enterprise license enforcer + sample quotas로 fixture를 빌드합니다.
func newLicQuotaFixture(t *testing.T, quotas license.Quota, expiresAt time.Time, features []license.Feature) *licQuotaFixture {
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
	scanSvc := scanrepo.New(scanrepo.Deps{Clock: clk, IDGen: ids, Audit: &nullAuditEmitter{}})
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
		Repo:       advisorRepoSvc,
		LLM:        advisorLLMClient,
		Dispatcher: advisorDispatcher,
	})

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

	// === E24-D — license enforcer 결선 ===
	usage := newQuotaTestUsageReader(store, clk)
	payload := license.Payload{
		Version:   license.SupportedVersion,
		LicenseID: "lic_TEST",
		IssuedTo:  "Test Corp",
		IssuedAt:  clk.Now().Add(-1 * time.Hour),
		ExpiresAt: expiresAt,
		Edition:   license.EditionEnterprise,
		Features:  features,
		Quotas:    quotas,
	}
	enforcer := license.NewEnforcer(payload, usage, clk.Now)

	h := handlers.New(handlers.Deps{
		Storage: store, Clock: clk, Tenant: tenantSvc,
		Robot: robotSvc, Scan: scanSvc, Reporting: reportingSvc,
		Insight: insightSvc, Compliance: complianceSvc, Advisor: advisorSvc,
		Audit: auditSvc, EventBus: bus,
		License: enforcer,
	})

	router := chi.NewRouter()
	h.Mount(router)
	server := httptest.NewServer(router)

	return &licQuotaFixture{
		server:   server,
		storage:  store,
		robotSvc: robotSvc,
		tenantID: createResult.Tenant.ID,
		adminID:  createResult.Admin.ID,
		email:    email,
		password: pw,
		closeFn: func() {
			server.Close()
			ctxClose, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
			_ = bus.Close(ctxClose)
			cancelClose()
			_ = store.Close()
		},
	}
}

func (f *licQuotaFixture) loginToken(t *testing.T) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": f.email, "password": f.password})
	resp, err := http.Post(f.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.AccessToken
}

func (f *licQuotaFixture) doRequest(t *testing.T, method, path, token string, body []byte) *http.Response {
	t.Helper()
	var br io.Reader
	if body != nil {
		br = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, f.server.URL+path, br)
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

// === quota usage reader (handlers 패키지 내 테스트 — cmd/* adapter 재사용 불가하므로 inline) ===

// quotaTestUsageReader는 license.UsageReader 구현체입니다 (handlers_test 전용 inline).
type quotaTestUsageReader struct {
	storage storage.Storage
	clock   clock.Clock
}

func newQuotaTestUsageReader(s storage.Storage, c clock.Clock) *quotaTestUsageReader {
	return &quotaTestUsageReader{storage: s, clock: c}
}

func (a *quotaTestUsageReader) CurrentRobots(ctx context.Context, tenantID string) (int, error) {
	var count int
	err := a.storage.Tx(
		storage.WithTenantID(ctx, storage.TenantID(tenantID)),
		func(ctx context.Context, tx storage.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM robots WHERE tenant_id = ? AND deleted_at IS NULL`,
				tenantID,
			).Scan(&count)
		})
	return count, err
}

func (a *quotaTestUsageReader) ScansToday(ctx context.Context, tenantID string) (int, error) {
	startOfDay := a.clock.Now().UTC().Truncate(24 * time.Hour)
	var count int
	err := a.storage.Tx(
		storage.WithTenantID(ctx, storage.TenantID(tenantID)),
		func(ctx context.Context, tx storage.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM scan_sessions WHERE tenant_id = ? AND created_at >= ?`,
				tenantID, startOfDay.Format(time.RFC3339Nano),
			).Scan(&count)
		})
	return count, err
}

func (a *quotaTestUsageReader) LLMTokensToday(ctx context.Context, tenantID string) (int, error) {
	startOfDay := a.clock.Now().UTC().Truncate(24 * time.Hour)
	var sum int
	err := a.storage.Tx(
		storage.WithTenantID(ctx, storage.TenantID(tenantID)),
		func(ctx context.Context, tx storage.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT COALESCE(SUM(input_tokens + output_tokens), 0) FROM advisor_turns
				  WHERE tenant_id = ? AND created_at >= ?`,
				tenantID, startOfDay.Format(time.RFC3339Nano),
			).Scan(&sum)
		})
	return sum, err
}

// === helpers — fleet/pack 시드 ===

func (f *licQuotaFixture) seedFleet(t *testing.T, name string) string {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), f.tenantID)
	var fleetID string
	if err := f.storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		fl, e := f.robotSvc.CreateFleet(ctx, tx, robot.CreateFleetRequest{Name: name})
		if e != nil {
			return e
		}
		fleetID = fl.ID
		return nil
	}); err != nil {
		t.Fatalf("seedFleet: %v", err)
	}
	return fleetID
}

func (f *licQuotaFixture) seedRobotDirect(t *testing.T, fleetID, name, host string) string {
	t.Helper()
	// 직접 SQL — quota gate를 우회하기 위해 도메인 service 거치지 않고 raw INSERT.
	// (quota gate가 적용된 handler 호출은 본 파일이 검증할 대상이라 시드는 raw로.)
	robotID := "ro_TEST_" + name
	credID := "cr_TEST_" + name
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := f.storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO credentials (
				id, tenant_id, type, encrypted_payload, encryption_meta,
				rotation_due_at, created_at, updated_at, revoked_at
			) VALUES (?, ?, 'password', x'00', ?, NULL, ?, ?, NULL)`,
			credID, string(f.tenantID),
			`{"version":1,"algorithm":"AES-256-GCM","kekKeyId":"kek_test"}`, now, now)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx,
			`INSERT INTO robots (
				id, tenant_id, fleet_id, credential_id, name, host, port, auth_type,
				os_distro, ros_distro, tags, role, criticality, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, 22, 'password', '', '', '[]', '', 'medium', ?, ?)`,
			robotID, string(f.tenantID), fleetID, credID, name, host, now, now)
		return e
	}); err != nil {
		t.Fatalf("seedRobotDirect: %v", err)
	}
	return robotID
}

func (f *licQuotaFixture) seedPackDirect(t *testing.T, packID string) string {
	t.Helper()
	if err := f.storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, e := tx.Exec(ctx,
			`INSERT INTO packs (id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at)
VALUES (?, ?, 'cis', '1.0', 'CIS', ?, x'00', 'key_test', ?)`,
			packID, string(f.tenantID), packID+"-key", now)
		return e
	}); err != nil {
		t.Fatalf("seedPackDirect: %v", err)
	}
	return packID
}

// seedScanDirect는 scan_session을 직접 INSERT (quota 한계 도달 시뮬레이션용).
func (f *licQuotaFixture) seedScanDirect(t *testing.T, fleetID, packID, sessionID string) {
	t.Helper()
	if err := f.storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, e := tx.Exec(ctx,
			`INSERT INTO scan_sessions (
				id, tenant_id, fleet_id, pack_id, trigger, status,
				progress_total, progress_completed, progress_failed,
				failure_reason, created_at, updated_at
			) VALUES (?, ?, ?, ?, 'manual', 'pending', 0, 0, 0, '', ?, ?)`,
			sessionID, string(f.tenantID), fleetID, packID, now, now)
		return e
	}); err != nil {
		t.Fatalf("seedScanDirect: %v", err)
	}
}

func (f *licQuotaFixture) seedAdvisorTurnDirect(t *testing.T, convID, turnID string, tokens int) {
	t.Helper()
	if err := f.storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, e := tx.Exec(ctx,
			`INSERT OR IGNORE INTO advisor_conversations (id, tenant_id, user_id, title, created_at, updated_at)
			 VALUES (?, ?, ?, '', ?, ?)`,
			convID, string(f.tenantID), f.adminID, now, now); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO advisor_turns (
				id, conversation_id, tenant_id, role, content, sequence,
				llm_provider, llm_model, input_tokens, output_tokens, cost_usd, created_at
			) VALUES (?, ?, ?, 'assistant', '', 0, 'noop', '', ?, 0, 0, ?)`,
			turnID, convID, string(f.tenantID), tokens, now)
		return e
	}); err != nil {
		t.Fatalf("seedAdvisorTurnDirect: %v", err)
	}
}

// === 1) CreateRobot quota gate ===

func TestCreateRobot_QuotaExceeded402(t *testing.T) {
	t.Parallel()
	f := newLicQuotaFixture(t,
		license.Quota{RobotsMax: 1, ScansPerDay: 1000, LLMTokensPerDay: 1_000_000},
		time.Now().Add(24*time.Hour),
		nil,
	)
	defer f.closeFn()

	fleetID := f.seedFleet(t, "fleet-q")
	// 이미 robot 1대가 존재 → quota 도달.
	_ = f.seedRobotDirect(t, fleetID, "rob-existing", "10.0.0.1")

	token := f.loginToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId":  fleetID,
		"name":     "rob-overflow",
		"host":     "10.0.0.99",
		"port":     22,
		"authType": "password",
		"username": "u",
		"password": "p",
	})
	resp := f.doRequest(t, "POST", "/api/v1/robots", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPaymentRequired {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 402", resp.StatusCode, string(raw))
	}
	var out struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
		Field  string `json:"field"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Error != "quota exceeded" {
		t.Errorf("error=%q, want 'quota exceeded'", out.Error)
	}
	if out.Field != "robots_max" {
		t.Errorf("field=%q, want robots_max", out.Field)
	}
	if out.Reason == "" {
		t.Error("reason is empty")
	}
}

func TestCreateRobot_BelowQuotaPasses(t *testing.T) {
	t.Parallel()
	f := newLicQuotaFixture(t,
		license.Quota{RobotsMax: 5, ScansPerDay: 1000, LLMTokensPerDay: 1_000_000},
		time.Now().Add(24*time.Hour),
		nil,
	)
	defer f.closeFn()

	fleetID := f.seedFleet(t, "fleet-ok")
	token := f.loginToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId":  fleetID,
		"name":     "rob-ok",
		"host":     "10.0.0.1",
		"port":     22,
		"authType": "password",
		"username": "u",
		"password": "p",
	})
	resp := f.doRequest(t, "POST", "/api/v1/robots", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 201", resp.StatusCode, string(raw))
	}
}

// === 2) CreateScan quota gate ===

func TestCreateScan_QuotaExceeded402(t *testing.T) {
	t.Parallel()
	f := newLicQuotaFixture(t,
		license.Quota{RobotsMax: 100, ScansPerDay: 1, LLMTokensPerDay: 1_000_000},
		time.Now().Add(24*time.Hour),
		nil,
	)
	defer f.closeFn()

	fleetID := f.seedFleet(t, "fleet-scan-q")
	packID := f.seedPackDirect(t, "pk_QUOTA")
	// 이미 오늘 scan 1건 → quota 도달.
	f.seedScanDirect(t, fleetID, packID, "scan_PRE")

	token := f.loginToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId": fleetID,
		"packId":  packID,
		"trigger": "manual",
	})
	resp := f.doRequest(t, "POST", "/api/v1/scans", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPaymentRequired {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 402", resp.StatusCode, string(raw))
	}
	var out struct {
		Field string `json:"field"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Field != "scans_per_day" {
		t.Errorf("field=%q, want scans_per_day", out.Field)
	}
}

// === 3) AskAdvisor LLM tokens quota gate ===

func TestAskAdvisor_LLMQuotaExceeded402(t *testing.T) {
	t.Parallel()
	f := newLicQuotaFixture(t,
		license.Quota{RobotsMax: 100, ScansPerDay: 1000, LLMTokensPerDay: 100},
		time.Now().Add(24*time.Hour),
		nil,
	)
	defer f.closeFn()

	// 이미 100 토큰 사용 — 다음 1 토큰 요청도 거부.
	f.seedAdvisorTurnDirect(t, "conv_Q", "turn_Q1", 100)

	token := f.loginToken(t)
	body, _ := json.Marshal(map[string]any{"question": "hello"})
	resp := f.doRequest(t, "POST", "/api/v1/advisor/conversations:ask", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPaymentRequired {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 402", resp.StatusCode, string(raw))
	}
	var out struct {
		Field string `json:"field"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Field != "llm_tokens_per_day" {
		t.Errorf("field=%q, want llm_tokens_per_day", out.Field)
	}
}

// === 4) Expired license ===

func TestCreateRobot_ExpiredLicense402(t *testing.T) {
	t.Parallel()
	f := newLicQuotaFixture(t,
		license.Quota{RobotsMax: 100},
		time.Now().Add(-1*time.Hour), // 이미 만료.
		nil,
	)
	defer f.closeFn()

	fleetID := f.seedFleet(t, "fleet-exp")
	token := f.loginToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId":  fleetID,
		"name":     "rob-x",
		"host":     "10.0.0.1",
		"port":     22,
		"authType": "password",
		"username": "u",
		"password": "p",
	})
	resp := f.doRequest(t, "POST", "/api/v1/robots", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPaymentRequired {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 402 (expired license)", resp.StatusCode, string(raw))
	}
	var out struct {
		Reason string `json:"reason"`
		Field  string `json:"field"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Field != "robots_max" {
		t.Errorf("field=%q, want robots_max", out.Field)
	}
}

// === Sanity — license payload encode/decode (Sign·Verify는 license_test.go에서 검증) ===

func TestLicenseBootstrapEnforcerPayloadHelper(t *testing.T) {
	t.Parallel()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	p := license.Payload{
		Version:   license.SupportedVersion,
		LicenseID: "lic_HEX",
		IssuedTo:  "Hex Corp",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		Edition:   license.EditionEnterprise,
		Quotas:    license.Quota{RobotsMax: 5},
	}
	tok, err := license.Sign(priv, p)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pubHex := hex.EncodeToString(pub)
	if pubHex == "" {
		t.Fatal("pubHex empty")
	}
	if tok == "" {
		t.Fatal("token empty")
	}
}
