package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/llm"
	"github.com/ssabro/rosshield/internal/platform/llm/anthropic"
)

func TestAnthropicCompleteParsesNonStreamingResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path=%q, want /v1/messages", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
		  "id": "msg_1",
		  "type": "message",
		  "role": "assistant",
		  "content": [{"type":"text","text":"Hello there"}],
		  "model": "claude-3-haiku-20240307",
		  "stop_reason": "end_turn",
		  "usage": {"input_tokens": 12, "output_tokens": 5}
		}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "test", BaseURL: srv.URL})
	resp, err := a.Complete(context.Background(), llm.CompleteRequest{
		TenantID: "tn_1",
		Model:    "claude-3-haiku-20240307",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "Hello there" {
		t.Fatalf("content=%q", resp.Content)
	}
	if resp.InputTokens != 12 || resp.OutputTokens != 5 {
		t.Fatalf("tokens=(%d,%d)", resp.InputTokens, resp.OutputTokens)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("stop=%q", resp.StopReason)
	}
	if resp.Trace.Provider != "anthropic" {
		t.Fatalf("trace.Provider=%q", resp.Trace.Provider)
	}
}

func TestAnthropicAuthHeadersIncluded(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "sk-test", BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "claude-3-haiku-20240307"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.Get("x-api-key") != "sk-test" {
		t.Fatalf("x-api-key=%q", got.Get("x-api-key"))
	}
	if got.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("anthropic-version=%q", got.Get("anthropic-version"))
	}
	if got.Get("content-type") != "application/json" {
		t.Fatalf("content-type=%q", got.Get("content-type"))
	}
}

func TestAnthropicCostEstimationForHaiku(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1000000,"output_tokens":1000000}}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "claude-3-haiku-20240307"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	// Haiku: input $0.25/MT, output $1.25/MT → 1M+1M = $1.50.
	want := 1.50
	if resp.Trace.Cost < want-0.001 || resp.Trace.Cost > want+0.001 {
		t.Fatalf("cost=%v, want ~%v", resp.Trace.Cost, want)
	}
}

func TestAnthropicCostUnknownModelIsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1000,"output_tokens":1000}}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "claude-future-9000"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Trace.Cost != 0 {
		t.Fatalf("cost=%v, want 0 (unknown model)", resp.Trace.Cost)
	}
}

func TestAnthropic401ReturnsErrUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "bad", BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "claude-3-haiku-20240307"})
	if !errors.Is(err, llm.ErrUnauthorized) {
		t.Fatalf("err=%v, want ErrUnauthorized", err)
	}
}

func TestAnthropic429ReturnsErrRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error"}}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "k", BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "claude-3-haiku-20240307"})
	if !errors.Is(err, llm.ErrRateLimited) {
		t.Fatalf("err=%v, want ErrRateLimited", err)
	}
}

func TestAnthropicCompleteStreamParsesSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush := func() {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		send := func(event, data string) {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flush()
		}
		send("message_start", `{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":7,"output_tokens":0}}}`)
		send("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		send("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`)
		send("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`)
		send("content_block_stop", `{"type":"content_block_stop","index":0}`)
		send("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`)
		send("message_stop", `{"type":"message_stop"}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "k", BaseURL: srv.URL})
	ch, err := a.CompleteStream(context.Background(), llm.CompleteRequest{
		Model:    "claude-3-haiku-20240307",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}

	var tokens []string
	var final llm.StreamChunk
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("chunk err=%v", c.Err)
		}
		if c.Done {
			final = c
			continue
		}
		tokens = append(tokens, c.Token)
	}
	if got := strings.Join(tokens, ""); got != "Hello" {
		t.Fatalf("tokens=%q, want Hello", got)
	}
	if !final.Done {
		t.Fatalf("no final Done chunk")
	}
	if final.Trace.InputTokens != 7 || final.Trace.OutputTokens != 3 {
		t.Fatalf("trace tokens=(%d,%d)", final.Trace.InputTokens, final.Trace.OutputTokens)
	}
	if final.Trace.Provider != "anthropic" {
		t.Fatalf("trace.Provider=%q", final.Trace.Provider)
	}
}

func TestAnthropicTimeoutReturnsErrTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "k", BaseURL: srv.URL, HTTPTimeout: 50 * time.Millisecond})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "claude-3-haiku-20240307"})
	if !errors.Is(err, llm.ErrTimeout) {
		t.Fatalf("err=%v, want ErrTimeout", err)
	}
}

func TestAnthropicSerializesMessagesAndSystemPrompt(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "k", BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{
		Model: "claude-3-haiku-20240307",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be terse"},
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, Content: "hello"},
			{Role: llm.RoleUser, Content: "more?"},
		},
		Temperature: 0.3,
		MaxTokens:   200,
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got, _ := captured["model"].(string); got != "claude-3-haiku-20240307" {
		t.Fatalf("model=%v", captured["model"])
	}
	if got, _ := captured["system"].(string); got != "be terse" {
		t.Fatalf("system=%v", captured["system"])
	}
	if got, _ := captured["max_tokens"].(float64); got != 200 {
		t.Fatalf("max_tokens=%v", captured["max_tokens"])
	}
	if got, _ := captured["temperature"].(float64); got != 0.3 {
		t.Fatalf("temperature=%v", captured["temperature"])
	}
	msgs, _ := captured["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages len=%d, want 3 (system stripped)", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "hi" {
		t.Fatalf("first msg=%v", first)
	}
	second, _ := msgs[1].(map[string]any)
	if second["role"] != "assistant" || second["content"] != "hello" {
		t.Fatalf("second msg=%v", second)
	}
}
