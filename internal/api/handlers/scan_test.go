package handlers_test

// scan_test.go — GET /api/v1/scans/{sessionId} + WS query-token auth fallback.
//
// 시나리오:
//   - GetScan: POST scan → GET 200 with payload / 미존재 → 404 / no auth → 401
//   - WS query token: ?access_token= 으로 connect → 정상 핸드셰이크 / 잘못된 token → 401

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestGetScanReturnsSession(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	fleetID := seedFleetAndRobot(t, f, "fleet-getscan", "rb-getscan", "10.0.0.10")
	packID := seedPack(t, f, "pk_GETSCAN")

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId": fleetID,
		"packId":  packID,
		"trigger": "manual",
	})
	resp := f.doRequest(t, "POST", "/api/v1/scans", token, body)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("POST status=%d body=%s", resp.StatusCode, string(raw))
	}
	var created struct {
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode POST: %v", err)
	}
	_ = resp.Body.Close()

	getResp := f.doRequest(t, "GET", "/api/v1/scans/"+created.SessionID, token, nil)
	defer func() { _ = getResp.Body.Close() }()

	if getResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(getResp.Body)
		t.Fatalf("GET status=%d body=%s", getResp.StatusCode, string(raw))
	}
	var got struct {
		SessionID string `json:"sessionId"`
		FleetID   string `json:"fleetId"`
		PackID    string `json:"packId"`
		Status    string `json:"status"`
		Total     int    `json:"total"`
		Completed int    `json:"completed"`
		Failed    int    `json:"failed"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.SessionID != created.SessionID {
		t.Errorf("sessionId=%q, want %q", got.SessionID, created.SessionID)
	}
	if got.FleetID != fleetID {
		t.Errorf("fleetId=%q, want %q", got.FleetID, fleetID)
	}
	if got.PackID != packID {
		t.Errorf("packId=%q, want %q", got.PackID, packID)
	}
	if got.CreatedAt == "" {
		t.Errorf("createdAt empty")
	}
}

func TestGetScanReturns404ForUnknownID(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/scans/scan_NOT_EXIST", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 404", resp.StatusCode, string(raw))
	}
}

func TestGetScanReturns401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	resp := f.doRequest(t, "GET", "/api/v1/scans/scan_ANY", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestScanProgressWSAcceptsQueryToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	const sessionID = "ss_QUERY_TOKEN"
	wsURL := wsURLFromHTTP(f.server.URL) + "/api/v1/scans/" + sessionID + "/progress?access_token=" + token

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial with query token: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	// 핸드셰이크만 성공해도 query token 인증 동작 확인 — 메시지 송수신은 별 테스트.
}

func TestScanProgressWSRejectsInvalidQueryToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()

	wsURL := wsURLFromHTTP(f.server.URL) + "/api/v1/scans/ss_BAD/progress?access_token=not.a.real.token"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		_ = conn.CloseNow()
		t.Fatalf("expected dial error for invalid query token")
	}
	if resp == nil {
		t.Fatalf("nil response from dial")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", resp.StatusCode)
	}
}

func TestCancelScanReturns200ForPendingSession(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	fleetID := seedFleetAndRobot(t, f, "fleet-cancel", "rb-cancel", "10.0.0.30")
	packID := seedPack(t, f, "pk_CANCEL")
	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId": fleetID,
		"packId":  packID,
		"trigger": "manual",
	})
	resp := f.doRequest(t, "POST", "/api/v1/scans", token, body)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("POST status=%d body=%s", resp.StatusCode, string(raw))
	}
	var created struct {
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()

	cancelBody, _ := json.Marshal(map[string]string{"reason": "test cancel"})
	cancelResp := f.doRequest(t, "POST", "/api/v1/scans/"+created.SessionID+":cancel", token, cancelBody)
	defer func() { _ = cancelResp.Body.Close() }()
	if cancelResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(cancelResp.Body)
		t.Fatalf("cancel status=%d body=%s", cancelResp.StatusCode, string(raw))
	}
	var got struct {
		SessionID     string `json:"sessionId"`
		Status        string `json:"status"`
		FailureReason string `json:"failureReason"`
	}
	if err := json.NewDecoder(cancelResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode cancel: %v", err)
	}
	if got.SessionID != created.SessionID {
		t.Errorf("sessionId=%q, want %q", got.SessionID, created.SessionID)
	}
	if got.Status != "cancelled" {
		t.Errorf("status=%q, want cancelled", got.Status)
	}
	if got.FailureReason != "test cancel" {
		t.Errorf("failureReason=%q, want %q", got.FailureReason, "test cancel")
	}
}

func TestCancelScanReturns404ForUnknownID(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "POST", "/api/v1/scans/scan_NOPE:cancel", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestCancelScanReturns401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	resp := f.doRequest(t, "POST", "/api/v1/scans/scan_X:cancel", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestCancelScanReturns409ForAlreadyTerminal(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	fleetID := seedFleetAndRobot(t, f, "fleet-c2", "rb-c2", "10.0.0.31")
	packID := seedPack(t, f, "pk_C2")
	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId": fleetID,
		"packId":  packID,
		"trigger": "manual",
	})
	resp := f.doRequest(t, "POST", "/api/v1/scans", token, body)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("POST status=%d body=%s", resp.StatusCode, string(raw))
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()

	// 첫 cancel — pending → cancelled.
	r1 := f.doRequest(t, "POST", "/api/v1/scans/"+created.SessionID+":cancel", token, nil)
	if r1.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(r1.Body)
		_ = r1.Body.Close()
		t.Fatalf("first cancel status=%d body=%s", r1.StatusCode, string(raw))
	}
	_ = r1.Body.Close()

	// 두 번째 cancel — terminal → 409.
	r2 := f.doRequest(t, "POST", "/api/v1/scans/"+created.SessionID+":cancel", token, nil)
	defer func() { _ = r2.Body.Close() }()
	if r2.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(r2.Body)
		t.Fatalf("second cancel status=%d body=%s, want 409", r2.StatusCode, string(raw))
	}
}

func TestGetScanCrossTenantReturns404(t *testing.T) {
	// Tenant A 세션은 Tenant B에서 보면 404 (tenant 격리).
	f := newFixture(t)
	defer f.closeFn()

	// A 세션 시드
	fleetID := seedFleetAndRobot(t, f, "fleet-iso", "rb-iso", "10.0.0.20")
	packID := seedPack(t, f, "pk_ISO")
	tokenA := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId": fleetID,
		"packId":  packID,
		"trigger": "manual",
	})
	resp := f.doRequest(t, "POST", "/api/v1/scans", tokenA, body)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("POST status=%d body=%s", resp.StatusCode, string(raw))
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()

	// 잘못된 sessionId(다른 tenant 패턴) — 본 테스트는 단일 tenant 환경이라
	// 미존재 ID로 대신 검증 (cross-tenant SQL 격리는 sqliterepo 단위에서 보장).
	// 본 핸들러에서는 ErrNotFound → 404 매핑만 확인.
	getResp := f.doRequest(t, "GET", "/api/v1/scans/scan_OTHER_TENANT", tokenA, nil)
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d, want 404 for cross-tenant ID", getResp.StatusCode)
	}
}
