// Package llm은 공급자 중립 LLM Adapter 표면입니다 (§8.3 LLM Adapter 계층).
//
// 결정 근거:
//   - P2 옵트인 지능화: noop이 기본값, ollama/anthropic은 명시 활성 후 동작.
//   - P3 에어갭 1급: 외부 SDK 미사용 (anthropic-sdk-go 등 금지) — stdlib net/http만.
//   - P5 도메인 결합: platform 패키지로 다른 도메인(scan/evidence)을 import하지 않음.
//   - P6 결정론적 fallback: 모든 호출은 LlmTrace로 기록 — caller는 ErrLLMDisabled 등을
//     보고 규칙 기반 경로로 우회한다.
//   - R14-1 LLM 기본 어댑터 = noop.
//   - R14-6 cost guardrail은 caller(Insight/Advisor 도메인)가 LlmTrace 누적으로 강제.
//
// 도메인 결합 규칙: 본 패키지는 다른 도메인을 import하지 않습니다 (P5 + depguard 예정).
// redaction은 caller(Insight/Advisor)가 evidence.Redact로 prompt·response에 직접 적용합니다.
package llm

import (
	"context"
	"errors"
	"io"
	"time"
)

// Role은 LLM 메시지 role입니다.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message는 단일 chat turn입니다.
type Message struct {
	Role    Role
	Content string
}

// CompleteRequest는 Adapter.Complete 입력입니다.
type CompleteRequest struct {
	TenantID    string
	Model       string // 어댑터별 모델 식별자 (ollama "llama3.2", anthropic "claude-3-haiku-20240307")
	Messages    []Message
	Temperature float64       // 0.0 ~ 1.0
	MaxTokens   int           // 응답 토큰 상한 (0이면 어댑터 기본값)
	Timeout     time.Duration // 0이면 어댑터 기본값
}

// CompleteResponse는 Adapter.Complete 출력입니다.
type CompleteResponse struct {
	Content      string
	InputTokens  int    // 어댑터가 보고하면 채움 (없으면 0)
	OutputTokens int    // 어댑터가 보고하면 채움 (없으면 0)
	StopReason   string // "end_turn"|"max_tokens"|"timeout"
	Trace        LlmTrace
}

// LlmTrace는 한 LLM 호출의 결정성·감사 가능성 메타입니다 (§8.7).
//
// 모든 어댑터가 동일 형식으로 채움 — audit emit + UI 표시 + 비용 추적에 사용.
type LlmTrace struct {
	Provider     string // "noop"|"ollama"|"anthropic"
	Model        string
	StartedAt    time.Time
	DurationMs   int64
	InputTokens  int
	OutputTokens int
	Cost         float64 // USD (어댑터별 가격표 기반 추정 — anthropic만 정확)
	Error        string  // 비어있으면 성공
}

// Adapter는 LLM 호출 표면입니다.
//
// 구현체:
//   - noop:      ErrLLMDisabled 즉시 반환 (R14-1 기본값).
//   - ollama:    로컬 HTTP (http://localhost:11434/api/generate).
//   - anthropic: 클라우드 HTTPS (https://api.anthropic.com/v1/messages).
type Adapter interface {
	// Provider는 어댑터 식별자를 반환합니다.
	Provider() string

	// Complete은 동기 호출 — streaming 내부 처리 후 전체 응답 반환.
	Complete(ctx context.Context, req CompleteRequest) (CompleteResponse, error)

	// CompleteStream은 token-by-token 스트리밍 — caller가 채널 소비.
	// 종료 시 LlmTrace는 Done=true 메시지에 채워집니다.
	CompleteStream(ctx context.Context, req CompleteRequest) (<-chan StreamChunk, error)
}

// StreamChunk는 streaming 응답의 한 token (또는 메시지 종료 신호)입니다.
type StreamChunk struct {
	Token string   // 한 토큰 또는 부분 문자열
	Done  bool     // true면 stream 종료, Trace 채워짐
	Trace LlmTrace // Done=true일 때만 의미 있음
	Err   error    // 비어있지 않으면 stream이 에러로 종료
}

// 공통 에러 sentinel.
var (
	ErrLLMDisabled  = errors.New("llm: provider disabled (use ollama or anthropic adapter to enable)")
	ErrTimeout      = errors.New("llm: request timed out")
	ErrRateLimited  = errors.New("llm: rate limit exceeded")
	ErrTokenLimit   = errors.New("llm: tenant daily token limit exceeded (R14-6)")
	ErrUnauthorized = errors.New("llm: provider authentication failed")
)

// 컴파일 타임 가드 — 어댑터가 io 등 사용 시.
var _ = io.EOF
