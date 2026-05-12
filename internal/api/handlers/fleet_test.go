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
			ID         string `json:"id"`
			TenantID   string `json:"tenantId"`
			Name       string `json:"name"`
			RobotCount int    `json:"robotCount"`
		} `json:"fleets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Fleets) != 3 {
		t.Fatalf("expected 3 fleets, got %d", len(out.Fleets))
	}
	// name ASC 검증 + 각 fleet에 robot 1대씩 시드됐으므로 robotCount 1.
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
		if fl.RobotCount != 1 {
			t.Errorf("fleet[%d] robotCount=%d, want 1", i, fl.RobotCount)
		}
	}
}

// TestListFleetsReturnsZeroRobotCountForEmptyFleet — robot 미시드 fleet은 RobotCount 0.
func TestListFleetsReturnsZeroRobotCountForEmptyFleet(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	// CreateFleet만 호출 (robot 시드 X).
	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{"name": "empty-fleet"})
	r1 := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	if r1.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(r1.Body)
		_ = r1.Body.Close()
		t.Fatalf("seed POST status=%d body=%s", r1.StatusCode, string(raw))
	}
	_ = r1.Body.Close()

	resp := f.doRequest(t, "GET", "/api/v1/fleets", token, nil)
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Fleets []struct {
			Name       string `json:"name"`
			RobotCount int    `json:"robotCount"`
		} `json:"fleets"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Fleets) != 1 || out.Fleets[0].Name != "empty-fleet" {
		t.Fatalf("got fleets=%+v, want 1 empty-fleet", out.Fleets)
	}
	if out.Fleets[0].RobotCount != 0 {
		t.Errorf("RobotCount=%d, want 0 (no robots seeded)", out.Fleets[0].RobotCount)
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

func TestCreateFleetReturns201(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"name":        "production-east",
		"description": "main production fleet (east)",
	})
	resp := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var got struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		TenantID    string `json:"tenantId"`
		CreatedAt   string `json:"createdAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" {
		t.Errorf("empty id")
	}
	if got.Name != "production-east" {
		t.Errorf("name=%q, want production-east", got.Name)
	}
	if got.CreatedAt == "" {
		t.Errorf("empty createdAt")
	}
}

func TestCreateFleetReturns409ForDuplicateName(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{"name": "fleet-dup"})

	r1 := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	if r1.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(r1.Body)
		_ = r1.Body.Close()
		t.Fatalf("first POST status=%d body=%s", r1.StatusCode, string(raw))
	}
	_ = r1.Body.Close()

	r2 := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	defer func() { _ = r2.Body.Close() }()
	if r2.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(r2.Body)
		t.Fatalf("second POST status=%d body=%s, want 409", r2.StatusCode, string(raw))
	}
}

func TestCreateFleetReturns400ForInvalidCronSpec(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"name": "fleet-bad-cron",
		"policy": map[string]any{
			"scanSchedule": "every minute please",
		},
	})
	resp := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 400", resp.StatusCode, string(raw))
	}
}

func TestCreateFleetAcceptsValidCronSpec(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"name": "fleet-good-cron",
		"policy": map[string]any{
			"scanSchedule":      "@every 6h",
			"defaultBaselineId": "cis-ubuntu-24.04",
		},
	})
	resp := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 201", resp.StatusCode, string(raw))
	}
}

func TestUpdateFleetReturns400ForInvalidCronSpec(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	// 시드.
	createBody, _ := json.Marshal(map[string]any{"name": "fleet-cron-update"})
	r1 := f.doRequest(t, "POST", "/api/v1/fleets", token, createBody)
	if r1.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(r1.Body)
		_ = r1.Body.Close()
		t.Fatalf("seed POST status=%d body=%s", r1.StatusCode, string(raw))
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r1.Body).Decode(&created)
	_ = r1.Body.Close()

	patchBody, _ := json.Marshal(map[string]any{
		"policy": map[string]any{
			"scanSchedule": "broken cron",
		},
	})
	r2 := f.doRequest(t, "PATCH", "/api/v1/fleets/"+created.ID, token, patchBody)
	defer func() { _ = r2.Body.Close() }()
	if r2.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(r2.Body)
		t.Fatalf("status=%d body=%s, want 400", r2.StatusCode, string(raw))
	}
}

func TestCreateFleetReturns400ForEmptyName(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{"name": "  "})
	resp := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 400", resp.StatusCode, string(raw))
	}
}

func TestUpdateFleetReturns200(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{"name": "fleet-orig"})
	r1 := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	if r1.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(r1.Body)
		_ = r1.Body.Close()
		t.Fatalf("seed POST status=%d body=%s", r1.StatusCode, string(raw))
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r1.Body).Decode(&created)
	_ = r1.Body.Close()

	patchBody, _ := json.Marshal(map[string]any{
		"name":        "fleet-renamed",
		"description": "new description",
	})
	r2 := f.doRequest(t, "PATCH", "/api/v1/fleets/"+created.ID, token, patchBody)
	defer func() { _ = r2.Body.Close() }()
	if r2.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(r2.Body)
		t.Fatalf("PATCH status=%d body=%s", r2.StatusCode, string(raw))
	}
	var got struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	_ = json.NewDecoder(r2.Body).Decode(&got)
	if got.Name != "fleet-renamed" {
		t.Errorf("name=%q, want fleet-renamed", got.Name)
	}
	if got.Description != "new description" {
		t.Errorf("description=%q, want %q", got.Description, "new description")
	}
}

func TestUpdateFleetReturns404ForUnknownID(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{"name": "anything"})
	resp := f.doRequest(t, "PATCH", "/api/v1/fleets/fl_NOT_EXIST", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestDeleteFleetReturns204(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{"name": "fleet-del"})
	r1 := f.doRequest(t, "POST", "/api/v1/fleets", token, body)
	if r1.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(r1.Body)
		_ = r1.Body.Close()
		t.Fatalf("seed POST status=%d body=%s", r1.StatusCode, string(raw))
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r1.Body).Decode(&created)
	_ = r1.Body.Close()

	r2 := f.doRequest(t, "DELETE", "/api/v1/fleets/"+created.ID, token, nil)
	defer func() { _ = r2.Body.Close() }()
	if r2.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(r2.Body)
		t.Fatalf("DELETE status=%d body=%s, want 204", r2.StatusCode, string(raw))
	}

	// 두 번째 DELETE → 404 (이미 deleted).
	r3 := f.doRequest(t, "DELETE", "/api/v1/fleets/"+created.ID, token, nil)
	defer func() { _ = r3.Body.Close() }()
	if r3.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(r3.Body)
		t.Fatalf("second DELETE status=%d body=%s, want 404", r3.StatusCode, string(raw))
	}
}
