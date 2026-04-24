package benchmark

import (
	"errors"
	"fmt"
)

// 라이프사이클 FSM (C7 결정):
//
//   Installed → Staged → Active ⇄ Inactive → Archived → Removed
//
// 직접 Active로 못 감 — 항상 Staged 거쳐 self-test 검증 완료 후. (P1·P8: 검증된 콘텐츠만 활성)
// Removed는 종착 — 더 이상 전이 불가.

// allowedTransitions는 from → 가능한 to 집합입니다.
//
// 외부에서 검증·시각화 가능하도록 노출 (read-only로 사용 권장).
var allowedTransitions = map[State]map[State]bool{
	StateInstalled: {
		StateStaged:   true,
		StateArchived: true, // 설치만 하고 archive하는 시나리오 (잘못 설치된 pack 제거)
	},
	StateStaged: {
		StateActive:    true,
		StateInstalled: true, // Staged 취소 → 다시 Installed
		StateArchived:  true,
	},
	StateActive: {
		StateInactive: true,
		StateArchived: true, // 사용 중지 + 보관
	},
	StateInactive: {
		StateActive:   true,
		StateArchived: true,
	},
	StateArchived: {
		StateRemoved: true,
	},
	StateRemoved: {}, // 종착 — 전이 불가
}

// CanTransition은 from → to 전이가 허용되는지 반환합니다.
func CanTransition(from, to State) bool {
	if from == to {
		return false // self-transition 금지
	}
	allowed, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	return allowed[to]
}

// Transition은 from → to 전이를 검증하고 to를 반환합니다 (실패 시 ErrIllegalTransition).
//
// 호출자(sqliterepo)가 같은 Tx에서:
//  1. 현재 state 조회 (latest pack_lifecycle row)
//  2. Transition으로 검증
//  3. 새 lifecycle row INSERT (pack_id, state=to, transitioned_at, actor, reason)
//  4. AuditEmitter.EmitPackLifecycleChanged
func Transition(from, to State) (State, error) {
	if !CanTransition(from, to) {
		return "", fmt.Errorf("%w: %s → %s", ErrIllegalTransition, from, to)
	}
	return to, nil
}

// IsTerminal은 state가 종착(더 이상 전이 불가)인지 반환합니다.
func IsTerminal(s State) bool {
	allowed, ok := allowedTransitions[s]
	return ok && len(allowed) == 0
}

// AllStates는 정의된 모든 상태를 반환합니다 (UI·문서용).
func AllStates() []State {
	return []State{
		StateInstalled, StateStaged, StateActive,
		StateInactive, StateArchived, StateRemoved,
	}
}

// AllowedNextStates는 from에서 갈 수 있는 모든 to 상태를 반환합니다 (UI 전이 메뉴용).
func AllowedNextStates(from State) []State {
	allowed := allowedTransitions[from]
	out := make([]State, 0, len(allowed))
	for to := range allowed {
		out = append(out, to)
	}
	return out
}

// Lifecycle 관련 sentinel 에러.
var (
	ErrIllegalTransition = errors.New("benchmark: illegal lifecycle transition")
	ErrUnknownState      = errors.New("benchmark: unknown state")
)
