package handlers_test

// usage_stats_test.go — GET /api/v1/usage/stats handler 단위 테스트.
//
// 시나리오 (handler 직접 호출 — httptest.Server 결선 회피, fixture overhead 최소화):
//
//	200 — Metrics registry 결선 + tenant context 주입 + 카운트 증가 후 read
//	503 — Deps.Metrics nil (registry 미설정)
//	401 — tenant context 부재

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/api/handlers"
	"github.com/ssabro/rosshield/internal/platform/metrics"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func TestGetUsageStats_Returns200WithTenantCounts(t *testing.T) {
	t.Parallel()
	reg := metrics.New()
	const tenantID storage.TenantID = "tn_test"

	// counter 증가 — 0이 아닌 값 read 검증.
	reg.ScansStartedTotal.WithLabelValues(string(tenantID)).Inc()
	reg.ScansCompletedTotal.WithLabelValues(string(tenantID), "completed").Inc()
	reg.ScansCompletedTotal.WithLabelValues(string(tenantID), "completed").Inc()
	reg.ScansCompletedTotal.WithLabelValues(string(tenantID), "failed").Inc()
	reg.ScanFailedChecksTotal.WithLabelValues(string(tenantID)).Add(7)

	h := handlers.New(handlers.Deps{Metrics: reg})

	req := httptest.NewRequest("GET", "/api/v1/usage/stats", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.GetUsageStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Tenant            string             `json:"tenant"`
		ScansStarted      float64            `json:"scansStarted"`
		ScansCompleted    map[string]float64 `json:"scansCompleted"`
		ScanFailedChecks  float64            `json:"scanFailedChecks"`
		ScansCompletedSum float64            `json:"scansCompletedSum"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Tenant != string(tenantID) {
		t.Errorf("Tenant=%q, want %q", got.Tenant, tenantID)
	}
	if got.ScansStarted != 1 {
		t.Errorf("ScansStarted=%v, want 1", got.ScansStarted)
	}
	if got.ScansCompleted["completed"] != 2 {
		t.Errorf("Completed[completed]=%v, want 2", got.ScansCompleted["completed"])
	}
	if got.ScansCompleted["failed"] != 1 {
		t.Errorf("Completed[failed]=%v, want 1", got.ScansCompleted["failed"])
	}
	if got.ScansCompletedSum != 3 { // 2 completed + 1 failed
		t.Errorf("ScansCompletedSum=%v, want 3", got.ScansCompletedSum)
	}
	if got.ScanFailedChecks != 7 {
		t.Errorf("ScanFailedChecks=%v, want 7", got.ScanFailedChecks)
	}
}

func TestGetUsageStats_Returns503WhenMetricsNotConfigured(t *testing.T) {
	t.Parallel()
	h := handlers.New(handlers.Deps{}) // Metrics nil

	req := httptest.NewRequest("GET", "/api/v1/usage/stats", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), "tn_test"))
	rec := httptest.NewRecorder()

	h.GetUsageStats(rec, req)

	if rec.Code != 503 {
		t.Fatalf("status=%d body=%s, want 503", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "metrics registry") {
		t.Errorf("body should mention 'metrics registry': %s", rec.Body.String())
	}
}

func TestGetUsageStats_Returns401WithoutTenantContext(t *testing.T) {
	t.Parallel()
	reg := metrics.New()
	h := handlers.New(handlers.Deps{Metrics: reg})

	// tenant context 미주입 — TenantIDFromContext returns ""
	req := httptest.NewRequest("GET", "/api/v1/usage/stats", nil)
	rec := httptest.NewRecorder()

	h.GetUsageStats(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tenant context") {
		t.Errorf("body should mention 'tenant context': %s", rec.Body.String())
	}
}
