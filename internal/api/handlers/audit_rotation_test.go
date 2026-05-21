package handlers

// audit_rotation_test.go — Phase 10.D-6 AbortAuditRotation handler 단위 테스트.
//
// admin 권한 게이트(RequirePermission)는 rbac 매트릭스가 별 layer 검증 — 본 단위 test 는
// handler 직접 호출(Mount 우회) 로 가벼운 분기 검증:
//   - KeyRotator 미주입 → 503
//   - body parse → audit emit + 정상 200 응답
//   - empty body 도 허용 (reason 빈 문자열)

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit/keyrotation"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const abortTestTenant storage.TenantID = "system"

func TestAbortAuditRotation_NoRotator_503(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Storage: openTestStorage(t),
		Clock:   clock.System(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/rotation/abort", bytes.NewReader([]byte(`{"reason":"drill"}`)))
	req = req.WithContext(storage.WithTenantID(req.Context(), abortTestTenant))
	rec := httptest.NewRecorder()

	h.AbortAuditRotation(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAbortAuditRotation_HappyPath_200(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})

	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)
	allocator := keyrotation.AllocatorFunc(func(newEpoch int64) (string, ed25519.PrivateKey, error) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		return "h", priv, nil
	})
	rotator, err := keyrotation.New(keyrotation.Deps{
		Storage:     store,
		Audit:       auditSvc,
		ChainKeys:   auditrepo.NewKeyEpochRepo(),
		Signer:      swap,
		Allocator:   allocator,
		Clock:       clk,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinInterval: 0,
		TenantID:    abortTestTenant,
	})
	if err != nil {
		t.Fatalf("keyrotation.New: %v", err)
	}

	h := New(Deps{
		Storage:    store,
		Clock:      clk,
		Audit:      auditSvc,
		KeyRotator: rotator,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/rotation/abort", bytes.NewReader([]byte(`{"reason":"operator drill"}`)))
	req.Header.Set("X-Rosshield-Actor", "ops-1")
	req = req.WithContext(storage.WithTenantID(req.Context(), abortTestTenant))
	rec := httptest.NewRecorder()

	h.AbortAuditRotation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp abortRotationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if !resp.Aborted {
		t.Error("Aborted = false, want true")
	}
	if resp.AuditEntryID <= 0 {
		t.Errorf("AuditEntryID = %d, want > 0", resp.AuditEntryID)
	}
	if resp.Reason != "operator drill" {
		t.Errorf("Reason = %q, want operator drill", resp.Reason)
	}
	if resp.AbortedAt == "" {
		t.Error("AbortedAt empty")
	}
	if resp.PreviousEpoch != 1 {
		t.Errorf("PreviousEpoch = %d, want 1", resp.PreviousEpoch)
	}

	// audit_entries 에 emit 확인.
	ctx := storage.WithTenantID(context.Background(), abortTestTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT COUNT(*) FROM audit_entries WHERE tenant_id = ? AND action = ?`,
			string(abortTestTenant), "audit.chain.rotation_aborted")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			t.Fatal("no count row")
		}
		var n int64
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if n != 1 {
			t.Errorf("audit emit count = %d, want 1", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

func TestAbortAuditRotation_EmptyBody_200(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})

	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)
	allocator := keyrotation.AllocatorFunc(func(newEpoch int64) (string, ed25519.PrivateKey, error) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		return "h", priv, nil
	})
	rotator, err := keyrotation.New(keyrotation.Deps{
		Storage:     store,
		Audit:       auditSvc,
		ChainKeys:   auditrepo.NewKeyEpochRepo(),
		Signer:      swap,
		Allocator:   allocator,
		Clock:       clk,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinInterval: 0,
		TenantID:    abortTestTenant,
	})
	if err != nil {
		t.Fatalf("keyrotation.New: %v", err)
	}

	h := New(Deps{
		Storage:    store,
		Clock:      clk,
		Audit:      auditSvc,
		KeyRotator: rotator,
	})

	// empty body — JSON decoder 가 EOF 만나도 정상 통과 (reason 빈 문자열로 처리).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/rotation/abort", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), abortTestTenant))
	rec := httptest.NewRecorder()

	h.AbortAuditRotation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp abortRotationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if !resp.Aborted {
		t.Error("Aborted = false, want true")
	}
	if resp.Reason != "" {
		t.Errorf("Reason = %q, want empty", resp.Reason)
	}
}
