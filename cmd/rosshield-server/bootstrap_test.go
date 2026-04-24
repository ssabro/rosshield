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
