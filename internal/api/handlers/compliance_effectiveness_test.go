package handlers

// compliance_effectiveness_test.go — Phase 11.B-6 effectiveness dashboard 단위 테스트.
//
// 검증:
//   - 의존성 미주입 → 503 (옵트인 게이트)
//   - tenant context 부재 → 401
//   - happy path → 200 + JSON 본문 (totalSubControls/coveredSubControls/categories)
//   - audit emit 0 (read-only — head seq 가 변하지 않음)
//   - audit_entries 집계 → CC6 매핑 시 audit.chain.key_rotated 카운트 응답에 포함

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const effectivenessTenant storage.TenantID = "system"

func TestGetComplianceEffectiveness_NoDeps_503(t *testing.T) {
	t.Parallel()
	h := New(Deps{
		Storage: openTestStorage(t),
		Clock:   clock.System(),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/effectiveness", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), effectivenessTenant))
	rec := httptest.NewRecorder()

	h.GetComplianceEffectiveness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetComplianceEffectiveness_NoTenant_401(t *testing.T) {
	t.Parallel()
	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})

	h := New(Deps{
		Storage:            store,
		Clock:              clk,
		Audit:              auditSvc,
		AuditEffectiveness: auditSvc,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/effectiveness", nil)
	rec := httptest.NewRecorder()

	h.GetComplianceEffectiveness(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetComplianceEffectiveness_HappyPath_200(t *testing.T) {
	t.Parallel()
	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})

	// seed: audit.chain.key_rotated 2 entries — CC6.6 매핑 cover.
	ctx := storage.WithTenantID(context.Background(), effectivenessTenant)
	if err := store.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		for i := 0; i < 2; i++ {
			if _, err := auditSvc.Append(c, tx, audit.AppendRequest{
				TenantID: effectivenessTenant,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "audit.chain.key_rotated",
				Target:   audit.Target{Type: "audit_chain", ID: string(effectivenessTenant)},
				Outcome:  audit.OutcomeSuccess,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// head seq before — read-only 검증용 (handler 호출 후 head 가 변하면 안 됨).
	var headBefore int64
	_ = store.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		h, _ := auditSvc.Head(c, tx, effectivenessTenant)
		headBefore = h.Seq
		return nil
	})

	h := New(Deps{
		Storage:            store,
		Clock:              clk,
		Audit:              auditSvc,
		AuditEffectiveness: auditSvc,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/effectiveness", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), effectivenessTenant))
	rec := httptest.NewRecorder()

	h.GetComplianceEffectiveness(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp effectivenessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}

	if resp.TotalSubControls != 40 {
		t.Errorf("TotalSubControls = %d, want 40", resp.TotalSubControls)
	}
	if resp.CoveredSubControls < 30 || resp.CoveredSubControls > 40 {
		t.Errorf("CoveredSubControls = %d, want in [30,40]", resp.CoveredSubControls)
	}
	if len(resp.Categories) != 12 {
		t.Errorf("Categories = %d, want 12", len(resp.Categories))
	}

	// CC6 카테고리에 audit.chain.key_rotated 매핑이 있음 — last30 >= 2.
	var cc6 *effectivenessCategoryResponse
	for i := range resp.Categories {
		if resp.Categories[i].Code == "CC6" {
			cc6 = &resp.Categories[i]
			break
		}
	}
	if cc6 == nil {
		t.Fatal("CC6 missing from response")
	}
	if cc6.AuditEvents.Last30Days < 2 {
		t.Errorf("CC6.auditEvents.last30Days = %d, want >= 2", cc6.AuditEvents.Last30Days)
	}

	// read-only — handler 호출 후 head seq 가 변하지 않음.
	var headAfter int64
	_ = store.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		h, _ := auditSvc.Head(c, tx, effectivenessTenant)
		headAfter = h.Seq
		return nil
	})
	if headAfter != headBefore {
		t.Errorf("audit head seq changed: before=%d after=%d (read-only handler 위반)",
			headBefore, headAfter)
	}
}
