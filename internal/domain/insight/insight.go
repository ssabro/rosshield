// Package insight는 결정론적 Insight 산출 도메인입니다 (E14 Phase 2).
//
// 책임:
//
//   - drift detector: 직전 N session 대비 (robot, check) outcome 전이 탐지 (R14-3, N=5)
//   - anomaly detector: 같은 (robot, check)의 duration_ms IQR 1.5× outlier (R14-4)
//   - peer detector: 같은 fleet 내 robot 간 pass 비율 평균 - 1σ 미달 (R14-5)
//
// 모든 detector는 결정론적이며 LLM 호출 0 — Insight.Reasoning 텍스트로 explainability(P11) 충족.
// LLM 옵션 설명은 E16 Advisor가 별도로 처리.
//
// 도메인 결합 규칙:
//
//	insight 도메인은 audit·scan 패키지를 직접 import하지 않습니다 (P5).
//	cmd/* bootstrap이 audit.Service 어댑터를 AuditEmitter로,
//	scan.Service 어댑터를 ScanReader로 주입.
//
// 결정: R14-3·R14-4·R14-5 (사용자 합의 2026-04-29).
package insight

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Kind는 Insight의 종류입니다 (§04.2 Insight).
type Kind string

const (
	KindDrift      Kind = "drift"
	KindAnomaly    Kind = "anomaly"
	KindPeer       Kind = "peer"
	KindRootCause  Kind = "root_cause" // E16 Advisor가 채움 (옵트인)
	KindPrediction Kind = "prediction" // Phase 3 후보
)

// Severity는 Insight의 심각도입니다 (§04.2 Insight).
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// ProducedBy는 Insight 산출 주체입니다 (§04.2 Insight).
type ProducedBy string

const (
	ProducedByRules  ProducedBy = "rules"
	ProducedByLLM    ProducedBy = "llm"
	ProducedByHybrid ProducedBy = "hybrid"
)

// Scope는 Insight가 가리키는 대상입니다.
//
// 모든 필드 옵션이며 detector가 의미 있는 것만 채웁니다:
//
//	drift   : RobotID + CheckID
//	anomaly : RobotID + CheckID
//	peer    : RobotID + FleetID
type Scope struct {
	RobotID string // 옵션
	FleetID string // 옵션
	CheckID string // pack_check_id, 옵션
}

// Insight는 한 detector 산출 1건입니다 (§04.2 Insight).
type Insight struct {
	ID           string
	TenantID     storage.TenantID
	Kind         Kind
	Severity     Severity
	Scope        Scope
	Summary      string
	Reasoning    string
	EvidenceJSON []byte // JSON 배열 raw
	RulesApplied []string
	Confidence   float64
	ProducedBy   ProducedBy
	CreatedAt    time.Time
	DismissedAt  *time.Time
	DismissedBy  string
}

// AuditEmitter는 insight 도메인 변경을 감사 로그에 기록하는 콜백입니다 (P5 — audit 도메인 직접 import 회피).
//
// emit 시점:
//
//	RunForFleet → 신규 INSERT 1건당 EmitInsightCreated 1회
//	Dismiss     → EmitInsightDismissed 1회 (reason 포함)
type AuditEmitter interface {
	EmitInsightCreated(ctx context.Context, tx storage.Tx, in Insight) error
	EmitInsightDismissed(ctx context.Context, tx storage.Tx, in Insight, reason string) error
}

// ScanReader는 insight가 필요한 scan 도메인 read-only 표면입니다 (P5 minimal DTO).
//
// scan.Service의 ListSessions/ListResults에 1:1 매핑되며, bootstrap이 어댑터 결선.
type ScanReader interface {
	// ListRecentSessions는 fleet 내 직전 N개 세션을 completed_at DESC로 반환합니다.
	// 미완료(running·pending) 세션은 제외 — completed_at IS NOT NULL 만 대상.
	ListRecentSessions(ctx context.Context, tx storage.Tx, fleetID string, limit int) ([]ScanSessionView, error)

	// ListResultsForSession은 한 세션의 모든 결과를 반환합니다.
	ListResultsForSession(ctx context.Context, tx storage.Tx, sessionID string) ([]ScanResultView, error)
}

// ScanSessionView는 ScanReader가 반환하는 세션 부분 뷰입니다 (도메인 격리).
type ScanSessionView struct {
	ID          string
	TenantID    storage.TenantID
	FleetID     string
	Status      string
	CompletedAt *time.Time
}

// ScanResultView는 ScanReader가 반환하는 결과 부분 뷰입니다 (도메인 격리).
type ScanResultView struct {
	ID         string
	SessionID  string
	RobotID    string
	CheckID    string // pack_check_id
	Outcome    string // "pass"|"fail"|"error"|"indeterminate"|"skipped"
	DurationMs int64
}

// ListFilter는 Service.ListActive 필터입니다.
//
// Limit=0이면 default 50.
type ListFilter struct {
	Kind     Kind     // 옵션
	Severity Severity // 옵션
	RobotID  string   // 옵션
	Limit    int      // 0이면 50
}

// Service는 insight 도메인 진입점입니다 (E14 Stage 단일).
type Service interface {
	// RunForFleet은 fleet 단위로 모든 insight detector를 실행하고 신규 Insight를 INSERT합니다.
	//
	// 흐름: ScanReader로 직전 N session(R14-3=5) 회수 → drift·anomaly·peer detector 실행
	// → 결과를 insights 테이블에 INSERT + audit emit. 같은 (tenant, kind, scope, summary)
	// 중복은 dedup(이미 활성 Insight 있으면 skip).
	RunForFleet(ctx context.Context, tx storage.Tx, fleetID string) ([]Insight, error)

	// ListActive는 dismissed=NULL인 Insight를 created_at DESC로 반환합니다.
	ListActive(ctx context.Context, tx storage.Tx, filter ListFilter) ([]Insight, error)

	// Dismiss는 Insight를 dismissed로 마킹 + audit emit합니다.
	// 이미 dismissed 상태면 ErrInsightNotFound (활성 인덱스 미스).
	Dismiss(ctx context.Context, tx storage.Tx, insightID string, dismissedBy string, reason string) (Insight, error)
}

// 공통 에러.
var (
	ErrInsightNotFound     = errors.New("insight: not found")
	ErrInsufficientHistory = errors.New("insight: insufficient session history (need >= N)")
)

// DefaultDriftWindow는 R14-3 합의값입니다 (직전 5 sessions).
const DefaultDriftWindow = 5

// DefaultIQRMultiplier는 R14-4 합의값입니다 (IQR × 1.5 outlier 임계).
const DefaultIQRMultiplier = 1.5

// DefaultPeerSigmaMultiplier는 R14-5 합의값입니다 (μ - 1σ 임계).
const DefaultPeerSigmaMultiplier = 1.0

// ValidateKind는 Kind 값이 enum 중 하나인지 검증합니다.
func ValidateKind(k Kind) bool {
	switch k {
	case KindDrift, KindAnomaly, KindPeer, KindRootCause, KindPrediction:
		return true
	default:
		return false
	}
}

// ValidateSeverity는 Severity 값이 enum 중 하나인지 검증합니다.
func ValidateSeverity(s Severity) bool {
	switch s {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}
