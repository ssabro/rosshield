// Package llmmapper는 platform/llm 어댑터를 compliance.LLMSuggester로 결선합니다 (E17 Phase 2).
//
// 책임:
//   - prompt 빌더: SuggestRequest → JSON-escaped prompt (after redaction by caller)
//   - JSON 응답 파싱: LLM이 반환한 JSON 배열 → []SuggestionDraft
//   - LlmTrace.Provider/Model을 SuggestResponse에 채워 audit cross-check 가능하게
//   - LLM이 ErrLLMDisabled를 반환하면 동일하게 그대로 propagate (caller가 sentinel 매핑)
//
// 도메인 결합 (P5): bootstrap만 본 패키지를 import. compliance 도메인은 본 패키지를 import 안 함.
package llmmapper

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/platform/llm"
)

// defaultTopN은 prompt에 포함할 candidate 수 상한입니다 (LLM 컨텍스트 절약).
const defaultTopN = 20

// defaultMaxResults는 LLM에 요청할 결과 수 상한입니다.
const defaultMaxResults = 5

// Suggester는 compliance.LLMSuggester 구현체입니다.
type Suggester struct {
	adapter llm.Adapter
	model   string // 기본 모델 (request.Model이 비어있을 때 fallback). caller가 cfg에서 주입.
}

// New는 새 Suggester를 반환합니다.
func New(adapter llm.Adapter, model string) *Suggester {
	return &Suggester{adapter: adapter, model: model}
}

// Suggest는 LLM 호출로 매핑 후보를 반환합니다.
func (s *Suggester) Suggest(ctx context.Context, req compliance.SuggestRequest) (compliance.SuggestResponse, error) {
	if s.adapter == nil {
		return compliance.SuggestResponse{}, fmt.Errorf("llmmapper: adapter is nil")
	}

	maxResults := req.TopN
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	prompt := buildPrompt(req, defaultTopN, maxResults)

	llmReq := llm.CompleteRequest{
		Model: s.model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: prompt},
		},
		Temperature: 0.0, // 결정성 우선 (P6 결정론 fallback 가능성 보장)
		MaxTokens:   2048,
	}

	resp, err := s.adapter.Complete(ctx, llmReq)
	if err != nil {
		return compliance.SuggestResponse{}, err
	}

	drafts, err := parseSuggestionsJSON(resp.Content)
	if err != nil {
		return compliance.SuggestResponse{
			LLMProvider: resp.Trace.Provider,
			LLMModel:    resp.Trace.Model,
		}, fmt.Errorf("llmmapper: parse: %w", err)
	}

	// confidence 내림차순으로 정렬 + maxResults 제한.
	sort.SliceStable(drafts, func(i, j int) bool { return drafts[i].Confidence > drafts[j].Confidence })
	if len(drafts) > maxResults {
		drafts = drafts[:maxResults]
	}

	return compliance.SuggestResponse{
		Suggestions: drafts,
		LLMProvider: resp.Trace.Provider,
		LLMModel:    resp.Trace.Model,
	}, nil
}

const systemPrompt = `You are a compliance expert mapping security check definitions to ` +
	`framework controls. You will receive a check description and a list of candidate controls. ` +
	`Return ONLY a JSON array of suggestion objects matching the schema:
[{"controlId":"<ID>","confidence":<0.0-1.0>,"reasoning":"<one-line>"},...]
Do not include any prose outside the JSON array. Order results by confidence descending.`

// buildPrompt는 SuggestRequest를 LLM user prompt로 직렬화합니다.
//
// candidate가 너무 많으면 lex 순서로 candidateLimit 개만 잘라 컨텍스트 비용 제어.
func buildPrompt(req compliance.SuggestRequest, candidateLimit, maxResults int) string {
	var sb strings.Builder
	sb.WriteString("Check to map:\n")
	sb.WriteString(fmt.Sprintf("  code: %s\n", req.CheckCode))
	if req.CheckTitle != "" {
		sb.WriteString(fmt.Sprintf("  title: %s\n", req.CheckTitle))
	}
	if req.CheckRationale != "" {
		sb.WriteString(fmt.Sprintf("  rationale: %s\n", req.CheckRationale))
	}
	sb.WriteString(fmt.Sprintf("\nFramework: %s\n\n", req.Framework))

	candidates := req.CandidateControls
	if len(candidates) > candidateLimit {
		// stable lex 순서로 truncate (결정성).
		sortedCands := append([]compliance.CandidateControl(nil), candidates...)
		sort.SliceStable(sortedCands, func(i, j int) bool { return sortedCands[i].ID < sortedCands[j].ID })
		candidates = sortedCands[:candidateLimit]
	}
	sb.WriteString(fmt.Sprintf("Candidate controls (%d):\n", len(candidates)))
	for _, c := range candidates {
		sb.WriteString(fmt.Sprintf("  - %s: %s\n", c.ID, c.Title))
		if c.Summary != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", c.Summary))
		}
	}

	sb.WriteString(fmt.Sprintf("\nReturn at most %d top suggestions as JSON array.\n", maxResults))
	return sb.String()
}

// parseSuggestionsJSON은 LLM 응답에서 JSON 배열을 추출·파싱합니다.
//
// LLM이 응답 외 텍스트를 흘릴 수 있으므로 첫 '[' ~ 마지막 ']' 범위만 잘라 파싱 시도.
func parseSuggestionsJSON(content string) ([]compliance.SuggestionDraft, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty response")
	}
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found")
	}
	body := content[start : end+1]

	var raw []rawSuggestion
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	out := make([]compliance.SuggestionDraft, 0, len(raw))
	for _, r := range raw {
		if r.ControlID == "" {
			continue // 잘못된 entry skip
		}
		conf := r.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		out = append(out, compliance.SuggestionDraft{
			ControlID:  r.ControlID,
			Confidence: conf,
			Reasoning:  r.Reasoning,
		})
	}
	return out, nil
}

// rawSuggestion은 LLM JSON 응답의 한 entry입니다.
type rawSuggestion struct {
	ControlID  string  `json:"controlId"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}
