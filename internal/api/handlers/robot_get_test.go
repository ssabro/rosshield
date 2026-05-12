package handlers_test

// robot_get_test.go — GET /api/v1/robots/{robotId} 통합 테스트.
//
// 시나리오:
//   - 시드 후 ID로 조회 → 200 + 메타 round-trip
//   - 미존재 ID → 404
//   - auth 없음 → 401

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestGetRobotReturnsSingleRobot(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	robotID := seedFleetAndRobot(t, f, "fleet-getrobot", "rb-getone", "10.0.6.1")
	// seedFleetAndRobot returns fleetID; need to fetch robot via list to get robot id.
	// 대신 seedFleetAndRobot 내부에서 robot ID도 반환하도록 별 헬퍼 사용 — 본 테스트는 ListRobots로 ID 조회.
	_ = robotID

	token := f.loginAndGetToken(t)
	listResp := f.doRequest(t, "GET", "/api/v1/robots", token, nil)
	if listResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(listResp.Body)
		_ = listResp.Body.Close()
		t.Fatalf("list status=%d body=%s", listResp.StatusCode, string(raw))
	}
	var list struct {
		Robots []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"robots"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	_ = listResp.Body.Close()
	if len(list.Robots) != 1 {
		t.Fatalf("expected 1 robot from list, got %d", len(list.Robots))
	}
	rid := list.Robots[0].ID

	resp := f.doRequest(t, "GET", "/api/v1/robots/"+rid, token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("get status=%d body=%s", resp.StatusCode, string(raw))
	}
	var got struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Host        string `json:"host"`
		Port        int    `json:"port"`
		AuthType    string `json:"authType"`
		Criticality string `json:"criticality"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != rid {
		t.Errorf("id=%q, want %q", got.ID, rid)
	}
	if got.Name != "rb-getone" {
		t.Errorf("name=%q, want rb-getone", got.Name)
	}
	if got.Host != "10.0.6.1" {
		t.Errorf("host=%q, want 10.0.6.1", got.Host)
	}
	if got.Criticality == "" {
		t.Errorf("criticality empty (expected default)")
	}
}

func TestGetRobotReturns404ForUnknownID(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/robots/ro_NOT_EXIST", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 404", resp.StatusCode, string(raw))
	}
}

func TestGetRobotReturns401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	resp := f.doRequest(t, "GET", "/api/v1/robots/ro_X", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestDeleteRobotReturns204AndIsNotIdempotent(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	seedFleetAndRobot(t, f, "fleet-del-rb", "rb-del", "10.0.7.1")
	token := f.loginAndGetToken(t)

	// list로 robot ID 추출.
	listResp := f.doRequest(t, "GET", "/api/v1/robots", token, nil)
	if listResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(listResp.Body)
		_ = listResp.Body.Close()
		t.Fatalf("list status=%d body=%s", listResp.StatusCode, string(raw))
	}
	var list struct {
		Robots []struct {
			ID string `json:"id"`
		} `json:"robots"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	_ = listResp.Body.Close()
	if len(list.Robots) != 1 {
		t.Fatalf("expected 1 robot from list, got %d", len(list.Robots))
	}
	rid := list.Robots[0].ID

	// 첫 DELETE → 204.
	r1 := f.doRequest(t, "DELETE", "/api/v1/robots/"+rid, token, nil)
	defer func() { _ = r1.Body.Close() }()
	if r1.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(r1.Body)
		t.Fatalf("first DELETE status=%d body=%s, want 204", r1.StatusCode, string(raw))
	}

	// 두 번째 DELETE → 404 (R3-5 명시적 한 번만, 멱등 X).
	r2 := f.doRequest(t, "DELETE", "/api/v1/robots/"+rid, token, nil)
	defer func() { _ = r2.Body.Close() }()
	if r2.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(r2.Body)
		t.Fatalf("second DELETE status=%d body=%s, want 404", r2.StatusCode, string(raw))
	}

	// 삭제된 robot은 list에서 사라짐.
	listResp2 := f.doRequest(t, "GET", "/api/v1/robots", token, nil)
	defer func() { _ = listResp2.Body.Close() }()
	var list2 struct {
		Robots []struct {
			ID string `json:"id"`
		} `json:"robots"`
	}
	_ = json.NewDecoder(listResp2.Body).Decode(&list2)
	if len(list2.Robots) != 0 {
		t.Errorf("after delete, list count=%d, want 0", len(list2.Robots))
	}
}

func TestDeleteRobotReturns404ForUnknownID(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "DELETE", "/api/v1/robots/ro_NOT_EXIST", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 404", resp.StatusCode, string(raw))
	}
}

func TestListRobotResultsReturnsEmptyForFreshRobot(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	seedFleetAndRobot(t, f, "fleet-results-empty", "rb-empty", "10.0.8.1")
	token := f.loginAndGetToken(t)

	// robot ID 추출.
	listResp := f.doRequest(t, "GET", "/api/v1/robots", token, nil)
	var list struct {
		Robots []struct {
			ID string `json:"id"`
		} `json:"robots"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	_ = listResp.Body.Close()
	if len(list.Robots) != 1 {
		t.Fatalf("expected 1 robot, got %d", len(list.Robots))
	}
	rid := list.Robots[0].ID

	resp := f.doRequest(t, "GET", "/api/v1/robots/"+rid+"/results", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 200", resp.StatusCode, string(raw))
	}
	var out struct {
		Results []map[string]any `json:"results"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(out.Results))
	}
}

func TestListRobotResultsReturns401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	resp := f.doRequest(t, "GET", "/api/v1/robots/ro_X/results", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestDeleteRobotReturns401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	resp := f.doRequest(t, "DELETE", "/api/v1/robots/ro_X", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}
