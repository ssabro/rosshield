package vllm_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/llm"
	"github.com/ssabro/rosshield/internal/platform/llm/vllm"
)

// chatJSONResponse는 OpenAI-compatible chat 응답을 단일 JSON으로 반환합니다.
func chatJSONResponse(w http.ResponseWriter, content string, finishReason string, promptTokens, completionTokens int) {
	body := map[string]any{
		"id":      "cmpl-test",
		"object":  "chat.completion",
		"created": 1700000000,
		"model":   "meta-llama/Llama-3.1-8B-Instruct",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": finishReason,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func TestVLLMCompleteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path=%q, want /v1/chat/completions", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%q, want POST", r.Method)
		}
		if ct := r.Header.Get("content-type"); ct != "application/json" {
			t.Errorf("content-type=%q, want application/json", ct)
		}
		chatJSONResponse(w, "Hello world!", "stop", 10, 5)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL, DefaultModel: "meta-llama/Llama-3.1-8B-Instruct"})
	resp, err := a.Complete(context.Background(), llm.CompleteRequest{
		TenantID: "tn_1",
		Model:    "meta-llama/Llama-3.1-8B-Instruct",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be terse"},
			{Role: llm.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "Hello world!" {
		t.Fatalf("content=%q, want %q", resp.Content, "Hello world!")
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("stop=%q, want end_turn (stop → end_turn)", resp.StopReason)
	}
	if resp.InputTokens != 10 || resp.OutputTokens != 5 {
		t.Fatalf("tokens=(%d,%d), want (10,5)", resp.InputTokens, resp.OutputTokens)
	}
	if resp.Trace.Provider != "vllm" {
		t.Fatalf("provider=%q, want vllm", resp.Trace.Provider)
	}
	if resp.Trace.Cost != 0 {
		t.Fatalf("cost=%v, want 0 (self-hosted)", resp.Trace.Cost)
	}
}

func TestVLLMFinishReasonLengthMapsToMaxTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chatJSONResponse(w, "truncated", "length", 5, 3)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	resp, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.StopReason != "max_tokens" {
		t.Fatalf("stop=%q, want max_tokens", resp.StopReason)
	}
}

func TestVLLMUnauthorizedMapsToErrUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL, APIKey: "wrong"})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if !errors.Is(err, llm.ErrUnauthorized) {
		t.Fatalf("err=%v, want ErrUnauthorized", err)
	}
}

func TestVLLMRateLimitMapsToErrRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if !errors.Is(err, llm.ErrRateLimited) {
		t.Fatalf("err=%v, want ErrRateLimited", err)
	}
}

func TestVLLM5xxReturnsRawError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"OOM"}`))
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if err == nil {
		t.Fatal("err=nil, want error")
	}
	if !strings.Contains(err.Error(), "http 500") {
		t.Fatalf("err=%v, want substring 'http 500'", err)
	}
}

func TestVLLMMalformedJSONReturnsParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if err == nil {
		t.Fatal("err=nil, want parse error")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Fatalf("err=%v, want substring 'parse response'", err)
	}
}

func TestVLLMEmptyChoicesReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`))
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if err == nil {
		t.Fatal("err=nil, want empty-choices error")
	}
	if !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("err=%v, want substring 'empty choices'", err)
	}
}

func TestVLLMTimeoutReturnsErrTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL, HTTPTimeout: 50 * time.Millisecond})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if !errors.Is(err, llm.ErrTimeout) {
		t.Fatalf("err=%v, want ErrTimeout", err)
	}
}

func TestVLLMContextCancellation(t *testing.T) {
	// Handler가 client 요청 끝까지 block — client가 context cancel하면 connection이
	// 끊기고 r.Context().Done()이 닫혀 handler가 return → srv.Close()가 정리 가능.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second): // safety guard against hang.
		}
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := a.Complete(ctx, llm.CompleteRequest{Model: "m"})
	if err == nil {
		t.Fatal("err=nil, want cancellation error")
	}
}

func TestVLLMMaxTokensAppliedFromRequest(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		chatJSONResponse(w, "ok", "stop", 1, 1)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{
		Model:     "m",
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(captured, `"max_tokens":256`) {
		t.Fatalf("body missing max_tokens=256: %s", captured)
	}
	if !strings.Contains(captured, `"stream":false`) {
		t.Fatalf("body missing stream=false (D-LLM-6): %s", captured)
	}
}

func TestVLLMMaxTokensFallsBackToAdapterDefault(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		chatJSONResponse(w, "ok", "stop", 1, 1)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL, MaxTokens: 512})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(captured, `"max_tokens":512`) {
		t.Fatalf("body missing adapter-default max_tokens=512: %s", captured)
	}
}

func TestVLLMAPIKeyHeaderSent(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		chatJSONResponse(w, "ok", "stop", 1, 1)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL, APIKey: "sk-test"})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if seenAuth != "Bearer sk-test" {
		t.Fatalf("Authorization=%q, want %q", seenAuth, "Bearer sk-test")
	}
}

func TestVLLMOmitsAuthHeaderWhenNoAPIKey(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		chatJSONResponse(w, "ok", "stop", 1, 1)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "m"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if seenAuth != "" {
		t.Fatalf("Authorization=%q, want empty (no API key)", seenAuth)
	}
}

func TestVLLMMessagesSerialization(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		chatJSONResponse(w, "ok", "stop", 1, 1)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	_, err := a.Complete(context.Background(), llm.CompleteRequest{
		Model: "m",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be terse"},
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, Content: "hello"},
			{Role: llm.RoleUser, Content: "bye"},
		},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	msgs, ok := captured["messages"].([]any)
	if !ok {
		t.Fatalf("messages not array: %T", captured["messages"])
	}
	if len(msgs) != 4 {
		t.Fatalf("messages=%d, want 4", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be terse" {
		t.Fatalf("first msg=%v, want system/be terse", first)
	}
}

func TestVLLMProviderAndCompleteStreamShim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chatJSONResponse(w, "ABC", "stop", 2, 3)
	}))
	defer srv.Close()

	a := vllm.New(vllm.Options{BaseURL: srv.URL})
	if a.Provider() != "vllm" {
		t.Fatalf("provider=%q, want vllm", a.Provider())
	}
	ch, err := a.CompleteStream(context.Background(), llm.CompleteRequest{Model: "m"})
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
	if strings.Join(tokens, "") != "ABC" {
		t.Fatalf("tokens=%v, want ABC (single chunk shim)", tokens)
	}
	if !final.Done {
		t.Fatal("no final Done chunk")
	}
	if final.Trace.OutputTokens != 3 {
		t.Fatalf("trace.OutputTokens=%d, want 3", final.Trace.OutputTokens)
	}
}
