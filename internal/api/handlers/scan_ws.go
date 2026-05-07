package handlers

// scan_ws.go — WebSocket scan progress 스트리밍 (C1 carryover, Phase 1 deferred 회수).
//
// 엔드포인트:
//
//	GET /api/v1/scans/{sessionId}/progress  (WebSocket upgrade)
//
// 흐름:
//
//  1. AuthMiddleware 통과 → tenant scope.
//  2. WebSocket upgrade.
//  3. EventBus subscribe ("scan.progress" + "scan.completed").
//  4. 매 이벤트 receive 시 sessionId 매칭 + tenant 매칭이면 JSON 메시지 전송.
//  5. "scan.completed" 메시지 1건 송신 후 close (Status는 completed/failed/cancelled 어느 것이든).
//  6. 클라이언트 disconnect 또는 ctx cancel 시 즉시 종료.
//
// Phase 1 단순화:
//   - 인증: 일반 Authorization: Bearer만 — 브라우저 WebSocket API에서는 헤더 못 붙이지만
//     서버측 일관성·CLI 호환을 우선. Phase 2에서 ?access_token= query param 또는 cookie 도입 검토.
//   - heartbeat ping: coder/websocket이 자동 ping 송신 (CloseRead 옵션). 본 핸들러는 명시 ping 안 보냄.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// wsProgressMessage는 클라이언트로 송신되는 단일 메시지입니다.
//
// kind="progress"이면 progress payload, kind="completed"이면 completed payload (마지막 메시지).
type wsProgressMessage struct {
	Kind          string `json:"kind"`
	Type          string `json:"type"` // 원본 EventBus type ("scan.progress" 등)
	SessionID     string `json:"sessionId"`
	Total         int    `json:"total"`
	Completed     int    `json:"completed"`
	Failed        int    `json:"failed"`
	Status        string `json:"status,omitempty"` // completed에만
	Reason        string `json:"reason,omitempty"` // failed/cancelled에만
	OccurredAt    string `json:"occurredAt"`
	CorrelationID string `json:"correlationId,omitempty"`
}

// ScanProgress는 GET /api/v1/scans/{sessionId}/progress (WebSocket) 핸들러입니다.
//
// EventBus 의존이 있으므로 Deps.EventBus가 nil이면 503 반환.
func (h *Handlers) ScanProgress(w http.ResponseWriter, r *http.Request, sessionID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.EventBus == nil {
		writeError(w, http.StatusServiceUnavailable, "event bus not configured")
		return
	}

	// localhost 개발과 동일 origin 둘 다 허용.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		// CompressionMode: defaults.
	})
	if err != nil {
		// AcceptError가 이미 응답을 마무리했음.
		return
	}
	defer func() { _ = conn.Close(websocket.StatusInternalError, "internal error") }()

	// 본 ctx는 클라이언트 disconnect까지 살아있음.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 메시지 enqueue용 채널 — handler goroutine이 evt를 감지하면 push, 메인 loop가 송신.
	msgCh := make(chan wsProgressMessage, 16)

	handler := func(_ context.Context, evt eventbus.Event) error {
		if evt.TenantID != string(tenantID) {
			return nil
		}
		if evt.Aggregate.ID != sessionID {
			return nil
		}
		msg := buildProgressMessage(evt)
		select {
		case msgCh <- msg:
		case <-ctx.Done():
		}
		return nil
	}

	subProg := h.deps.EventBus.Subscribe(ctx, scan.EventTypeProgress, handler)
	defer subProg.Cancel()
	subDone := h.deps.EventBus.Subscribe(ctx, scan.EventTypeCompleted, handler)
	defer subDone.Cancel()

	// CloseRead는 새 ctx를 반환합니다 — main loop만 이 새 ctx를 사용하고,
	// handler closure는 위에서 캡처한 원본 ctx를 그대로 사용해야 race를 피할 수 있습니다.
	// (ctx 변수 자체에 재할당하면 handler goroutine이 동시에 읽고 main goroutine이
	// 쓰는 data race 발생 — 2026-05-07 CI -race 검출.)
	readCtx := conn.CloseRead(ctx)

	for {
		select {
		case <-readCtx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "context done")
			return
		case msg := <-msgCh:
			writeCtx, writeCancel := context.WithTimeout(readCtx, 5*time.Second)
			err := wsjsonWrite(writeCtx, conn, msg)
			writeCancel()
			if err != nil {
				_ = conn.Close(websocket.StatusInternalError, "write failed")
				return
			}
			if msg.Kind == "completed" {
				_ = conn.Close(websocket.StatusNormalClosure, "scan completed")
				return
			}
		}
	}
}

// buildProgressMessage는 EventBus event를 client 메시지로 변환합니다.
func buildProgressMessage(evt eventbus.Event) wsProgressMessage {
	msg := wsProgressMessage{
		Type:          evt.Type,
		OccurredAt:    evt.OccurredAt.UTC().Format(time.RFC3339Nano),
		CorrelationID: evt.CorrelationID,
		SessionID:     evt.Aggregate.ID,
	}
	switch evt.Type {
	case scan.EventTypeProgress:
		msg.Kind = "progress"
		var p scan.ProgressEventPayload
		if err := json.Unmarshal(evt.Payload, &p); err == nil {
			msg.Total = p.Total
			msg.Completed = p.Completed
			msg.Failed = p.Failed
		}
	case scan.EventTypeCompleted:
		msg.Kind = "completed"
		var p scan.CompletedEventPayload
		if err := json.Unmarshal(evt.Payload, &p); err == nil {
			msg.Total = p.Total
			msg.Completed = p.Completed
			msg.Failed = p.Failed
			msg.Status = p.Status
			msg.Reason = p.Reason
		}
	default:
		msg.Kind = evt.Type
	}
	return msg
}

// wsjsonWrite는 wsjson.Write의 직접 구현입니다 (별도 sub-package 의존 회피).
//
// coder/websocket는 wsjson가 별 패키지에 있으나 의존 단순화를 위해 직접 직렬화.
func wsjsonWrite(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
