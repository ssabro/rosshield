package sqliterepo_test

// repo_test.go — E16-A advisor sqliterepo 단위 테스트.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/domain/advisor/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === fakes ===

type fakeAuditEmitter struct {
	mu             sync.Mutex
	convStarted    int
	toolCalled     int
	advResponded   int
	lastConvID     string
	lastToolName   string
	lastResponseID string
}

func (a *fakeAuditEmitter) EmitConversationStarted(_ context.Context, _ storage.Tx, c advisor.Conversation) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.convStarted++
	a.lastConvID = c.ID
	return nil
}
func (a *fakeAuditEmitter) EmitToolCalled(_ context.Context, _ storage.Tx, t advisor.ToolCall) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolCalled++
	a.lastToolName = t.ToolName
	return nil
}
func (a *fakeAuditEmitter) EmitAdvisorResponded(_ context.Context, _ storage.Tx, t advisor.Turn) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.advResponded++
	a.lastResponseID = t.ID
	return nil
}

// === harness ===

const testTenant = "tn_E16A"
const testUser = "us_E16A"

func newTestRepo(t *testing.T) (*sqliterepo.Repo, *fakeAuditEmitter, storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: filepath.Join(dir, "advisor.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'adv-test', 'desktop_free', ?)`,
			testTenant, now); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO users (id, tenant_id, email, display_name, auth_provider, status, created_at, updated_at)
VALUES (?, ?, 'u@adv.test', 'U', 'password', 'active', ?, ?)`, testUser, testTenant, now, now)
		return e
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	emitter := &fakeAuditEmitter{}
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		Audit: emitter,
	})
	return repo, emitter, store
}

func tenantCtx() context.Context {
	return storage.WithTenantID(context.Background(), testTenant)
}

// === tests ===

func TestStartConversationCreatesAndEmitsAudit(t *testing.T) {
	t.Parallel()
	repo, emitter, store := newTestRepo(t)

	var conv advisor.Conversation
	if err := store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		c, err := repo.StartConversation(ctx, tx, testUser, "왜 CIS-1.1.1.1 check가 fail이야?")
		conv = c
		return err
	}); err != nil {
		t.Fatalf("StartConversation: %v", err)
	}
	if !strings.HasPrefix(conv.ID, "conv_") {
		t.Errorf("ID = %q, want conv_ prefix", conv.ID)
	}
	if !strings.Contains(conv.Title, "CIS-1.1.1.1") {
		t.Errorf("Title = %q, want question content", conv.Title)
	}
	if emitter.convStarted != 1 {
		t.Errorf("convStarted emit = %d, want 1", emitter.convStarted)
	}
}

func TestAppendTurnAssignsSequenceAndUpdatesConversation(t *testing.T) {
	t.Parallel()
	repo, emitter, store := newTestRepo(t)

	var conv advisor.Conversation
	_ = store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		c, _ := repo.StartConversation(ctx, tx, testUser, "안녕")
		conv = c
		return nil
	})

	// user → assistant 두 turn 추가.
	if err := store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.AppendTurn(ctx, tx, conv.ID, advisor.Turn{
			Role:    advisor.RoleUser,
			Content: "안녕",
		})
		return e
	}); err != nil {
		t.Fatalf("user turn: %v", err)
	}
	if err := store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.AppendTurn(ctx, tx, conv.ID, advisor.Turn{
			Role:         advisor.RoleAssistant,
			Content:      "안녕하세요!",
			LLMProvider:  "ollama",
			LLMModel:     "llama3.2",
			InputTokens:  10,
			OutputTokens: 5,
		})
		return e
	}); err != nil {
		t.Fatalf("assistant turn: %v", err)
	}

	// 조회 → 2 turns + sequence 0,1.
	var turns []advisor.Turn
	if err := store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		_, ts, e := repo.GetConversation(ctx, tx, conv.ID)
		turns = ts
		return e
	}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("len turns = %d, want 2", len(turns))
	}
	if turns[0].Sequence != 0 || turns[1].Sequence != 1 {
		t.Errorf("sequence = %d,%d, want 0,1", turns[0].Sequence, turns[1].Sequence)
	}
	if turns[1].Role != advisor.RoleAssistant {
		t.Errorf("role = %s, want assistant", turns[1].Role)
	}
	// assistant + content → responded emit 1회.
	if emitter.advResponded != 1 {
		t.Errorf("advResponded = %d, want 1", emitter.advResponded)
	}
}

func TestAppendTurnPersistsToolCallsAndAuditEmits(t *testing.T) {
	t.Parallel()
	repo, emitter, store := newTestRepo(t)

	var conv advisor.Conversation
	_ = store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		c, _ := repo.StartConversation(ctx, tx, testUser, "검사")
		conv = c
		return nil
	})

	args, _ := json.Marshal(map[string]string{"checkId": "ck_X"})
	result, _ := json.Marshal(map[string]string{"outcome": "fail", "reason": "redacted"})

	if err := store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.AppendTurn(ctx, tx, conv.ID, advisor.Turn{
			Role: advisor.RoleAssistant,
			ToolCalls: []advisor.ToolCall{
				{ToolName: "get_check", ArgsJSON: args, ResultJSON: result, DurationMs: 12},
				{ToolName: "list_evidence", ArgsJSON: args, ResultJSON: result, DurationMs: 8},
			},
		})
		return e
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if emitter.toolCalled != 2 {
		t.Errorf("toolCalled = %d, want 2", emitter.toolCalled)
	}

	// 회수 검증.
	var turns []advisor.Turn
	_ = store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		_, ts, _ := repo.GetConversation(ctx, tx, conv.ID)
		turns = ts
		return nil
	})
	if len(turns) != 1 {
		t.Fatalf("len = %d, want 1", len(turns))
	}
	if len(turns[0].ToolCalls) != 2 {
		t.Errorf("len tool_calls = %d, want 2", len(turns[0].ToolCalls))
	}
	if turns[0].ToolCalls[0].ToolName != "get_check" {
		t.Errorf("first tool = %s, want get_check", turns[0].ToolCalls[0].ToolName)
	}
}

func TestGetConversationCrossTenantReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	var conv advisor.Conversation
	_ = store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		c, _ := repo.StartConversation(ctx, tx, testUser, "test")
		conv = c
		return nil
	})

	// 다른 tenant context로 조회 → NotFound (격리).
	otherCtx := storage.WithTenantID(context.Background(), "tn_OTHER")
	err := store.Tx(otherCtx, func(ctx context.Context, tx storage.Tx) error {
		_, _, e := repo.GetConversation(ctx, tx, conv.ID)
		return e
	})
	if !errors.Is(err, advisor.ErrConversationNotFound) {
		t.Errorf("err = %v, want ErrConversationNotFound (cross-tenant)", err)
	}
}

func TestListConversationsDESCByUpdated(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	for i, q := range []string{"first", "second", "third"} {
		_ = store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
			_, _ = repo.StartConversation(ctx, tx, testUser, q)
			return nil
		})
		_ = i
		time.Sleep(2 * time.Millisecond)
	}

	var convs []advisor.Conversation
	_ = store.Tx(tenantCtx(), func(ctx context.Context, tx storage.Tx) error {
		out, _ := repo.ListConversations(ctx, tx, testUser, 0)
		convs = out
		return nil
	})
	if len(convs) != 3 {
		t.Fatalf("len = %d, want 3", len(convs))
	}
	// updated_at DESC: third가 첫 번째.
	if !strings.Contains(convs[0].Title, "third") {
		t.Errorf("first = %q, want 'third'", convs[0].Title)
	}
	if !strings.Contains(convs[2].Title, "first") {
		t.Errorf("last = %q, want 'first'", convs[2].Title)
	}
}

func TestStartConversationRequiresTenantContext(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.StartConversation(ctx, tx, testUser, "x")
		return e
	})
	if !errors.Is(err, storage.ErrTenantMissing) {
		t.Errorf("err = %v, want ErrTenantMissing", err)
	}
}
