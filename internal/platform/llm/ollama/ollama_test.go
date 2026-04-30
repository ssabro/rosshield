package ollama_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/llm"
	"github.com/ssabro/rosshield/internal/platform/llm/ollama"
)

// ndjsonResponse 는 ollama generate 엔드포인트가 보내는 NDJSON 라인 시퀀스를 흉내 냅니다.
func ndjsonResponse(w http.ResponseWriter, lines []string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	for _, l := range lines {
		fmt.Fprintln(w, l)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func TestOllamaCompleteAggregatesStreamingResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%q, want POST", r.Method)
		}
		ndjsonResponse(w, []string{
			`{"response":"Hello","done":false}`,
			`{"response":" world","done":false}`,
			`{"response":"!","done":true,"prompt_eval_count":7,"eval_count":3}`,
		})
	}))
	defer srv.Close()

	a := ollama.New(ollama.Options{Endpoint: srv.URL, DefaultModel: "llama3.2"})
	resp, err := a.Complete(context.Background(), llm.CompleteRequest{
		TenantID: "tn_1",
		Model:    "llama3.2",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "Hello world!" {
		t.Fatalf("content=%q, want %q", resp.Content, "Hello world!")
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("stopReason=%q, want end_turn", resp.StopReason)
	}
}

func TestOllamaTraceCapturesTokenCounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ndjsonResponse(w, []string{
			`{"response":"answer","done":true,"prompt_eval_count":42,"eval_count":17}`,
		})
	}))
	defer srv.Close()

	a := ollama.New(ollama.Options{Endpoint: srv.URL})
	resp, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Trace.Provider != "ollama" {
		t.Fatalf("provider=%q, want ollama", resp.Trace.Provider)
	}
	if resp.Trace.InputTokens != 42 || resp.Trace.OutputTokens != 17 {
		t.Fatalf("tokens=(%d,%d), want (42,17)", resp.Trace.InputTokens, resp.Trace.OutputTokens)
	}
	if resp.Trace.Cost != 0 {
		t.Fatalf("cost=%v, want 0 (local ollama free)", resp.Trace.Cost)
	}
	if resp.Trace.Model != "llama3.2" {
		t.Fatalf("trace model=%q, want llama3.2", resp.Trace.Model)
	}
}

func TestOllamaTimeoutReturnsErrTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// 클라이언트 타임아웃이 만료되도록 지연.
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	a := ollama.New(ollama.Options{Endpoint: srv.URL, HTTPTimeout: 50 * time.Millisecond})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "llama3.2"})
	if !errors.Is(err, llm.ErrTimeout) {
		t.Fatalf("err=%v, want ErrTimeout", err)
	}
}

func TestOllamaCompleteStreamYieldsTokensInOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ndjsonResponse(w, []string{
			`{"response":"A","done":false}`,
			`{"response":"B","done":false}`,
			`{"response":"C","done":true,"prompt_eval_count":1,"eval_count":3}`,
		})
	}))
	defer srv.Close()

	a := ollama.New(ollama.Options{Endpoint: srv.URL})
	ch, err := a.CompleteStream(context.Background(), llm.CompleteRequest{Model: "llama3.2"})
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
	if got := strings.Join(tokens, ""); got != "ABC" {
		t.Fatalf("tokens=%q, want ABC", got)
	}
	if !final.Done {
		t.Fatalf("no final Done chunk")
	}
	if final.Trace.OutputTokens != 3 {
		t.Fatalf("trace.OutputTokens=%d, want 3", final.Trace.OutputTokens)
	}
}

func TestOllamaSerializesMessagesAsChatTemplate(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		ndjsonResponse(w, []string{`{"response":"ok","done":true}`})
	}))
	defer srv.Close()

	a := ollama.New(ollama.Options{Endpoint: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{
		Model: "llama3.2",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be terse"},
			{Role: llm.RoleUser, Content: "hi"},
		},
		Temperature: 0.2,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	for _, want := range []string{`"model":"llama3.2"`, `<|system|>`, `be terse`, `<|user|>`, `hi`, `"stream":true`} {
		if !strings.Contains(captured, want) {
			t.Fatalf("body missing %q\nbody=%s", want, captured)
		}
	}
}
