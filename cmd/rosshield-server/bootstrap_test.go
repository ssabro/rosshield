package main

import (
	"context"
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

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// newTestPlatform은 임시 디렉토리에 SQLite DB를 두고 Platform을 초기화합니다.
// Cleanup으로 graceful shutdown을 보장합니다.
func newTestPlatform(t *testing.T) *Platform {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})
	return p
}

func TestBootstrapInitsAllPlatformServices(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)

	if p.Logger == nil {
		t.Error("Logger is nil")
	}
	if p.Clock == nil {
		t.Error("Clock is nil")
	}
	if p.IDGen == nil {
		t.Error("IDGen is nil")
	}
	if p.Storage == nil {
		t.Error("Storage is nil")
	}
	if p.EventBus == nil {
		t.Error("EventBus is nil")
	}
	if p.Signer == nil {
		t.Error("Signer is nil")
	}
	if p.Scheduler == nil {
		t.Error("Scheduler is nil")
	}
	if p.Tenant == nil {
		t.Error("Tenant is nil")
	}
	if p.Benchmark == nil {
		t.Error("Benchmark is nil")
	}
	if p.Robot == nil {
		t.Error("Robot is nil")
	}
	if p.Scan == nil {
		t.Error("Scan is nil")
	}
	if p.ScanRun == nil {
		t.Error("ScanRun is nil")
	}
	if p.Evidence == nil {
		t.Error("Evidence is nil")
	}
	if p.BlobStore == nil {
		t.Error("BlobStore is nil")
	}
	if p.Reporting == nil {
		t.Error("Reporting is nil")
	}
	if p.ReportSigner == nil {
		t.Error("ReportSigner is nil")
	}
	if p.Insight == nil {
		t.Error("Insight is nil")
	}
	if p.Compliance == nil {
		t.Error("Compliance is nil")
	}
	if p.LLM == nil {
		t.Error("LLM is nil")
	}
	if p.LLM.Provider() != "noop" {
		t.Errorf("default LLM.Provider() = %q, want %q", p.LLM.Provider(), "noop")
	}
}

// TestBootstrapLLMProviderSelection은 cfg.LLMProvider에 따른 어댑터 선택을 검증합니다.
func TestBootstrapLLMProviderSelection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		cfg          Config
		wantProvider string
		wantErr      bool
	}{
		{name: "default empty → noop", cfg: Config{}, wantProvider: "noop"},
		{name: "noop explicit", cfg: Config{LLMProvider: "noop"}, wantProvider: "noop"},
		{name: "ollama", cfg: Config{LLMProvider: "ollama"}, wantProvider: "ollama"},
		{name: "vllm without key (self-hosted)", cfg: Config{LLMProvider: "vllm"}, wantProvider: "vllm"},
		{name: "vllm with bearer key", cfg: Config{LLMProvider: "vllm", LLMAPIKey: "sk-vllm"}, wantProvider: "vllm"},
		{name: "anthropic without key → error", cfg: Config{LLMProvider: "anthropic"}, wantErr: true},
		{name: "anthropic with key", cfg: Config{LLMProvider: "anthropic", LLMAPIKey: "sk-test"}, wantProvider: "anthropic"},
		{name: "unknown provider → error", cfg: Config{LLMProvider: "openai"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := tc.cfg
			cfg.DataDir = t.TempDir()
			cfg.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
			p, err := Bootstrap(context.Background(), cfg)
			if tc.wantErr {
				if err == nil {
					t.Errorf("Bootstrap should error, got nil")
					_ = p.Shutdown(context.Background())
				}
				return
			}
			if err != nil {
				t.Fatalf("Bootstrap: %v", err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = p.Shutdown(ctx)
			})
			if p.LLM.Provider() != tc.wantProvider {
				t.Errorf("Provider() = %q, want %q", p.LLM.Provider(), tc.wantProvider)
			}
		})
	}
}

func TestBootstrapCreatesDataFileAndAppliesMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})

	// data.db 파일이 생성되어야 함.
	dbPath := filepath.Join(dir, "data.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("data.db not created at %s: %v", dbPath, err)
	}

	// 첫 마이그레이션이 적용되었으면 platform_info 테이블이 존재해야 함.
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		var name string
		row := tx.QueryRow(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='platform_info'`)
		return row.Scan(&name)
	}); err != nil {
		t.Fatalf("platform_info 테이블 검증 실패: %v", err)
	}
}

func TestBootstrapDataDirAutoCreated(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	nested := filepath.Join(parent, "nonexistent", "rosshield")

	cfg := Config{
		DataDir: nested,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap should auto-create data dir: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})

	if _, err := os.Stat(nested); err != nil {
		t.Errorf("data dir not created: %v", err)
	}
}

func TestHealthzReturnsAllComponentsOk(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)

	mux := newMux(p)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.Components.Storage != "ok" {
		t.Errorf("components.storage = %q, want ok", body.Components.Storage)
	}
	if body.Components.EventBus != "ok" {
		t.Errorf("components.eventbus = %q, want ok", body.Components.EventBus)
	}
	if body.Components.Scheduler != "ok" {
		t.Errorf("components.scheduler = %q, want ok", body.Components.Scheduler)
	}
	if body.Components.Signer == "" {
		t.Errorf("components.signer should be keyID, got empty")
	}
}

func TestHealthzAfterShutdownReturns503(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	mux := newMux(p)

	// shutdown 전 200.
	{
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("before shutdown: status = %d, want 200", rec.Code)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// shutdown 후 503.
	{
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("after shutdown: status = %d, want 503", rec.Code)
		}
	}
}

func TestHealthzRejectsPostStillWorks(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)

	mux := newMux(p)
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz: status = %d, want 405", rec.Code)
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := p.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown should be no-op, got %v", err)
	}
}

func TestBootstrapFailsWhenDataDirEmpty(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DataDir: "",
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	_, err := Bootstrap(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for empty DataDir")
	}
}

// E12 — first-boot built-in pack seed loader가 systemTenant에 자동 install 했는지 확인.
//
// _archives/ 비어 있으면 (첫 dev clone) ErrNoBuiltinsEmbedded → seed warn skip → 0 packs.
// 이 경우 t.Skip — make pack-archive 실행 후 테스트 가치. CI는 ci 타깃이 pack-archive 의존.
func TestBootstrapSeedsBuiltinPacks(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)

	const tenantID storage.TenantID = "system"
	ctx := storage.WithTenantID(context.Background(), tenantID)
	var packCount int
	err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		packs, e := p.Benchmark.ListPacks(ctx, tx, tenantID)
		if e != nil {
			return e
		}
		packCount = len(packs)
		return nil
	})
	if err != nil {
		t.Fatalf("ListPacks: %v", err)
	}
	if packCount == 0 {
		t.Skip("no built-in packs embedded — run 'make pack-archive' before testing seed loader")
	}
	if packCount < 2 {
		t.Errorf("ListPacks: got %d packs, want >= 2 (cis + ros2)", packCount)
	}
}

// E25 — sqlite + HAEnabled 조합은 부팅 거부 (R30-2 부속2 결정).
// PG advisory lock 동등 기능 부재로 audit chain 손상 위험 → 조용한 fallback 금지(원칙 §11).
func TestBootstrapRejectsHaEnabledOnSqlite(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DataDir:       t.TempDir(),
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		StorageDriver: "sqlite",
		HAEnabled:     true,
	}
	_, err := Bootstrap(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for --ha-enabled with sqlite storage")
	}
	if !strings.Contains(err.Error(), "ha-enabled") {
		t.Errorf("error message does not mention ha-enabled: %v", err)
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Errorf("error message does not mention postgres requirement: %v", err)
	}
}

// E34 Stage 2-B — KeystoreType="tpm"은 Linux + TPM 디바이스 환경에서만 부팅 성공.
// 본 단위 테스트는 cross-platform 호환성 우선:
//   - Windows·macOS: 항상 ErrTpmDeviceNotAvailable로 부팅 실패 (조용한 fallback 금지)
//   - Linux: /dev/tpm* 부재 시 동일 에러 — CI runner가 TPM이 없으므로 동일하게 실패
//
// 실 simulator round-trip은 store_linux_test.go의 tpm_integration build tag로 수행.
func TestBootstrapKeystoreTpmReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DataDir:      t.TempDir(),
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		KeystoreType: "tpm",
	}
	_, err := Bootstrap(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected bootstrap failure with KeystoreType=tpm (no TPM device in test env)")
	}
	if !strings.Contains(err.Error(), "TPM") && !strings.Contains(err.Error(), "tpm") {
		t.Errorf("error does not mention TPM: %v", err)
	}
}

// E34 — KeystoreType="" 기본값은 file 어댑터 → 정상 부팅 (현재 동작 보존).
func TestBootstrapKeystoreDefaultIsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		// KeystoreType "" → file
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap with default keystore: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	}()
	if p.Keystore == nil {
		t.Errorf("expected p.Keystore != nil, got nil")
	}
	// keys 디렉터리 존재 확인 (file 어댑터가 첫 LoadOrCreate 시 생성)
	if _, err := os.Stat(filepath.Join(dir, "keys", "platform.ed25519")); err != nil {
		t.Errorf("platform key file not created: %v", err)
	}
}

// E34 — 알 수 없는 driver는 ErrUnsupportedDriver.
func TestBootstrapKeystoreUnknownDriverFails(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DataDir:      t.TempDir(),
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		KeystoreType: "hsm",
	}
	_, err := Bootstrap(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for unknown KeystoreType=hsm")
	}
	if !strings.Contains(err.Error(), "hsm") {
		t.Errorf("error does not mention 'hsm': %v", err)
	}
}

// E25 — HAEnabled 기본값 false → 단일 인스턴스 정상 부팅.
func TestBootstrapDefaultHaDisabledNormalBoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		// HAEnabled 명시 X (기본값 false).
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	}()
	if p.HA != nil {
		t.Errorf("expected p.HA == nil when HAEnabled=false, got non-nil")
	}
}

// 키 영속: 두 번 부팅하면 같은 keyID.
func TestBootstrapPersistsSignerKeyAcrossRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}

	p1, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first Bootstrap: %v", err)
	}
	keyID1 := p1.Signer.KeyID()
	publicKey1 := p1.Signer.PublicKey()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p1.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	p2, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p2.Shutdown(ctx)
	})

	if p2.Signer.KeyID() != keyID1 {
		t.Errorf("KeyID changed across restart: %q → %q (key file not persisted?)", keyID1, p2.Signer.KeyID())
	}
	pub2 := p2.Signer.PublicKey()
	if string(pub2) != string(publicKey1) {
		t.Error("PublicKey bytes differ across restart")
	}

	// 키 파일이 실제로 디스크에 있어야 함.
	if _, err := os.Stat(filepath.Join(dir, "keys", "platform.ed25519")); err != nil {
		t.Errorf("key file missing: %v", err)
	}
}

// audit Service가 결선되어 동작하는지.
func TestBootstrapAuditIsWired(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)

	if p.Audit == nil {
		t.Fatal("Audit is nil")
	}

	// Append 한 번 → Head가 +1 되는지 확인 (system tenant).
	// E12 Stage 8 — bootstrap이 builtin pack을 system tenant에 자동 install하므로
	// 사전 entry가 있을 수 있음. 절대값(=1) 대신 monotonic +1 검증으로 변경.
	ctx := storage.WithTenantID(context.Background(), "system")
	var headBefore audit.ChainHead
	if err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, err := p.Audit.Head(ctx, tx, "system")
		headBefore = h
		return err
	}); err != nil {
		t.Fatalf("Audit.Head before: %v", err)
	}

	if err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := p.Audit.Append(ctx, tx, audit.AppendRequest{
			TenantID: "system",
			Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
			Action:   "platform.boot",
			Target:   audit.Target{Type: "platform", ID: "rosshield-server"},
			Payload:  []byte(`{"version":"0.0.1"}`),
			Outcome:  audit.OutcomeSuccess,
		})
		return e
	}); err != nil {
		t.Fatalf("Audit.Append via Platform: %v", err)
	}

	var headAfter audit.ChainHead
	if err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, err := p.Audit.Head(ctx, tx, "system")
		headAfter = h
		return err
	}); err != nil {
		t.Fatalf("Audit.Head after: %v", err)
	}
	if headAfter.Seq != headBefore.Seq+1 {
		t.Errorf("head.Seq = %d, want %d (before=%d, +1)", headAfter.Seq, headBefore.Seq+1, headBefore.Seq)
	}
}

// healthz가 audit 정보 노출.
func TestHealthzExposesAuditState(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)

	// audit entry 1개 추가.
	ctx := storage.WithTenantID(context.Background(), "system")
	if err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := p.Audit.Append(ctx, tx, audit.AppendRequest{
			TenantID: "system",
			Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
			Action:   "platform.test",
			Target:   audit.Target{Type: "platform", ID: "x"},
			Outcome:  audit.OutcomeSuccess,
		})
		return e
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	mux := newMux(p)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// E12 Stage 8 — bootstrap이 builtin pack 자동 install로 system audit chain에 entry 추가.
	// Append 1건 후 HeadSeq는 정확히 알 수 없음(builtin 개수에 의존) — Status 'ok' + Seq>=1만 검증.
	if body.Audit.HeadSeq < 1 {
		t.Errorf("audit.headSeq = %d, want >= 1", body.Audit.HeadSeq)
	}
	if body.Audit.Status != "ok" {
		t.Errorf("audit.status = %q, want ok", body.Audit.Status)
	}
}

// healthz: 빈 체인이면 audit.status = no-entries.
//
// E12 Stage 8 — bootstrap이 builtin pack을 시드하므로 _archives 비어있을 때만 빈 체인.
// _archives 채워진 환경에서는 t.Skip — builtin 자동 install이 audit entry 만듦.
func TestHealthzEmptyAuditReportsNoEntries(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)

	mux := newMux(p)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Audit.HeadSeq > 0 {
		t.Skipf("builtin packs seeded (audit headSeq=%d) — empty-audit case requires _archives empty",
			body.Audit.HeadSeq)
	}
	if body.Audit.Status != "no-entries" {
		t.Errorf("audit.status = %q, want no-entries", body.Audit.Status)
	}
	if body.Audit.HeadSeq != 0 {
		t.Errorf("audit.headSeq = %d, want 0", body.Audit.HeadSeq)
	}
}

// Scheduler에 system checkpoint 잡이 등록됐는지 — `@every 1s` 짧은 spec으로 확인.
func TestBootstrapRegistersSystemCheckpointJob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir:        dir,
		Logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		CheckpointSpec: "@every 1s",
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})

	// system tenant entry 추가 → 잡이 다음 발화에 checkpoint 작성해야 함.
	ctx := storage.WithTenantID(context.Background(), "system")
	if err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := p.Audit.Append(ctx, tx, audit.AppendRequest{
			TenantID: "system",
			Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
			Action:   "platform.boot",
			Target:   audit.Target{Type: "platform", ID: "x"},
			Outcome:  audit.OutcomeSuccess,
		})
		return e
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	deadline := time.Now().Add(3500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var found bool
		_ = p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
			cp, err := p.Audit.LatestCheckpoint(ctx, tx, "system")
			if err == nil && cp.Seq >= 1 {
				found = true
			}
			return nil
		})
		if found {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("no checkpoint written — system checkpoint job not firing")
}
