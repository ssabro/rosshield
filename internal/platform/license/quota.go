package license

import (
	"context"
	"fmt"
	"time"
)

// UsageReader는 호출 시점의 tenant 사용량을 조회합니다.
//
// 도메인 layer가 본 인터페이스의 구현체를 주입 (P5 격리). license platform은 도메인을
// 직접 import 하지 않고 read-only callback으로만 사용량 확인.
type UsageReader interface {
	// CurrentRobots는 tenant의 활성 robot 수를 반환합니다.
	CurrentRobots(ctx context.Context, tenantID string) (int, error)
	// ScansToday는 오늘 시작된 scan session 수를 반환합니다.
	ScansToday(ctx context.Context, tenantID string) (int, error)
	// LLMTokensToday는 오늘의 LlmTrace input+output 토큰 합을 반환합니다.
	LLMTokensToday(ctx context.Context, tenantID string) (int, error)
}

// QuotaCheckResult는 enforcement 결과입니다.
type QuotaCheckResult struct {
	Allowed bool
	Reason  string // 거부 이유 (Allowed=true면 빈 문자열).
	Field   string // 초과한 quota field 이름. Allowed=true면 빈 문자열.
}

// Enforcer는 라이선스 + 한도 검사를 수행합니다.
//
// 사용처: enterprise endpoint 진입 시점 (handler middleware) + 도메인 mutation 직전
// (예: scan 시작 직전에 ScansToday 한도 검증).
type Enforcer struct {
	payload Payload
	usage   UsageReader
	now     func() time.Time
}

// NewEnforcer는 검증된 Payload + UsageReader로 Enforcer를 만듭니다.
//
// nowFn은 결정론적 테스트용 — production은 time.Now 주입.
func NewEnforcer(p Payload, usage UsageReader, nowFn func() time.Time) *Enforcer {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Enforcer{payload: p, usage: usage, now: nowFn}
}

// CheckFeature는 enterprise feature가 활성인지 검사합니다 (만료도 포함).
//
// 라이선스가 없거나 만료된 경우 community SKU로 간주 — 모든 enterprise feature 거부.
func (e *Enforcer) CheckFeature(f Feature) QuotaCheckResult {
	if e == nil || e.payload.Version == 0 {
		return QuotaCheckResult{Allowed: false, Reason: "no license", Field: "feature:" + string(f)}
	}
	if e.payload.IsExpired(e.now()) {
		return QuotaCheckResult{Allowed: false, Reason: "license expired", Field: "feature:" + string(f)}
	}
	if e.payload.Edition != EditionEnterprise {
		return QuotaCheckResult{Allowed: false, Reason: "not enterprise edition", Field: "feature:" + string(f)}
	}
	if !e.payload.HasFeature(f) {
		return QuotaCheckResult{Allowed: false, Reason: "feature not licensed", Field: "feature:" + string(f)}
	}
	return QuotaCheckResult{Allowed: true}
}

// CheckRobotsAdd는 tenant에 robot N개를 추가할 수 있는지 검사합니다.
//
// robots_max=0 또는 음수는 무제한. usage.CurrentRobots() + addCount > robots_max면 거부.
func (e *Enforcer) CheckRobotsAdd(ctx context.Context, tenantID string, addCount int) (QuotaCheckResult, error) {
	if e == nil || e.payload.Version == 0 {
		// 라이선스 미부여 → community SKU. 본 모듈은 community 한도를 정의하지 않음 — 도메인에서.
		return QuotaCheckResult{Allowed: true}, nil
	}
	if e.payload.IsExpired(e.now()) {
		return QuotaCheckResult{Allowed: false, Reason: "license expired", Field: "robots_max"}, nil
	}
	if e.payload.Quotas.IsUnlimited("robots_max") {
		return QuotaCheckResult{Allowed: true}, nil
	}
	current, err := e.usage.CurrentRobots(ctx, tenantID)
	if err != nil {
		return QuotaCheckResult{}, fmt.Errorf("license: read robots usage: %w", err)
	}
	if current+addCount > e.payload.Quotas.RobotsMax {
		return QuotaCheckResult{
			Allowed: false,
			Reason:  fmt.Sprintf("robots quota exceeded (current=%d add=%d max=%d)", current, addCount, e.payload.Quotas.RobotsMax),
			Field:   "robots_max",
		}, nil
	}
	return QuotaCheckResult{Allowed: true}, nil
}

// CheckScansToday는 오늘 scan을 1건 더 시작할 수 있는지 검사합니다.
func (e *Enforcer) CheckScansToday(ctx context.Context, tenantID string) (QuotaCheckResult, error) {
	if e == nil || e.payload.Version == 0 {
		return QuotaCheckResult{Allowed: true}, nil
	}
	if e.payload.IsExpired(e.now()) {
		return QuotaCheckResult{Allowed: false, Reason: "license expired", Field: "scans_per_day"}, nil
	}
	if e.payload.Quotas.IsUnlimited("scans_per_day") {
		return QuotaCheckResult{Allowed: true}, nil
	}
	used, err := e.usage.ScansToday(ctx, tenantID)
	if err != nil {
		return QuotaCheckResult{}, fmt.Errorf("license: read scans usage: %w", err)
	}
	if used+1 > e.payload.Quotas.ScansPerDay {
		return QuotaCheckResult{
			Allowed: false,
			Reason:  fmt.Sprintf("scans/day quota exceeded (today=%d max=%d)", used, e.payload.Quotas.ScansPerDay),
			Field:   "scans_per_day",
		}, nil
	}
	return QuotaCheckResult{Allowed: true}, nil
}

// CheckLLMTokens는 오늘 LLM 토큰 limit이 남았는지 검사합니다 (요청 토큰 = wantTokens).
func (e *Enforcer) CheckLLMTokens(ctx context.Context, tenantID string, wantTokens int) (QuotaCheckResult, error) {
	if e == nil || e.payload.Version == 0 {
		return QuotaCheckResult{Allowed: true}, nil
	}
	if e.payload.IsExpired(e.now()) {
		return QuotaCheckResult{Allowed: false, Reason: "license expired", Field: "llm_tokens_per_day"}, nil
	}
	if e.payload.Quotas.IsUnlimited("llm_tokens_per_day") {
		return QuotaCheckResult{Allowed: true}, nil
	}
	used, err := e.usage.LLMTokensToday(ctx, tenantID)
	if err != nil {
		return QuotaCheckResult{}, fmt.Errorf("license: read llm usage: %w", err)
	}
	if used+wantTokens > e.payload.Quotas.LLMTokensPerDay {
		return QuotaCheckResult{
			Allowed: false,
			Reason:  fmt.Sprintf("LLM tokens/day quota exceeded (today=%d want=%d max=%d)", used, wantTokens, e.payload.Quotas.LLMTokensPerDay),
			Field:   "llm_tokens_per_day",
		}, nil
	}
	return QuotaCheckResult{Allowed: true}, nil
}

// Payload는 검증·만료 외 다른 메타 사용을 위해 노출합니다 (예: UI에 IssuedTo 표시).
func (e *Enforcer) Payload() Payload {
	if e == nil {
		return Payload{}
	}
	return e.payload
}

// UsageSnapshot은 tenant의 현재 사용량을 한 번 읽어 반환합니다 (E29 — license info usage).
//
// UsageReader 미주입 시 zero 반환 (community SKU 또는 미결선 환경). 에러는 partial result —
// 한 메서드 실패하더라도 나머지 값은 그대로 반환.
type UsageSnapshot struct {
	CurrentRobots  int
	ScansToday     int
	LLMTokensToday int
}

// ReadUsage는 UsageSnapshot을 채워 반환합니다.
//
// usage reader 호출은 storage Tx를 새로 열기 때문에 짧은 timeout ctx 권장.
func (e *Enforcer) ReadUsage(ctx context.Context, tenantID string) UsageSnapshot {
	var snap UsageSnapshot
	if e == nil || e.usage == nil || tenantID == "" {
		return snap
	}
	if v, err := e.usage.CurrentRobots(ctx, tenantID); err == nil {
		snap.CurrentRobots = v
	}
	if v, err := e.usage.ScansToday(ctx, tenantID); err == nil {
		snap.ScansToday = v
	}
	if v, err := e.usage.LLMTokensToday(ctx, tenantID); err == nil {
		snap.LLMTokensToday = v
	}
	return snap
}
