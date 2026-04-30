package llmmapper_test

// mapper_test.go — E17-B Suggester 단위 테스트 (mock LLM 어댑터 기반).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/app/llmmapper"
	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/platform/llm"
)

// === fakes ===

type fakeAdapter struct {
	provider string
	model    string
	resp     string
	err      error
	lastReq  llm.CompleteRequest
}

func (f *fakeAdapter) Provider() string { return f.provider }

func (f *fakeAdapter) Complete(_ context.Context, req llm.CompleteRequest) (llm.CompleteResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return llm.CompleteResponse{Trace: llm.LlmTrace{Provider: f.provider, Model: f.model, Error: f.err.Error()}}, f.err
	}
	return llm.CompleteResponse{
		Content: f.resp,
		Trace: llm.LlmTrace{
			Provider:   f.provider,
			Model:      f.model,
			StartedAt:  time.Now().UTC(),
			DurationMs: 50,
		},
	}, nil
}

func (f *fakeAdapter) CompleteStream(_ context.Context, _ llm.CompleteRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

// === tests ===

func TestSuggestParsesJSONArrayAndSortsByConfidence(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{
		provider: "anthropic",
		model:    "claude-3-haiku-20240307",
		resp: `Here is the result:
[
  {"controlId":"C2","confidence":0.65,"reasoning":"weak match"},
  {"controlId":"C1","confidence":0.92,"reasoning":"strong match"},
  {"controlId":"C3","confidence":0.40,"reasoning":"low"}
]`,
	}
	s := llmmapper.New(adapter, "claude-3-haiku-20240307")

	resp, err := s.Suggest(context.Background(), compliance.SuggestRequest{
		CheckCode: "CIS-1.1.1.1",
		Framework: compliance.FrameworkISMSP,
		CandidateControls: []compliance.CandidateControl{
			{ID: "C1", Title: "ctl1"}, {ID: "C2", Title: "ctl2"}, {ID: "C3", Title: "ctl3"},
		},
		TopN: 5,
	})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(resp.Suggestions) != 3 {
		t.Fatalf("len = %d, want 3", len(resp.Suggestions))
	}
	if resp.Suggestions[0].ControlID != "C1" || resp.Suggestions[0].Confidence != 0.92 {
		t.Errorf("first = %+v, want C1/0.92", resp.Suggestions[0])
	}
	if resp.Suggestions[2].ControlID != "C3" {
		t.Errorf("last = %s, want C3", resp.Suggestions[2].ControlID)
	}
	if resp.LLMProvider != "anthropic" || resp.LLMModel != "claude-3-haiku-20240307" {
		t.Errorf("trace propagation failed: %+v", resp)
	}
}

func TestSuggestRespectsTopNLimit(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{
		provider: "ollama",
		resp: `[
  {"controlId":"A","confidence":0.9},
  {"controlId":"B","confidence":0.8},
  {"controlId":"C","confidence":0.7},
  {"controlId":"D","confidence":0.6}
]`,
	}
	s := llmmapper.New(adapter, "")

	resp, err := s.Suggest(context.Background(), compliance.SuggestRequest{
		CheckCode: "X",
		Framework: compliance.FrameworkNIST,
		TopN:      2,
	})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(resp.Suggestions) != 2 {
		t.Errorf("len = %d, want 2 (TopN limit)", len(resp.Suggestions))
	}
}

func TestSuggestPropagatesLLMDisabledError(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{
		provider: "noop",
		err:      llm.ErrLLMDisabled,
	}
	s := llmmapper.New(adapter, "")
	_, err := s.Suggest(context.Background(), compliance.SuggestRequest{
		CheckCode: "X",
		Framework: compliance.FrameworkISMSP,
	})
	if !errors.Is(err, llm.ErrLLMDisabled) {
		t.Errorf("err = %v, want ErrLLMDisabled propagated", err)
	}
}

func TestSuggestReturnsErrorOnInvalidJSON(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{
		provider: "ollama",
		resp:     "I'm sorry, I cannot help with that.",
	}
	s := llmmapper.New(adapter, "")
	_, err := s.Suggest(context.Background(), compliance.SuggestRequest{
		CheckCode: "X",
		Framework: compliance.FrameworkISMSP,
	})
	if err == nil {
		t.Errorf("err is nil, want parse failure")
	}
	if !strings.Contains(err.Error(), "no JSON array") {
		t.Errorf("err = %v, want 'no JSON array' message", err)
	}
}

func TestSuggestClampsConfidenceTo01Range(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{
		provider: "ollama",
		resp: `[
  {"controlId":"A","confidence":1.5},
  {"controlId":"B","confidence":-0.3}
]`,
	}
	s := llmmapper.New(adapter, "")
	resp, err := s.Suggest(context.Background(), compliance.SuggestRequest{
		CheckCode: "X",
		Framework: compliance.FrameworkISMSP,
		TopN:      5,
	})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if resp.Suggestions[0].Confidence != 1.0 {
		t.Errorf("Confidence not clamped to 1.0: %v", resp.Suggestions[0].Confidence)
	}
	// 두 번째는 0으로 clamp.
	for _, sug := range resp.Suggestions {
		if sug.ControlID == "B" && sug.Confidence != 0 {
			t.Errorf("B Confidence = %v, want 0 (clamped)", sug.Confidence)
		}
	}
}

func TestSuggestPromptIncludesCheckMeta(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{
		provider: "ollama",
		resp:     "[]",
	}
	s := llmmapper.New(adapter, "")
	_, err := s.Suggest(context.Background(), compliance.SuggestRequest{
		CheckCode:      "CIS-1.2.3",
		CheckTitle:     "Test title",
		CheckRationale: "Test rationale",
		Framework:      compliance.FrameworkISMSP,
		CandidateControls: []compliance.CandidateControl{
			{ID: "C1", Title: "ctl1", Summary: "summary"},
		},
	})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	userMsg := adapter.lastReq.Messages[1].Content
	for _, want := range []string{"CIS-1.2.3", "Test title", "Test rationale", "C1", "summary"} {
		if !strings.Contains(userMsg, want) {
			t.Errorf("prompt missing %q: %s", want, userMsg)
		}
	}
}
