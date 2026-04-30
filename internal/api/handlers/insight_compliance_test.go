package handlers_test

// insight_compliance_test.go — E17 Phase 2 Insight·Compliance 핸들러 통합 테스트.
//
// fixture는 handlers_test.go의 newFixture 활용 — admin seed + 7 신규 endpoint mount.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// === Insight ===

func TestListInsightsRequiresAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()

	resp, err := http.Get(f.server.URL + "/api/v1/insights")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestListInsightsReturnsEmptyWhenNoneExist(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	req, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/insights", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(raw))
	}
	var body struct {
		Insights []map[string]any `json:"insights"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Insights) != 0 {
		t.Errorf("insights = %d, want 0", len(body.Insights))
	}
}

func TestRunFleetInsightsReturnsZeroForEmptyHistory(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	req, _ := http.NewRequest(http.MethodPost,
		f.server.URL+"/api/v1/fleets/fl_NONEXISTENT/insights:run", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(raw))
	}
	var body struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Count != 0 {
		t.Errorf("count = %d, want 0 (no history)", body.Count)
	}
}

func TestDismissInsightNotFoundReturns404(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]string{"reason": "test dismissal"})
	req, _ := http.NewRequest(http.MethodPost,
		f.server.URL+"/api/v1/insights/ins_NONEXISTENT:dismiss", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d (body=%s), want 404", resp.StatusCode, string(raw))
	}
}

func TestDismissInsightRejectsEmptyReason(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]string{"reason": ""})
	req, _ := http.NewRequest(http.MethodPost,
		f.server.URL+"/api/v1/insights/ins_TEST:dismiss", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (empty reason)", resp.StatusCode)
	}
}

// === Compliance ===

func TestListComplianceProfilesRequiresAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()

	resp, err := http.Get(f.server.URL + "/api/v1/compliance/profiles")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCreateComplianceProfileSucceedsAndDuplicateReturns409(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"framework":        "isms-p",
		"frameworkVersion": "2024",
		"enabled":          true,
	})
	doCreate := func() *http.Response {
		req, _ := http.NewRequest(http.MethodPost,
			f.server.URL+"/api/v1/compliance/profiles", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		return resp
	}

	resp1 := doCreate()
	defer func() { _ = resp1.Body.Close() }()
	if resp1.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp1.Body)
		t.Fatalf("first POST status = %d body=%s, want 201", resp1.StatusCode, string(raw))
	}
	var profile struct {
		ID        string `json:"id"`
		Framework string `json:"framework"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&profile); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if profile.Framework != "isms-p" {
		t.Errorf("framework = %s, want isms-p", profile.Framework)
	}

	// 두 번째 요청 — 동일 framework로 중복 → 409.
	body, _ = json.Marshal(map[string]any{
		"framework":        "isms-p",
		"frameworkVersion": "2024",
	})
	resp2 := doCreate()
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp2.Body)
		t.Errorf("duplicate status = %d body=%s, want 409", resp2.StatusCode, string(raw))
	}
}

func TestCreateComplianceProfileWithBadVersionReturns400(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"framework":        "isms-p",
		"frameworkVersion": "1999", // YAML과 불일치.
	})
	req, _ := http.NewRequest(http.MethodPost,
		f.server.URL+"/api/v1/compliance/profiles", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d body=%s, want 400 (version mismatch)", resp.StatusCode, string(raw))
	}
}

func TestListComplianceSnapshotsAndGenerate(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	// 1. profile 생성.
	body, _ := json.Marshal(map[string]any{
		"framework":        "iso27001-2022",
		"frameworkVersion": "2022",
	})
	req, _ := http.NewRequest(http.MethodPost,
		f.server.URL+"/api/v1/compliance/profiles", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create status=%d body=%s", resp.StatusCode, string(raw))
	}
	var prof struct{ ID string }
	if err := json.NewDecoder(resp.Body).Decode(&prof); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()

	// 2. ListSnapshots — 빈 리스트.
	listURL := f.server.URL + "/api/v1/compliance/profiles/" + prof.ID + "/snapshots"
	req2, _ := http.NewRequest(http.MethodGet, listURL, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp2.Body)
		t.Fatalf("list status=%d body=%s", resp2.StatusCode, string(raw))
	}
	var listBody struct {
		Snapshots []map[string]any `json:"snapshots"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listBody.Snapshots) != 0 {
		t.Errorf("empty list expected, got %d", len(listBody.Snapshots))
	}

	// 3. GenerateSnapshot — sessionID로 새 anchored snapshot.
	//    빈 results여도 통제 평가는 unmapped로 채워짐.
	genBody, _ := json.Marshal(map[string]string{"sessionId": "scan_TEST"})
	genReq, _ := http.NewRequest(http.MethodPost, listURL, bytes.NewReader(genBody))
	genReq.Header.Set("Authorization", "Bearer "+token)
	genReq.Header.Set("Content-Type", "application/json")
	genResp, err := http.DefaultClient.Do(genReq)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	defer func() { _ = genResp.Body.Close() }()
	if genResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(genResp.Body)
		t.Fatalf("generate status=%d body=%s", genResp.StatusCode, string(raw))
	}
	var snap struct {
		ID            string  `json:"id"`
		ProfileID     string  `json:"profileId"`
		OverallScore  float64 `json:"overallScore"`
		UnmappedCount int     `json:"unmappedCount"`
	}
	if err := json.NewDecoder(genResp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.ProfileID != prof.ID {
		t.Errorf("profileID = %s, want %s", snap.ProfileID, prof.ID)
	}
	if snap.UnmappedCount == 0 {
		t.Errorf("UnmappedCount = 0, want >0 (no scan results → all controls unmapped)")
	}
}
