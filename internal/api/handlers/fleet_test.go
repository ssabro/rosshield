package handlers_test

// fleet_test.go — GET /api/v1/fleets 통합 테스트.
//
// 시나리오: tenant 시드 후 fleet 0~N개 → name ASC 정렬 + tenant scope + 401 검증.

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestListFleetsReturnsEmptyForFreshTenant(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/fleets", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Fleets []map[string]any `json:"fleets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Fleets) != 0 {
		t.Errorf("expected 0 fleets, got %d", len(out.Fleets))
	}
}

func TestListFleetsReturnsTenantScopedFleetsSorted(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	// 3 fleet 시드 (이름 역순으로 만들어 정렬 검증).
	seedFleetAndRobot(t, f, "zeta-fleet", "rb-z", "10.0.5.1")
	seedFleetAndRobot(t, f, "alpha-fleet", "rb-a", "10.0.5.2")
	seedFleetAndRobot(t, f, "mu-fleet", "rb-m", "10.0.5.3")

	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/fleets", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Fleets []struct {
			ID       string `json:"id"`
			TenantID string `json:"tenantId"`
			Name     string `json:"name"`
		} `json:"fleets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Fleets) != 3 {
		t.Fatalf("expected 3 fleets, got %d", len(out.Fleets))
	}
	// name ASC 검증.
	wantOrder := []string{"alpha-fleet", "mu-fleet", "zeta-fleet"}
	for i, fl := range out.Fleets {
		if fl.Name != wantOrder[i] {
			t.Errorf("fleet[%d] name=%q, want %q (sort order)", i, fl.Name, wantOrder[i])
		}
		if fl.TenantID != string(f.tenantID) {
			t.Errorf("fleet[%d] tenantId=%q, want %q", i, fl.TenantID, string(f.tenantID))
		}
		if fl.ID == "" {
			t.Errorf("fleet[%d] empty id", i)
		}
	}
}

func TestListFleetsReturns401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	resp := f.doRequest(t, "GET", "/api/v1/fleets", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}
