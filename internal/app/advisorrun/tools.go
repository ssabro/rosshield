// Package advisorrun은 advisor 도메인의 application layer입니다 (E16-B/C).
//
// 책임:
//   - tools.go: ToolDispatcher 구현 — 7종(Phase 2: 3종 시작) read-only tool 등록
//   - orchestrator.go: LLM client + tool dispatch loop (E16-C)
//
// 도메인 결합 (P5 + P11 explainability):
//
//	advisorrun은 다른 도메인의 read-only Service interface만 호출 — write API 절대 금지.
//	tool 결과는 redaction(E7)으로 자격증명·secret 제거 후 LLM에 전달.
package advisorrun

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/domain/evidence"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Dispatcher는 advisor.ToolDispatcher 구현체입니다.
//
// tool 등록은 NewDispatcher에서 한 번에 — 신규 추가는 도메인 Service 주입 + tool case 추가.
type Dispatcher struct {
	scan     scan.Service
	evidence evidence.Service
	clock    clock.Clock
	tools    []advisor.ToolDefinition
}

// NewDispatcher는 read-only tool 3종이 등록된 dispatcher를 반환합니다.
//
// 등록 tool:
//   - get_session(sessionId) → ScanSession 메타
//   - list_results(sessionId) → ScanResult 배열 (outcome·duration·rationale)
//   - get_evidence(evidenceId) → Evidence record + redacted body (text only, max 8KB)
//
// scan/evidence가 nil이면 해당 tool 호출 시 에러.
func NewDispatcher(scn scan.Service, ev evidence.Service, clk clock.Clock) *Dispatcher {
	if clk == nil {
		clk = clock.System()
	}
	return &Dispatcher{
		scan:     scn,
		evidence: ev,
		clock:    clk,
		tools:    builtinToolDefs(),
	}
}

// AvailableTools는 LLM 컨텍스트에 포함할 tool 정의 목록을 반환합니다.
func (d *Dispatcher) AvailableTools() []advisor.ToolDefinition {
	out := make([]advisor.ToolDefinition, len(d.tools))
	copy(out, d.tools)
	return out
}

// Dispatch는 단일 tool 호출을 실행합니다.
//
// 등록되지 않은 tool은 ErrUnknownTool. dispatch 자체 에러는 ToolCallResult에 빈 결과 + 에러 반환.
// 결과 ResultJSON은 caller(orchestrator)가 ToolCall.ResultJSON으로 영속.
func (d *Dispatcher) Dispatch(ctx context.Context, tx storage.Tx, req advisor.ToolCallRequest) (advisor.ToolCallResult, error) {
	start := d.clock.Now()
	defer func() {
		_ = start // duration은 각 tool 함수에서 계산
	}()

	switch req.ToolName {
	case "get_session":
		return d.toolGetSession(ctx, tx, req.ArgsJSON, start)
	case "list_results":
		return d.toolListResults(ctx, tx, req.ArgsJSON, start)
	case "get_evidence":
		return d.toolGetEvidence(ctx, tx, req.ArgsJSON, start)
	default:
		return advisor.ToolCallResult{}, fmt.Errorf("%w: %s", advisor.ErrUnknownTool, req.ToolName)
	}
}

// === tool 구현 ===

type getSessionArgs struct {
	SessionID string `json:"sessionId"`
}

type sessionView struct {
	ID            string `json:"id"`
	FleetID       string `json:"fleetId"`
	PackID        string `json:"packId"`
	Status        string `json:"status"`
	Trigger       string `json:"trigger"`
	Total         int    `json:"total"`
	Completed     int    `json:"completed"`
	Failed        int    `json:"failed"`
	FailureReason string `json:"failureReason,omitempty"`
	StartedAt     string `json:"startedAt,omitempty"`
	CompletedAt   string `json:"completedAt,omitempty"`
}

func (d *Dispatcher) toolGetSession(ctx context.Context, tx storage.Tx, args json.RawMessage, start time.Time) (advisor.ToolCallResult, error) {
	if d.scan == nil {
		return advisor.ToolCallResult{}, fmt.Errorf("advisorrun: scan service not configured")
	}
	var a getSessionArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return advisor.ToolCallResult{}, fmt.Errorf("get_session: invalid args: %w", err)
	}
	if a.SessionID == "" {
		return advisor.ToolCallResult{}, fmt.Errorf("get_session: sessionId is required")
	}
	s, err := d.scan.GetSession(ctx, tx, a.SessionID)
	if err != nil {
		return advisor.ToolCallResult{}, fmt.Errorf("get_session: %w", err)
	}
	view := sessionView{
		ID:            s.ID,
		FleetID:       s.FleetID,
		PackID:        s.PackID,
		Status:        string(s.Status),
		Trigger:       string(s.Trigger),
		Total:         s.Progress.Total,
		Completed:     s.Progress.Completed,
		Failed:        s.Progress.Failed,
		FailureReason: s.FailureReason,
	}
	if s.StartedAt != nil {
		view.StartedAt = s.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if s.CompletedAt != nil {
		view.CompletedAt = s.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	body, _ := json.Marshal(view)
	return advisor.ToolCallResult{
		ResultJSON: body,
		DurationMs: d.clock.Now().Sub(start).Milliseconds(),
	}, nil
}

type listResultsArgs struct {
	SessionID string `json:"sessionId"`
	RobotID   string `json:"robotId,omitempty"`
}

type resultView struct {
	ID         string `json:"id"`
	RobotID    string `json:"robotId"`
	CheckID    string `json:"checkId"`
	Outcome    string `json:"outcome"`
	EvalReason string `json:"evalReason,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

type listResultsView struct {
	SessionID string       `json:"sessionId"`
	Results   []resultView `json:"results"`
	Count     int          `json:"count"`
}

func (d *Dispatcher) toolListResults(ctx context.Context, tx storage.Tx, args json.RawMessage, start time.Time) (advisor.ToolCallResult, error) {
	if d.scan == nil {
		return advisor.ToolCallResult{}, fmt.Errorf("advisorrun: scan service not configured")
	}
	var a listResultsArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return advisor.ToolCallResult{}, fmt.Errorf("list_results: invalid args: %w", err)
	}
	if a.SessionID == "" {
		return advisor.ToolCallResult{}, fmt.Errorf("list_results: sessionId is required")
	}
	results, err := d.scan.ListResults(ctx, tx, a.SessionID)
	if err != nil {
		return advisor.ToolCallResult{}, fmt.Errorf("list_results: %w", err)
	}
	views := make([]resultView, 0, len(results))
	for _, r := range results {
		if a.RobotID != "" && r.RobotID != a.RobotID {
			continue
		}
		views = append(views, resultView{
			ID:         r.ID,
			RobotID:    r.RobotID,
			CheckID:    r.CheckID,
			Outcome:    string(r.Outcome),
			EvalReason: r.EvalReason,
			DurationMs: r.DurationMs,
		})
	}
	// 결정성 보장: ID로 정렬.
	sort.SliceStable(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	body, _ := json.Marshal(listResultsView{
		SessionID: a.SessionID,
		Results:   views,
		Count:     len(views),
	})
	return advisor.ToolCallResult{
		ResultJSON: body,
		DurationMs: d.clock.Now().Sub(start).Milliseconds(),
	}, nil
}

type getEvidenceArgs struct {
	EvidenceID string `json:"evidenceId"`
}

type evidenceView struct {
	ID          string `json:"id"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	BodyText    string `json:"bodyText,omitempty"` // text 형식만 + max 8KB
	Truncated   bool   `json:"truncated,omitempty"`
}

const maxEvidenceBodyBytes = 8 * 1024

func (d *Dispatcher) toolGetEvidence(ctx context.Context, tx storage.Tx, args json.RawMessage, start time.Time) (advisor.ToolCallResult, error) {
	if d.evidence == nil {
		return advisor.ToolCallResult{}, fmt.Errorf("advisorrun: evidence service not configured")
	}
	var a getEvidenceArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return advisor.ToolCallResult{}, fmt.Errorf("get_evidence: invalid args: %w", err)
	}
	if a.EvidenceID == "" {
		return advisor.ToolCallResult{}, fmt.Errorf("get_evidence: evidenceId is required")
	}
	rec, body, err := d.evidence.Read(ctx, tx, a.EvidenceID)
	if err != nil {
		return advisor.ToolCallResult{}, fmt.Errorf("get_evidence: %w", err)
	}
	view := evidenceView{
		ID:          rec.ID,
		SHA256:      rec.SHA256,
		ContentType: string(rec.ContentType),
		SizeBytes:   rec.SizeBytes,
	}
	// text 종류만 본문 노출 (binary file/screenshot은 sha256만).
	switch string(rec.ContentType) {
	case "stdout", "stderr", "config-snapshot":
		text := string(body)
		if len(text) > maxEvidenceBodyBytes {
			text = text[:maxEvidenceBodyBytes]
			view.Truncated = true
		}
		// Note: evidence 도메인의 Store 흐름이 이미 redaction(E7)을 적용했으므로
		// 본문은 redacted 상태. 추가 redaction 불필요.
		view.BodyText = text
	default:
		// binary나 알 수 없는 형식은 본문 비공개.
	}
	out, _ := json.Marshal(view)
	return advisor.ToolCallResult{
		ResultJSON: out,
		DurationMs: d.clock.Now().Sub(start).Milliseconds(),
	}, nil
}

// builtinToolDefs는 LLM에 노출할 tool 정의 목록입니다 (Anthropic input_schema 형식).
//
// JSON Schema는 minimal — required 필드만 명시.
func builtinToolDefs() []advisor.ToolDefinition {
	return []advisor.ToolDefinition{
		{
			Name:        "get_session",
			Description: "Get scan session metadata (status, fleet, pack, progress, started/completed timestamps).",
			Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "sessionId": {"type": "string", "description": "Scan session ID (e.g. scan_ABC123)"}
  },
  "required": ["sessionId"]
}`),
		},
		{
			Name:        "list_results",
			Description: "List scan results for a session. Optional robotId filter. Returns outcome (pass/fail/error/...) and evalReason.",
			Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "sessionId": {"type": "string"},
    "robotId":   {"type": "string", "description": "Optional robot filter"}
  },
  "required": ["sessionId"]
}`),
		},
		{
			Name:        "get_evidence",
			Description: "Get evidence record by ID. Body is included only for text content types (stdout/stderr/text/config), redacted by E7 pipeline. Truncated to 8KB.",
			Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "evidenceId": {"type": "string", "description": "Evidence record ID (e.g. ev_ABC123)"}
  },
  "required": ["evidenceId"]
}`),
		},
	}
}
