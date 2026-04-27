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

	// Append 한 번 → Head가 갱신되는지 확인 (system tenant).
	ctx := storage.WithTenantID(context.Background(), "system")
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

	var head audit.ChainHead
	if err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, err := p.Audit.Head(ctx, tx, "system")
		head = h
		return err
	}); err != nil {
		t.Fatalf("Audit.Head: %v", err)
	}
	if head.Seq != 1 {
		t.Errorf("head.Seq = %d, want 1", head.Seq)
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
	if body.Audit.HeadSeq != 1 {
		t.Errorf("audit.headSeq = %d, want 1", body.Audit.HeadSeq)
	}
	if body.Audit.Status != "ok" {
		t.Errorf("audit.status = %q, want ok", body.Audit.Status)
	}
}

// healthz: 빈 체인이면 audit.status = no-entries.
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
