package sqliterepo_test

// suggestions_repo_test.go — E17 Phase 2 mapping suggestions 단위 테스트.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/domain/compliance/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === fakes ===

type fakeSuggester struct {
	resp compliance.SuggestResponse
	err  error
	last compliance.SuggestRequest
}

func (s *fakeSuggester) Suggest(_ context.Context, req compliance.SuggestRequest) (compliance.SuggestResponse, error) {
	s.last = req
	if s.err != nil {
		return compliance.SuggestResponse{}, s.err
	}
	return s.resp, nil
}

// === harness ===

type sugHarness struct {
	repo      *sqliterepo.Repo
	store     storage.Storage
	emitter   *fakeAuditEmitter
	suggester *fakeSuggester
	tenantID  storage.TenantID
}

func newSuggestionHarness(t *testing.T, suggester *fakeSuggester) *sugHarness {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: filepath.Join(dir, "sug.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	const tenantID = "tn_E17"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'sug-test', 'desktop_free', ?)`,
			tenantID, now)
		return e
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	emitter := &fakeAuditEmitter{}
	deps := sqliterepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		Audit: emitter,
	}
	// suggester == nil이면 interface도 nil로 둠 (typed nil 회피).
	if suggester != nil {
		deps.Suggester = suggester
	}
	repo := sqliterepo.New(deps)
	return &sugHarness{
		repo:      repo,
		store:     store,
		emitter:   emitter,
		suggester: suggester,
		tenantID:  tenantID,
	}
}

func sugCtx(tenantID storage.TenantID) context.Context {
	return storage.WithTenantID(context.Background(), tenantID)
}

// === tests ===

func TestSuggestMappingsPersistsAndAuditEmits(t *testing.T) {
	t.Parallel()
	suggester := &fakeSuggester{
		resp: compliance.SuggestResponse{
			LLMProvider: "anthropic",
			LLMModel:    "claude-3-haiku-20240307",
			Suggestions: []compliance.SuggestionDraft{
				{ControlID: "ISMS-P:2.5.1", Confidence: 0.92, Reasoning: "접근 제어와 매핑"},
				{ControlID: "ISMS-P:2.5.2", Confidence: 0.65, Reasoning: "약한 매칭"},
			},
		},
	}
	h := newSuggestionHarness(t, suggester)

	var produced []compliance.MappingSuggestion
	if err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		out, err := h.repo.SuggestMappings(ctx, tx, compliance.SuggestMappingsRequest{
			CheckCode:      "CIS-1.1.1.1",
			CheckTitle:     "Disable cramfs",
			CheckRationale: "filesystem 모듈 비활성화",
			Framework:      compliance.FrameworkISMSP,
			TopN:           5,
		})
		produced = out
		return err
	}); err != nil {
		t.Fatalf("SuggestMappings: %v", err)
	}
	if len(produced) != 2 {
		t.Fatalf("len = %d, want 2", len(produced))
	}
	if produced[0].LLMProvider != "anthropic" || produced[0].LLMModel != "claude-3-haiku-20240307" {
		t.Errorf("LLM meta not propagated: %+v", produced[0])
	}
	if produced[0].Status != compliance.SuggestionPending {
		t.Errorf("status = %s, want pending", produced[0].Status)
	}

	// Suggester input 검증 — candidates는 LoadFramework 결과를 그대로 전달.
	if len(suggester.last.CandidateControls) == 0 {
		t.Errorf("suggester received no candidates — LoadFramework 호출 누락?")
	}
	if suggester.last.CheckCode != "CIS-1.1.1.1" {
		t.Errorf("suggester.CheckCode = %s", suggester.last.CheckCode)
	}
}

func TestSuggestMappingsDedupsByUniqueConstraint(t *testing.T) {
	t.Parallel()
	suggester := &fakeSuggester{
		resp: compliance.SuggestResponse{
			LLMProvider: "noop",
			Suggestions: []compliance.SuggestionDraft{
				{ControlID: "ISMS-P:2.5.1", Confidence: 0.9, Reasoning: "first"},
			},
		},
	}
	h := newSuggestionHarness(t, suggester)

	for i := 0; i < 2; i++ {
		var out []compliance.MappingSuggestion
		if err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
			r, err := h.repo.SuggestMappings(ctx, tx, compliance.SuggestMappingsRequest{
				CheckCode: "CIS-1.1.1.1",
				Framework: compliance.FrameworkISMSP,
			})
			out = r
			return err
		}); err != nil {
			t.Fatalf("SuggestMappings[%d]: %v", i, err)
		}
		if i == 0 && len(out) != 1 {
			t.Errorf("first call should produce 1, got %d", len(out))
		}
		if i == 1 && len(out) != 0 {
			t.Errorf("second call should dedup (UNIQUE), got %d", len(out))
		}
	}
}

func TestSuggestMappingsReturnsSentinelWhenLLMUnavailable(t *testing.T) {
	t.Parallel()
	h := newSuggestionHarness(t, nil) // nil Suggester → ErrLLMSuggesterUnavailable
	err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.SuggestMappings(ctx, tx, compliance.SuggestMappingsRequest{
			CheckCode: "CIS-1.1.1.1",
			Framework: compliance.FrameworkISMSP,
		})
		return e
	})
	if !errors.Is(err, compliance.ErrLLMSuggesterUnavailable) {
		t.Errorf("err = %v, want ErrLLMSuggesterUnavailable", err)
	}
}

func TestConfirmAndRejectFlowsAuditEmits(t *testing.T) {
	t.Parallel()
	suggester := &fakeSuggester{
		resp: compliance.SuggestResponse{
			LLMProvider: "noop",
			Suggestions: []compliance.SuggestionDraft{
				{ControlID: "ISMS-P:2.5.1", Confidence: 0.9},
				{ControlID: "ISMS-P:2.5.2", Confidence: 0.8},
			},
		},
	}
	h := newSuggestionHarness(t, suggester)

	var produced []compliance.MappingSuggestion
	if err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		r, err := h.repo.SuggestMappings(ctx, tx, compliance.SuggestMappingsRequest{
			CheckCode: "CIS-1.1.1.1",
			Framework: compliance.FrameworkISMSP,
		})
		produced = r
		return err
	}); err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(produced) != 2 {
		t.Fatalf("len = %d, want 2", len(produced))
	}

	// confirm 첫 번째.
	var confirmed compliance.MappingSuggestion
	if err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		c, err := h.repo.ConfirmSuggestion(ctx, tx, produced[0].ID, "user_X")
		confirmed = c
		return err
	}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if confirmed.Status != compliance.SuggestionConfirmed {
		t.Errorf("status = %s, want confirmed", confirmed.Status)
	}
	if confirmed.DecidedBy != "user_X" || confirmed.DecidedAt == nil {
		t.Errorf("DecidedBy/At not set: %+v", confirmed)
	}

	// reject 두 번째.
	if err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.RejectSuggestion(ctx, tx, produced[1].ID, "user_Y")
		return e
	}); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// 두 번째 confirm 시도 → ErrSuggestionAlreadyDecided.
	err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.ConfirmSuggestion(ctx, tx, produced[0].ID, "user_Z")
		return e
	})
	if !errors.Is(err, compliance.ErrSuggestionAlreadyDecided) {
		t.Errorf("err = %v, want ErrSuggestionAlreadyDecided", err)
	}
}

func TestListSuggestionsByFilter(t *testing.T) {
	t.Parallel()
	suggester := &fakeSuggester{
		resp: compliance.SuggestResponse{
			Suggestions: []compliance.SuggestionDraft{
				{ControlID: "ISMS-P:2.5.1", Confidence: 0.9},
				{ControlID: "ISMS-P:2.5.2", Confidence: 0.8},
				{ControlID: "ISMS-P:2.5.3", Confidence: 0.7},
			},
		},
	}
	h := newSuggestionHarness(t, suggester)

	if err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.SuggestMappings(ctx, tx, compliance.SuggestMappingsRequest{
			CheckCode: "CIS-1.1.1.1",
			Framework: compliance.FrameworkISMSP,
		})
		return e
	}); err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	var all []compliance.MappingSuggestion
	if err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.ListSuggestions(ctx, tx, compliance.SuggestionListFilter{})
		all = out
		return e
	}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("len = %d, want 3", len(all))
	}

	var pending []compliance.MappingSuggestion
	if err := h.store.Tx(sugCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.ListSuggestions(ctx, tx, compliance.SuggestionListFilter{
			Status: compliance.SuggestionPending,
		})
		pending = out
		return e
	}); err != nil {
		t.Fatalf("List pending: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("pending = %d, want 3", len(pending))
	}
}
