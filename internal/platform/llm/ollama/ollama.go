// Package ollama는 로컬 Ollama HTTP API를 호출하는 LLM Adapter입니다 (옵트인, R14-1).
//
// 결정 근거:
//   - 에어갭 친화: 로컬 daemon (기본 http://localhost:11434), 외부 SDK 없음 (P3·P7).
//   - NDJSON 스트리밍: `POST /api/generate`는 line-by-line JSON을 보내므로
//     Complete은 내부 누적, CompleteStream은 그대로 전달.
//   - chat template: Messages를 `<|role|>content` 라인으로 직렬화 — 모델별 차이는
//     무시(qwen·llama 모두 동일 prompt format으로 충분히 동작).
//   - 비용: 로컬 무료 → LlmTrace.Cost = 0.
//   - 토큰 카운트: ollama가 prompt_eval_count·eval_count로 보고하므로 그대로 사용.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/platform/llm"
)

// defaultEndpoint는 ollama daemon의 기본 주소입니다.
const defaultEndpoint = "http://localhost:11434"

// defaultHTTPTimeout은 요청 전체(stream 포함) 상한입니다.
const defaultHTTPTimeout = 60 * time.Second

// Options는 Adapter 생성 옵션입니다.
type Options struct {
	Endpoint     string        // 기본 http://localhost:11434
	DefaultModel string        // request에 model이 비어있을 때 fallback (예: "llama3.2")
	HTTPTimeout  time.Duration // 0이면 defaultHTTPTimeout
}

// Adapter는 ollama HTTP API 호출 어댑터입니다.
type Adapter struct {
	endpoint     string
	defaultModel string
	httpClient   *http.Client
}

// New는 옵션 기반으로 Adapter를 만듭니다.
func New(opts Options) *Adapter {
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	timeout := opts.HTTPTimeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return &Adapter{
		endpoint:     strings.TrimRight(endpoint, "/"),
		defaultModel: opts.DefaultModel,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

// Provider는 식별자 "ollama"를 반환합니다.
func (*Adapter) Provider() string { return "ollama" }

// generateRequest는 ollama `POST /api/generate` body입니다.
type generateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

// generateLine는 NDJSON 응답의 한 라인 스키마입니다 (필요 필드만).
type generateLine struct {
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	DoneReason      string `json:"done_reason"`
}

// Complete은 NDJSON 스트림을 내부 누적해 단일 응답으로 돌려줍니다.
func (a *Adapter) Complete(ctx context.Context, req llm.CompleteRequest) (llm.CompleteResponse, error) {
	started := time.Now().UTC()
	model := a.resolveModel(req.Model)

	resp, finalLine, err := a.openStream(ctx, req, model)
	if err != nil {
		return llm.CompleteResponse{
			Trace: llm.LlmTrace{
				Provider:   "ollama",
				Model:      model,
				StartedAt:  started,
				DurationMs: time.Since(started).Milliseconds(),
				Error:      err.Error(),
			},
		}, err
	}
	defer resp.Body.Close()

	var (
		buf      bytes.Buffer
		gotFinal generateLine
	)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var gl generateLine
		if err := json.Unmarshal(line, &gl); err != nil {
			return llm.CompleteResponse{}, fmt.Errorf("ollama: parse ndjson line: %w", err)
		}
		buf.WriteString(gl.Response)
		if gl.Done {
			gotFinal = gl
			break
		}
	}
	if err := scanner.Err(); err != nil {
		mapped := mapErr(err)
		return llm.CompleteResponse{
			Trace: llm.LlmTrace{
				Provider:   "ollama",
				Model:      model,
				StartedAt:  started,
				DurationMs: time.Since(started).Milliseconds(),
				Error:      mapped.Error(),
			},
		}, mapped
	}
	_ = finalLine

	stop := "end_turn"
	if gotFinal.DoneReason == "length" {
		stop = "max_tokens"
	}
	trace := llm.LlmTrace{
		Provider:     "ollama",
		Model:        model,
		StartedAt:    started,
		DurationMs:   time.Since(started).Milliseconds(),
		InputTokens:  gotFinal.PromptEvalCount,
		OutputTokens: gotFinal.EvalCount,
	}
	return llm.CompleteResponse{
		Content:      buf.String(),
		InputTokens:  gotFinal.PromptEvalCount,
		OutputTokens: gotFinal.EvalCount,
		StopReason:   stop,
		Trace:        trace,
	}, nil
}

// CompleteStream은 NDJSON 라인을 token chunk로 그대로 흘립니다.
// 마지막 chunk(Done=true)에 LlmTrace가 채워집니다.
func (a *Adapter) CompleteStream(ctx context.Context, req llm.CompleteRequest) (<-chan llm.StreamChunk, error) {
	started := time.Now().UTC()
	model := a.resolveModel(req.Model)

	resp, _, err := a.openStream(ctx, req, model)
	if err != nil {
		return nil, err
	}

	out := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		var gotFinal generateLine
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var gl generateLine
			if err := json.Unmarshal(line, &gl); err != nil {
				out <- llm.StreamChunk{Done: true, Err: fmt.Errorf("ollama: parse ndjson: %w", err)}
				return
			}
			if gl.Response != "" && !gl.Done {
				out <- llm.StreamChunk{Token: gl.Response}
			}
			if gl.Done {
				if gl.Response != "" {
					out <- llm.StreamChunk{Token: gl.Response}
				}
				gotFinal = gl
				break
			}
		}
		if err := scanner.Err(); err != nil {
			mapped := mapErr(err)
			out <- llm.StreamChunk{
				Done: true,
				Err:  mapped,
				Trace: llm.LlmTrace{
					Provider:   "ollama",
					Model:      model,
					StartedAt:  started,
					DurationMs: time.Since(started).Milliseconds(),
					Error:      mapped.Error(),
				},
			}
			return
		}
		out <- llm.StreamChunk{
			Done: true,
			Trace: llm.LlmTrace{
				Provider:     "ollama",
				Model:        model,
				StartedAt:    started,
				DurationMs:   time.Since(started).Milliseconds(),
				InputTokens:  gotFinal.PromptEvalCount,
				OutputTokens: gotFinal.EvalCount,
			},
		}
	}()
	return out, nil
}

// openStream은 generate 요청을 보내고 *http.Response를 돌려줍니다.
func (a *Adapter) openStream(ctx context.Context, req llm.CompleteRequest, model string) (*http.Response, generateLine, error) {
	prompt := serializeMessages(req.Messages)
	options := map[string]any{}
	if req.Temperature > 0 {
		options["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		options["num_predict"] = req.MaxTokens
	}
	var bodyBuf bytes.Buffer
	enc := json.NewEncoder(&bodyBuf)
	enc.SetEscapeHTML(false) // `<|role|>` 토큰을 unicode escape 없이 그대로 보낸다.
	if err := enc.Encode(generateRequest{
		Model:   model,
		Prompt:  prompt,
		Stream:  true,
		Options: options,
	}); err != nil {
		return nil, generateLine{}, fmt.Errorf("ollama: marshal: %w", err)
	}
	body := bodyBuf.Bytes()

	url := a.endpoint + "/api/generate"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, generateLine{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, generateLine{}, mapErr(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, generateLine{}, fmt.Errorf("ollama: http %d", resp.StatusCode)
	}
	return resp, generateLine{}, nil
}

func (a *Adapter) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return a.defaultModel
}

// serializeMessages는 chat 메시지를 `<|role|>content\n` 시퀀스로 직렬화합니다.
//
// 모델별 chat template 차이는 무시 — 단일 prompt 모드에서 system/user/assistant
// 구조만 보전합니다. 마지막은 `<|assistant|>`로 답변 시작 신호.
func serializeMessages(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "<|%s|>\n%s\n", m.Role, m.Content)
	}
	b.WriteString("<|assistant|>\n")
	return b.String()
}

// mapErr는 net/http 에러를 sentinel로 매핑합니다.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return llm.ErrTimeout
	}
	// http.Client.Timeout 만료는 url.Error{Timeout: true}로 옴.
	type timeoutErr interface{ Timeout() bool }
	var te timeoutErr
	if errors.As(err, &te) && te.Timeout() {
		return llm.ErrTimeout
	}
	return err
}
