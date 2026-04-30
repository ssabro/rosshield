// Package anthropic는 Anthropic Messages API를 호출하는 LLM Adapter입니다 (옵트인, R14-1).
//
// 결정 근거:
//   - SDK 미사용: stdlib net/http + 직접 SSE 파싱 (P3 에어갭 + P7 단일 바이너리).
//   - 가격 추정: claude-3-haiku-20240307만 정확 (input $0.25/MT, output $1.25/MT).
//     다른 모델은 보수적으로 0 — 시간이 지나며 가격표가 흔들리므로 caller가 별도 추정.
//   - SSE 스펙: `event: <name>\ndata: <json>\n\n` — content_block_delta 안의
//     text_delta.text를 누적, message_start의 input_tokens·message_delta의 output_tokens를 LlmTrace에 채움.
//   - 401 → ErrUnauthorized, 429 → ErrRateLimited, 그 외 4xx/5xx는 raw error.
package anthropic

import (
	"bufio"
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

// defaultBaseURL은 Anthropic API 기본 엔드포인트입니다.
const defaultBaseURL = "https://api.anthropic.com"

// defaultHTTPTimeout은 단일 요청의 wall-clock 상한입니다.
const defaultHTTPTimeout = 60 * time.Second

// anthropicVersion은 메시지 API 헤더에 보낼 스펙 버전입니다.
const anthropicVersion = "2023-06-01"

// defaultMaxTokens는 caller가 MaxTokens=0을 보냈을 때 보수 fallback입니다.
const defaultMaxTokens = 1024

// Options는 Adapter 생성 옵션입니다.
type Options struct {
	APIKey       string
	BaseURL      string        // 기본 https://api.anthropic.com
	DefaultModel string        // request에 model이 비어있을 때 fallback
	HTTPTimeout  time.Duration // 0이면 defaultHTTPTimeout
}

// Adapter는 Anthropic Messages API 어댑터입니다.
type Adapter struct {
	apiKey       string
	baseURL      string
	defaultModel string
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
	return &Adapter{
		apiKey:       opts.APIKey,
		baseURL:      strings.TrimRight(baseURL, "/"),
		defaultModel: opts.DefaultModel,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

// Provider는 식별자 "anthropic"을 반환합니다.
func (*Adapter) Provider() string { return "anthropic" }

// messagesRequest는 `POST /v1/messages` body입니다.
type messagesRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	Messages    []apiMessage `json:"messages"`
	System      string       `json:"system,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Complete은 stream=false 동기 호출입니다.
func (a *Adapter) Complete(ctx context.Context, req llm.CompleteRequest) (llm.CompleteResponse, error) {
	started := time.Now().UTC()
	model := a.resolveModel(req.Model)

	body, err := a.buildBody(req, model, false)
	if err != nil {
		return llm.CompleteResponse{}, err
	}
	resp, err := a.do(ctx, body)
	if err != nil {
		return llm.CompleteResponse{
			Trace: traceErr(model, started, err),
		}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		mapped := mapErr(err)
		return llm.CompleteResponse{Trace: traceErr(model, started, mapped)}, mapped
	}
	var parsed messagesResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return llm.CompleteResponse{Trace: traceErr(model, started, err)}, fmt.Errorf("anthropic: parse response: %w", err)
	}

	var content strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			content.WriteString(c.Text)
		}
	}
	cost := estimateCost(model, parsed.Usage.InputTokens, parsed.Usage.OutputTokens)
	stop := parsed.StopReason
	if stop == "" {
		stop = "end_turn"
	}
	trace := llm.LlmTrace{
		Provider:     "anthropic",
		Model:        model,
		StartedAt:    started,
		DurationMs:   time.Since(started).Milliseconds(),
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		Cost:         cost,
	}
	return llm.CompleteResponse{
		Content:      content.String(),
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		StopReason:   stop,
		Trace:        trace,
	}, nil
}

// CompleteStream은 stream=true SSE 호출 — content_block_delta의 text_delta를 누적해 chunk로 흘립니다.
func (a *Adapter) CompleteStream(ctx context.Context, req llm.CompleteRequest) (<-chan llm.StreamChunk, error) {
	started := time.Now().UTC()
	model := a.resolveModel(req.Model)

	body, err := a.buildBody(req, model, true)
	if err != nil {
		return nil, err
	}
	resp, err := a.do(ctx, body)
	if err != nil {
		return nil, err
	}

	out := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		var (
			inputTokens  int
			outputTokens int
		)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			// 우리는 event: 타입 파싱은 생략 — data JSON의 type 필드로 분기 가능.
			var evt struct {
				Type    string `json:"type"`
				Message struct {
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
				Delta struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				out <- llm.StreamChunk{Done: true, Err: fmt.Errorf("anthropic: parse SSE: %w", err)}
				return
			}
			switch evt.Type {
			case "message_start":
				inputTokens = evt.Message.Usage.InputTokens
				if evt.Message.Usage.OutputTokens > 0 {
					outputTokens = evt.Message.Usage.OutputTokens
				}
			case "content_block_delta":
				if evt.Delta.Type == "text_delta" && evt.Delta.Text != "" {
					out <- llm.StreamChunk{Token: evt.Delta.Text}
				}
			case "message_delta":
				if evt.Usage.OutputTokens > 0 {
					outputTokens = evt.Usage.OutputTokens
				}
			case "message_stop":
				// stream 종료 — Done chunk는 루프 후 송출.
			}
		}
		if err := scanner.Err(); err != nil {
			mapped := mapErr(err)
			out <- llm.StreamChunk{
				Done:  true,
				Err:   mapped,
				Trace: traceErr(model, started, mapped),
			}
			return
		}
		out <- llm.StreamChunk{
			Done: true,
			Trace: llm.LlmTrace{
				Provider:     "anthropic",
				Model:        model,
				StartedAt:    started,
				DurationMs:   time.Since(started).Milliseconds(),
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				Cost:         estimateCost(model, inputTokens, outputTokens),
			},
		}
	}()
	return out, nil
}

// buildBody는 messagesRequest를 JSON으로 직렬화합니다.
//
// system role은 별도 `system` 필드로 분리(Anthropic 스펙). user/assistant는 messages 배열에.
func (a *Adapter) buildBody(req llm.CompleteRequest, model string, stream bool) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	body := messagesRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Stream:      stream,
		Temperature: req.Temperature,
	}
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleSystem:
			// 마지막 system이 우선 — 보수적으로 join도 가능하나 단순화.
			if body.System == "" {
				body.System = m.Content
			} else {
				body.System += "\n\n" + m.Content
			}
		case llm.RoleUser, llm.RoleAssistant:
			body.Messages = append(body.Messages, apiMessage{Role: string(m.Role), Content: m.Content})
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, fmt.Errorf("anthropic: marshal: %w", err)
	}
	return buf.Bytes(), nil
}

// do는 POST /v1/messages 요청을 보내고 4xx를 sentinel로 매핑합니다.
func (a *Adapter) do(ctx context.Context, body []byte) (*http.Response, error) {
	url := a.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

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
		return nil, fmt.Errorf("anthropic: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func (a *Adapter) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return a.defaultModel
}

// estimateCost는 알려진 모델만 가격을 적용하고, 그 외엔 0을 반환합니다.
//
// 단가 단위: USD per 1M tokens (MT). caller(Insight·Advisor)는 LlmTrace.Cost 합산으로
// R14-6 일일 한도 모니터링에 사용합니다.
func estimateCost(model string, inputTokens, outputTokens int) float64 {
	type price struct{ inPerMT, outPerMT float64 }
	table := map[string]price{
		"claude-3-haiku-20240307": {inPerMT: 0.25, outPerMT: 1.25},
	}
	p, ok := table[model]
	if !ok {
		return 0
	}
	return float64(inputTokens)/1_000_000*p.inPerMT + float64(outputTokens)/1_000_000*p.outPerMT
}

// traceErr는 에러 경로의 LlmTrace를 만듭니다.
func traceErr(model string, started time.Time, err error) llm.LlmTrace {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return llm.LlmTrace{
		Provider:   "anthropic",
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
