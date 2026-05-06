package handlers_test

// scan_ws_test.go — C1 WebSocket scan progress 통합 테스트.
//
// 시나리오:
//   - 인증 미부착 → handshake 실패(401)
//   - 인증된 클라이언트 connect → publish progress 2건 + completed 1건 → 모두 수신 + 정상 close
//   - tenant ID·sessionId 미일치 이벤트는 무시

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
)

func TestScanProgressWSRequiresAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()

	wsURL := wsURLFromHTTP(f.server.URL) + "/api/v1/scans/ss_DEMO/progress"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Authorization 없이 dial — 401 close before WS upgrade.
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		_ = conn.CloseNow()
		t.Fatalf("expected error from unauthorized dial")
	}
	if resp == nil {
		t.Fatalf("nil response from dial")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", resp.StatusCode)
	}
}

func TestScanProgressWSStreamsEvents(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	const sessionID = "ss_DEMO_WS"

	wsURL := wsURLFromHTTP(f.server.URL) + "/api/v1/scans/" + sessionID + "/progress"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// 백그라운드에서 publish 3건 (잡음 1건 + 본 session 2건 + completed 1건).
	publishCtx, publishCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer publishCancel()

	go func() {
		// 짧은 sleep — handler가 subscribe를 등록할 시간 확보.
		time.Sleep(100 * time.Millisecond)

		// 잡음: 다른 sessionID — 클라이언트는 무시해야 함.
		_ = f.bus.Publish(publishCtx, makeEvt(t, scan.EventTypeProgress, string(f.tenantID),
			"ss_OTHER", scan.ProgressEventPayload{SessionID: "ss_OTHER", Total: 99, Completed: 1}))

		// 본 session progress 2건.
		_ = f.bus.Publish(publishCtx, makeEvt(t, scan.EventTypeProgress, string(f.tenantID),
			sessionID, scan.ProgressEventPayload{SessionID: sessionID, Total: 10, Completed: 3, Failed: 0}))
		_ = f.bus.Publish(publishCtx, makeEvt(t, scan.EventTypeProgress, string(f.tenantID),
			sessionID, scan.ProgressEventPayload{SessionID: sessionID, Total: 10, Completed: 7, Failed: 1}))

		// completed는 별 topic이라 worker goroutine이 분리 — publish 순서가 도착 순서를
		// 보장하지 못해 progress가 완전히 channel에 들어갈 짧은 시간을 준다.
		time.Sleep(150 * time.Millisecond)
		_ = f.bus.Publish(publishCtx, makeEvt(t, scan.EventTypeCompleted, string(f.tenantID),
			sessionID, scan.CompletedEventPayload{
				SessionID: sessionID, Status: "completed", Total: 10, Completed: 10, Failed: 1,
			}))
	}()

	// progress 2건 + completed 1건 = 3 메시지 수신.
	var msgs []map[string]any
	for i := 0; i < 3; i++ {
		readCtx, rc := context.WithTimeout(ctx, 5*time.Second)
		typ, data, err := conn.Read(readCtx)
		rc()
		if err != nil {
			t.Fatalf("read[%d]: %v (msgs so far=%v)", i, err, msgs)
		}
		if typ != websocket.MessageText {
			t.Fatalf("unexpected message type: %v", typ)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal[%d]: %v raw=%s", i, err, string(data))
		}
		msgs = append(msgs, m)
	}

	// 모든 메시지가 본 sessionID여야 함 (잡음 필터링 검증).
	for i, m := range msgs {
		if sid, _ := m["sessionId"].(string); sid != sessionID {
			t.Errorf("msg[%d] sessionId=%q, want %q", i, sid, sessionID)
		}
	}
	// 첫·둘째는 progress, 셋째는 completed.
	if k, _ := msgs[0]["kind"].(string); k != "progress" {
		t.Errorf("msg[0] kind=%q, want progress", k)
	}
	if k, _ := msgs[2]["kind"].(string); k != "completed" {
		t.Errorf("msg[2] kind=%q, want completed", k)
	}
	if c, _ := msgs[1]["completed"].(float64); int(c) != 7 {
		t.Errorf("msg[1] completed=%v, want 7", c)
	}
	if status, _ := msgs[2]["status"].(string); status != "completed" {
		t.Errorf("msg[2] status=%q, want completed", status)
	}
}

// wsURLFromHTTP는 httptest URL을 ws:// scheme으로 변환합니다.
func wsURLFromHTTP(u string) string {
	if strings.HasPrefix(u, "https://") {
		return "wss://" + strings.TrimPrefix(u, "https://")
	}
	return "ws://" + strings.TrimPrefix(u, "http://")
}

// makeEvt는 publish용 Event를 빌드합니다 (id/occurredAt는 bus가 자동).
func makeEvt(t *testing.T, eventType, tenantID, sessionID string, payload any) eventbus.Event {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return eventbus.Event{
		Type:      eventType,
		Version:   1,
		TenantID:  tenantID,
		Aggregate: eventbus.AggregateRef{Type: scan.AggregateTypeScanSession, ID: sessionID},
		Payload:   data,
	}
}
