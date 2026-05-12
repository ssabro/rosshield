package advisorrun_test

// orchestrator_test.go — E16-C Orchestrator 통합 테스트.
//
// 실 sqliterepo + mock LLMClient + Dispatcher 결선으로 Ask 흐름 e2e 검증.
// LLM 어댑터 자체는 fakeLLMClient로 시뮬레이션 — 실 anthropic/ollama 결선 불필요.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/app/advisorrun"
	"github.com/ssabro/rosshield/internal/domain/advisor"
	advisorrepo "github.com/ssabro/rosshield/internal/domain/advisor/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/evidence"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/llm"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === fakes ===

// scriptedLLMClient — 미리 설정된 응답 sequence를 순서대로 반환합니다.
type scriptedLLMClient struct {
	mu      sync.Mutex
	scripts []advisor.LLMResponse
	idx     int
	err     error
}

func (c *scriptedLLMClient) CompleteWithTools(_ context.Context, _ advisor.LLMRequest) (advisor.LLMResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return advisor.LLMResponse{}, c.err
	}
	if c.idx >= len(c.scripts) {
		return advisor.LLMResponse{Content: "(no more scripts)"}, nil
	}
	resp := c.scripts[c.idx]
	c.idx++
	return resp, nil
}

// fakeAuditEmitter — orchestrator_test에서 사용 (sqliterepo의 fakeAuditEmitter는 다른 패키지).
type advAuditEmitter struct {
	mu                               sync.Mutex
	convStarted, toolCalled, advResp int
}

func (a *advAuditEmitter) EmitConversationStarted(_ context.Context, _ storage.Tx, _ advisor.Conversation) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.convStarted++
	return nil
}
func (a *advAuditEmitter) EmitToolCalled(_ context.Context, _ storage.Tx, _ advisor.ToolCall) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolCalled++
	return nil
}
func (a *advAuditEmitter) EmitAdvisorResponded(_ context.Context, _ storage.Tx, _ advisor.Turn) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.advResp++
	return nil
}

// scanFakeForOrchestrator — tools_test의 fakeScan을 단순 복제 (interface 만족용).
type orchScan struct {
	session scan.ScanSession
	results []scan.ScanResult
}

func (s *orchScan) GetSession(_ context.Context, _ storage.Tx, _ string) (scan.ScanSession, error) {
	return s.session, nil
}
func (s *orchScan) ListResults(_ context.Context, _ storage.Tx, _ string) ([]scan.ScanResult, error) {
	return s.results, nil
}
func (s *orchScan) ListResultsByRobot(_ context.Context, _ storage.Tx, _ string, _ int) ([]scan.ScanResult, error) {
	return s.results, nil
}
func (s *orchScan) StartScan(_ context.Context, _ storage.Tx, _ scan.StartScanRequest) (scan.ScanSession, error) {
	panic("unused")
}
func (s *orchScan) ListSessions(_ context.Context, _ storage.Tx, _ scan.ListSessionsFilter) ([]scan.ScanSession, error) {
	panic("unused")
}
func (s *orchScan) TransitionSession(_ context.Context, _ storage.Tx, _ string, _ scan.SessionStatus, _ string) (scan.ScanSession, error) {
	panic("unused")
}
func (s *orchScan) CancelSession(_ context.Context, _ storage.Tx, _, _ string) (scan.ScanSession, error) {
	panic("unused")
}
func (s *orchScan) RecordResult(_ context.Context, _ storage.Tx, _ scan.RecordResultRequest) (scan.ScanResult, error) {
	panic("unused")
}

type orchEvidence struct{}

func (e *orchEvidence) Read(_ context.Context, _ storage.Tx, _ string) (evidence.Record, []byte, error) {
	return evidence.Record{}, nil, nil
}
func (e *orchEvidence) Store(_ context.Context, _ storage.Tx, _ evidence.StoreInput) (evidence.StoreResult, error) {
	panic("unused")
}
func (e *orchEvidence) LinkToResult(_ context.Context, _ storage.Tx, _ string, _ []string) ([]evidence.RecordedRef, error) {
	panic("unused")
}
func (e *orchEvidence) ListForResult(_ context.Context, _ storage.Tx, _ string) ([]evidence.Record, error) {
	panic("unused")
}

// === harness ===

const orchTenant = "tn_E16C"
const orchUser = "us_E16C"

func newOrchestratorHarness(t *testing.T, scripts []advisor.LLMResponse, llmErr error) (*advisorrun.Orchestrator, *advAuditEmitter, storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: filepath.Join(dir, "orch.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'orch', 'desktop_free', ?)`,
			orchTenant, now); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO users (id, tenant_id, email, display_name, auth_provider, status, created_at, updated_at)
VALUES (?, ?, 'u@orch', 'U', 'password', 'active', ?, ?)`, orchUser, orchTenant, now, now)
		return e
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	emitter := &advAuditEmitter{}
	repo := advisorrepo.New(advisorrepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		Audit: emitter,
	})
	dispatcher := advisorrun.NewDispatcher(&orchScan{}, &orchEvidence{}, clock.System())

	llmClient := &scriptedLLMClient{scripts: scripts, err: llmErr}

	orch := advisorrun.NewOrchestrator(advisorrun.OrchestratorDeps{
		Repo:       repo,
		LLM:        llmClient,
		Dispatcher: dispatcher,
	})
	return orch, emitter, store
}

// === tests ===

func TestAskWithoutToolsReturnsFinalAnswer(t *testing.T) {
	t.Parallel()
	orch, emitter, store := newOrchestratorHarness(t, []advisor.LLMResponse{
		{Content: "이 check는 mount 옵션이 없어서 fail입니다.", LLMProvider: "anthropic", LLMModel: "claude-3-haiku", InputTokens: 50, OutputTokens: 30},
	}, nil)

	tCtx := storage.WithTenantID(context.Background(), orchTenant)
	var resp advisor.AskResponse
	if err := store.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		r, err := orch.Ask(ctx, tx, advisor.AskRequest{
			UserID:   orchUser,
			Question: "왜 CIS-1.1.1.1이 fail이야?",
		})
		resp = r
		return err
	}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.FinalAnswer == "" {
		t.Errorf("FinalAnswer empty")
	}
	if len(resp.Turns) != 2 {
		t.Errorf("turns = %d, want 2 (user + assistant)", len(resp.Turns))
	}
	if emitter.convStarted != 1 {
		t.Errorf("convStarted = %d, want 1", emitter.convStarted)
	}
	if emitter.advResp != 1 {
		t.Errorf("advResp = %d, want 1 (final answer)", emitter.advResp)
	}
}

func TestAskDispatchesToolCallsThenFinalizes(t *testing.T) {
	t.Parallel()
	args, _ := json.Marshal(map[string]string{"sessionId": "scan_X"})
	orch, emitter, store := newOrchestratorHarness(t, []advisor.LLMResponse{
		{
			ToolCalls:   []advisor.ToolCallRequest{{ID: "call_1", ToolName: "get_session", ArgsJSON: args}},
			LLMProvider: "anthropic",
			LLMModel:    "claude-3-haiku",
		},
		{Content: "세션은 completed 상태이고 8/10 통과했습니다.", LLMProvider: "anthropic", LLMModel: "claude-3-haiku"},
	}, nil)

	tCtx := storage.WithTenantID(context.Background(), orchTenant)
	var resp advisor.AskResponse
	if err := store.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		r, err := orch.Ask(ctx, tx, advisor.AskRequest{
			UserID:   orchUser,
			Question: "이 세션은 어떤 상태?",
		})
		resp = r
		return err
	}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.FinalAnswer == "" {
		t.Errorf("FinalAnswer empty")
	}
	// turns: user 1 + assistant(tool_use) 1 + assistant(final) 1 = 3
	if len(resp.Turns) != 3 {
		t.Errorf("turns = %d, want 3", len(resp.Turns))
	}
	if emitter.toolCalled != 1 {
		t.Errorf("toolCalled = %d, want 1", emitter.toolCalled)
	}
	if emitter.advResp != 1 {
		t.Errorf("advResp (final) = %d, want 1", emitter.advResp)
	}
}

func TestAskRespectsMaxToolCalls(t *testing.T) {
	t.Parallel()
	args, _ := json.Marshal(map[string]string{"sessionId": "scan_X"})
	// LLM이 무한 tool call만 반환 → MaxToolCalls 초과 시 break.
	scripts := make([]advisor.LLMResponse, 10)
	for i := range scripts {
		scripts[i] = advisor.LLMResponse{
			ToolCalls: []advisor.ToolCallRequest{{ID: "c", ToolName: "get_session", ArgsJSON: args}},
		}
	}
	orch, _, store := newOrchestratorHarness(t, scripts, nil)

	tCtx := storage.WithTenantID(context.Background(), orchTenant)
	var resp advisor.AskResponse
	if err := store.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		r, err := orch.Ask(ctx, tx, advisor.AskRequest{
			UserID:       orchUser,
			Question:     "test",
			MaxToolCalls: 2,
		})
		resp = r
		return err
	}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	// MaxToolCalls=2 이후 break — partial answer 반환.
	if resp.FinalAnswer == "" {
		t.Errorf("partial answer should not be empty")
	}
}

func TestAskWithDisabledLLMReturnsErrAdvisorDisabled(t *testing.T) {
	t.Parallel()
	orch, _, store := newOrchestratorHarness(t, nil, llm.ErrLLMDisabled)

	tCtx := storage.WithTenantID(context.Background(), orchTenant)
	err := store.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := orch.Ask(ctx, tx, advisor.AskRequest{UserID: orchUser, Question: "test"})
		return e
	})
	if !errors.Is(err, advisor.ErrAdvisorDisabled) {
		t.Errorf("err = %v, want ErrAdvisorDisabled", err)
	}
}

func TestAskRejectsEmptyQuestion(t *testing.T) {
	t.Parallel()
	orch, _, store := newOrchestratorHarness(t, []advisor.LLMResponse{}, nil)
	tCtx := storage.WithTenantID(context.Background(), orchTenant)
	err := store.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := orch.Ask(ctx, tx, advisor.AskRequest{UserID: orchUser, Question: "   "})
		return e
	})
	if !errors.Is(err, advisor.ErrEmptyQuestion) {
		t.Errorf("err = %v, want ErrEmptyQuestion", err)
	}
}

func TestAskReusesExistingConversation(t *testing.T) {
	t.Parallel()
	orch, _, store := newOrchestratorHarness(t, []advisor.LLMResponse{
		{Content: "first answer"},
		{Content: "second answer"},
	}, nil)

	tCtx := storage.WithTenantID(context.Background(), orchTenant)
	var convID string

	// first Ask — 신규 conversation.
	if err := store.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		r, err := orch.Ask(ctx, tx, advisor.AskRequest{UserID: orchUser, Question: "first"})
		convID = r.ConversationID
		return err
	}); err != nil {
		t.Fatalf("first Ask: %v", err)
	}

	// second Ask — 같은 conversation 재사용.
	if err := store.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		r, err := orch.Ask(ctx, tx, advisor.AskRequest{
			ConversationID: convID,
			UserID:         orchUser,
			Question:       "second",
		})
		if r.ConversationID != convID {
			t.Errorf("conversationID changed: %s → %s", convID, r.ConversationID)
		}
		return err
	}); err != nil {
		t.Fatalf("second Ask: %v", err)
	}

	// 4 turns 누적: user1+assistant1 + user2+assistant2.
	var turns []advisor.Turn
	_ = store.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		_, ts, _ := orch.GetConversation(ctx, tx, convID)
		turns = ts
		return nil
	})
	if len(turns) != 4 {
		t.Errorf("turns = %d, want 4", len(turns))
	}
}
