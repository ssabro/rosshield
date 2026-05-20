package handlers

// audit_gc_test.go — E32 Stage 4: RunAuditGC 단위 테스트.
//
// 본 파일은 handler 직접 호출 (Mount 우회) 로 가벼운 단위 검증:
//   - HotGC 미주입 → 503
//   - tenant 미주입 → 401
//   - dry_run query 파싱 (invalid → 400, true/false → dryRun 응답에 반영)
//   - sqlite 기반 HotGC 실행 (dryRun=true) → 200 OK + 응답 JSON 모양 검증
//
// admin 권한 게이트 자체는 본 단위 test 가 우회 — handlers_test.go 의 rbac_integration_test 패턴이
// permission matrix 회귀 차단 (본 endpoint 도 RequirePermission 적용).
//
// PG 실측 (실제 DELETE)은 integration test 별 layer.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const gcTestTenant storage.TenantID = "tn_gc_handler"

func TestRunAuditGC_NoHotGC_503(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Storage: openTestStorage(t),
		Clock:   clock.System(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/gc/run", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), gcTestTenant))
	rec := httptest.NewRecorder()

	h.RunAuditGC(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestRunAuditGC_NoTenant_401(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	gc, _ := rotation.NewHotGC(rotation.HotGCDeps{
		Policy: rotation.DefaultPolicy(),
		Clock:  clock.System(),
	})
	h := New(Deps{Storage: store, Clock: clock.System(), HotGC: gc})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/gc/run", nil)
	// no tenant in context
	rec := httptest.NewRecorder()

	h.RunAuditGC(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRunAuditGC_InvalidDryRun_400(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	gc, _ := rotation.NewHotGC(rotation.HotGCDeps{
		Policy: rotation.DefaultPolicy(),
		Clock:  clock.System(),
	})
	h := New(Deps{Storage: store, Clock: clock.System(), HotGC: gc})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/gc/run?dry_run=notabool", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), gcTestTenant))
	rec := httptest.NewRecorder()

	h.RunAuditGC(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestRunAuditGC_DryRunTrue_200 — sqlite 에서 dryRun=true 로 archived segment 추정 카운트 응답.
//
// HotGC.Run dryRun=true 는 DELETE 미실행 → sqlite 트리거 안전.
func TestRunAuditGC_DryRunTrue_200(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	repo := auditrepo.New(auditrepo.Deps{Clock: clock.System()})

	// 4 entries seed + 2 rotation (각 2 entries).
	seedAuditEntries(t, store, repo, gcTestTenant, 4)
	be, _ := rotation.NewFileBackend(t.TempDir())
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rotOld, _ := rotation.New(rotation.Deps{Clock: clock.NewFake(old), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), gcTestTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, gcTestTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("seg1: %v", err)
	}
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotOld.Rotate(ctx, tx, gcTestTenant, 2, 3, 4)
		return err
	}); err != nil {
		t.Fatalf("seg2: %v", err)
	}

	// HotGC — gcNow 2027-01-01, retention 30d → 모든 segment(old 2026-01-01) 통과.
	gcNow := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	gc, _ := rotation.NewHotGC(rotation.HotGCDeps{
		Policy:   rotation.RotationPolicy{HotRetention: 30 * 24 * time.Hour, ColdBackend: rotation.ColdBackendFile},
		Appender: repo,
		Clock:    clock.NewFake(gcNow),
	})

	h := New(Deps{Storage: store, Clock: clock.NewFake(gcNow), HotGC: gc})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/gc/run?dry_run=true", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), gcTestTenant))
	rec := httptest.NewRecorder()

	h.RunAuditGC(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp auditGCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if !resp.DryRun {
		t.Errorf("dryRun = false, want true")
	}
	if resp.DeletedCount != 4 {
		t.Errorf("deletedCount = %d, want 4", resp.DeletedCount)
	}
	if len(resp.SegmentNumbers) != 2 {
		t.Errorf("segmentNumbers = %v, want 2 items", resp.SegmentNumbers)
	}
	if resp.OldestKeptEntrySeq != 0 {
		t.Errorf("oldestKeptEntrySeq = %d, want 0 (dry run)", resp.OldestKeptEntrySeq)
	}
}

// --- helpers ---

func openTestStorage(t *testing.T) storage.Storage {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_gc.db")
	s, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

func seedAuditEntries(t *testing.T, s storage.Storage, repo *auditrepo.Repo, tenantID storage.TenantID, n int) {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), tenantID)
	for i := 0; i < n; i++ {
		if err := s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: tenantID,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "test.event",
				Target:   audit.Target{Type: "robot", ID: "ro_t"},
				Payload:  []byte(`{}`),
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		}); err != nil {
			t.Fatalf("seed audit %d: %v", i, err)
		}
	}
}
