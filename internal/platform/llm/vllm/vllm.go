// Package vllm은 vLLM (또는 OpenAI-compatible) Inference 서버를 호출하는 LLM Adapter입니다.
//
// 결정 근거 (옵션 C — Ollama edge + vLLM data center 둘 다 driver):
//   - vLLM은 `/v1/chat/completions` OpenAI 호환 엔드포인트를 제공 — 동일 형식의
//     다른 self-hosted server (TGI, Ollama OpenAI-compat 모드, llama.cpp server)도
//     같은 driver로 호출 가능.
//   - SDK 미사용: stdlib net/http만 (P3 에어갭 + P7 단일 바이너리).
//   - streaming 비활성: D-LLM-6 결정 — Phase 8+ 별 epic. 본 driver는 stream=false로
//     동기 호출만, CompleteStream은 Complete 결과를 단일 chunk로 emit.
//   - PII 마스킹: middleware 책임 (D-LLM-4) — 본 driver는 prompt를 그대로 송신.
//   - audit 정책: D-LLM-3 — prompt 미기록, LlmTrace에 provider+token+cost+error만.
//   - 비용: self-hosted → LlmTrace.Cost = 0.
//   - 토큰 카운트: 응답의 `usage.prompt_tokens` / `usage.completion_tokens` 사용.
//   - error 매핑: 401 → ErrUnauthorized, 429 → ErrRateLimited, timeout → ErrTimeout.
package vllm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/platform/llm"
)

// defaultBaseURL은 vLLM serve의 일반적 localhost 기본값입니다.
const defaultBaseURL = "http://localhost:8000"

// defaultHTTPTimeout은 wall-clock 상한입니다 (8B 모델 + GPU 가정).
const defaultHTTPTimeout = 120 * time.Second

// defaultMaxTokens는 caller가 MaxTokens=0을 보냈을 때 fallback입니다.
const defaultMaxTokens = 1024

// Options는 Adapter 생성 옵션입니다.
type Options struct {
	BaseURL      string        // 기본 http://localhost:8000
	APIKey       string        // optional — vLLM은 OpenAI 호환이므로 Authorization Bearer 지원
	DefaultModel string        // request에 model이 비어있을 때 fallback (예: "meta-llama/Llama-3.1-8B-Instruct")
	HTTPTimeout  time.Duration // 0이면 defaultHTTPTimeout
	MaxTokens    int           // 0이면 defaultMaxTokens (request의 MaxTokens=0일 때 fallback)
}

// Adapter는 vLLM HTTP API 어댑터입니다.
type Adapter struct {
	baseURL      string
	apiKey       string
	defaultModel string
	maxTokens    int
	httpClient   *http.Client
}

// New는 옵션 기반으로 Adapter를 만듭니다.
func New(opts Options) *Adapter {
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	timeout := opts.HTTPTimeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	mt := opts.MaxTokens
	if mt <= 0 {
		mt = defaultMaxTokens
	}
	return &Adapter{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       opts.APIKey,
		defaultModel: opts.DefaultModel,
		maxTokens:    mt,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

// Provider는 식별자 "vllm"을 반환합니다.
func (*Adapter) Provider() string { return "vllm" }

// chatRequest는 OpenAI-compatible `POST /v1/chat/completions` body입니다.
type chatRequest struct {
	Model       string       `json:"model"`
	Messages    []apiMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse는 OpenAI-compatible 응답 (필요 필드만)입니다.
type chatResponse struct {
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Complete은 stream=false 동기 호출입니다 (D-LLM-6 streaming 비활성).
func (a *Adapter) Complete(ctx context.Context, req llm.CompleteRequest) (llm.CompleteResponse, error) {
	started := time.Now().UTC()
	model := a.resolveModel(req.Model)

	body, err := a.buildBody(req, model)
	if err != nil {
		return llm.CompleteResponse{Trace: traceErr(model, started, err)}, err
	}
	resp, err := a.do(ctx, body)
	if err != nil {
		return llm.CompleteResponse{Trace: traceErr(model, started, err)}, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		mapped := mapErr(err)
		return llm.CompleteResponse{Trace: traceErr(model, started, mapped)}, mapped
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return llm.CompleteResponse{Trace: traceErr(model, started, err)},
			fmt.Errorf("vllm: parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		err := errors.New("vllm: empty choices in response")
		return llm.CompleteResponse{Trace: traceErr(model, started, err)}, err
	}

	first := parsed.Choices[0]
	stop := mapFinishReason(first.FinishReason)
	trace := llm.LlmTrace{
		Provider:     "vllm",
		Model:        model,
		StartedAt:    started,
		DurationMs:   time.Since(started).Milliseconds(),
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		// Cost = 0 (self-hosted, no provider pricing).
	}
	return llm.CompleteResponse{
		Content:      first.Message.Content,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		StopReason:   stop,
		Trace:        trace,
	}, nil
}

// CompleteStream은 D-LLM-6 결정으로 streaming 비활성 — Complete 결과를 단일 chunk로 emit.
//
// caller가 Adapter interface 일관성을 위해 호출할 수 있음. Phase 8+ 별 epic에서
// 진짜 SSE streaming (`stream=true` + `data: {"choices":[{"delta":...}]}`)으로 교체.
func (a *Adapter) CompleteStream(ctx context.Context, req llm.CompleteRequest) (<-chan llm.StreamChunk, error) {
	out := make(chan llm.StreamChunk, 4)
	go func() {
		defer close(out)
		resp, err := a.Complete(ctx, req)
		if err != nil {
			out <- llm.StreamChunk{Done: true, Err: err, Trace: resp.Trace}
			return
		}
		if resp.Content != "" {
			out <- llm.StreamChunk{Token: resp.Content}
		}
		out <- llm.StreamChunk{Done: true, Trace: resp.Trace}
	}()
	return out, nil
}

// buildBody는 chatRequest를 JSON으로 직렬화합니다.
//
// system/user/assistant 모두 messages 배열에 그대로 넣음 (OpenAI 형식).
func (a *Adapter) buildBody(req llm.CompleteRequest, model string) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = a.maxTokens
	}
	body := chatRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Stream:      false,
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, apiMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, fmt.Errorf("vllm: marshal: %w", err)
	}
	return buf.Bytes(), nil
}

// do는 POST /v1/chat/completions 요청을 보내고 4xx를 sentinel로 매핑합니다.
func (a *Adapter) do(ctx context.Context, body []byte) (*http.Response, error) {
	url := a.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vllm: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	if a.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, mapErr(err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusUnauthorized:
		_ = resp.Body.Close()
		return nil, llm.ErrUnauthorized
	case http.StatusTooManyRequests:
		_ = resp.Body.Close()
		return nil, llm.ErrRateLimited
	default:
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("vllm: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func (a *Adapter) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return a.defaultModel
}

// mapFinishReason은 OpenAI finish_reason을 llm.CompleteResponse.StopReason으로 정규화합니다.
//
// OpenAI 스펙: "stop"|"length"|"content_filter"|"tool_calls".
// llm 스펙: "end_turn"|"max_tokens"|"timeout".
func mapFinishReason(r string) string {
	switch r {
	case "length":
		return "max_tokens"
	case "stop", "":
		return "end_turn"
	default:
		return r
	}
}

// traceErr는 에러 경로의 LlmTrace를 만듭니다.
func traceErr(model string, started time.Time, err error) llm.LlmTrace {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return llm.LlmTrace{
		Provider:   "vllm",
		Model:      model,
		StartedAt:  started,
		DurationMs: time.Since(started).Milliseconds(),
		Error:      msg,
	}
}

// mapErr는 transport-level 에러를 sentinel로 매핑합니다.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return llm.ErrTimeout
	}
	type timeoutErr interface{ Timeout() bool }
	var te timeoutErr
	if errors.As(err, &te) && te.Timeout() {
		return llm.ErrTimeout
	}
	return err
}
