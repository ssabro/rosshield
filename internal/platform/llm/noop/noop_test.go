package noop_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/llm"
	"github.com/ssabro/rosshield/internal/platform/llm/noop"
)

func TestNoopProviderName(t *testing.T) {
	a := noop.New()
	if got := a.Provider(); got != "noop" {
		t.Fatalf("Provider()=%q, want noop", got)
	}
}

func TestNoopReturnsErrLLMDisabled(t *testing.T) {
	a := noop.New()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	t.Run("Complete", func(t *testing.T) {
		resp, err := a.Complete(ctx, llm.CompleteRequest{
			TenantID: "tn_1",
			Model:    "any",
			Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		})
		if !errors.Is(err, llm.ErrLLMDisabled) {
			t.Fatalf("err=%v, want ErrLLMDisabled", err)
		}
		if resp.Content != "" {
			t.Fatalf("content=%q, want empty", resp.Content)
		}
		if resp.Trace.Provider != "noop" {
			t.Fatalf("trace.Provider=%q, want noop", resp.Trace.Provider)
		}
		if resp.Trace.Error == "" {
			t.Fatalf("trace.Error empty, want ErrLLMDisabled message")
		}
		if resp.Trace.Cost != 0 {
			t.Fatalf("cost=%v, want 0", resp.Trace.Cost)
		}
	})

	t.Run("CompleteStream", func(t *testing.T) {
		ch, err := a.CompleteStream(ctx, llm.CompleteRequest{
			TenantID: "tn_1",
			Model:    "any",
			Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("CompleteStream err=%v, want nil (channel-delivered error)", err)
		}
		var chunks []llm.StreamChunk
		for c := range ch {
			chunks = append(chunks, c)
		}
		if len(chunks) != 1 {
			t.Fatalf("got %d chunks, want 1", len(chunks))
		}
		final := chunks[0]
		if !final.Done {
			t.Fatalf("final.Done=false, want true")
		}
		if !errors.Is(final.Err, llm.ErrLLMDisabled) {
			t.Fatalf("final.Err=%v, want ErrLLMDisabled", final.Err)
		}
		if final.Trace.Provider != "noop" {
			t.Fatalf("trace.Provider=%q, want noop", final.Trace.Provider)
		}
	})
}
