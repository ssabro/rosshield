package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// HashVersionTransitionMeta 는 transition marker entry 의 payload meta 입니다.
//
// design `audit-hash-key-epoch-input-design.md` §6.3 — 외부 검증 도구가 transition entry
// 의 payload 를 unmarshal 해 from/to version 을 명시적으로 인식 가능합니다.
type HashVersionTransitionMeta struct {
	FromVersion int `json:"fromVersion"`
	ToVersion   int `json:"toVersion"`
}

// HashVersionTransitionSetter 는 Repo 가 transition seq 를 caching 할 수 있는 minimal
// interface 입니다.
//
// sqliterepo.Repo 가 자동 만족. 본 interface 를 audit 패키지에서 정의해 EnsureHashVersionTransition
// 이 sqliterepo 를 import 하지 않도록 합니다 (도메인 경계 일관 — P5).
type HashVersionTransitionSetter interface {
	SetHashVersionTransitionSeq(seq int64)
}

// EnsureHashVersionTransition 은 audit chain 의 hash version 전환 marker entry 가
// 존재함을 보장합니다 (Phase 11.C-3 — idempotent).
//
// 동작:
//  1. locator.FindHashVersionTransitionSeq 로 기존 transition entry 조회.
//  2. ok=true → 이미 emit 됨. setter.SetHashVersionTransitionSeq(seq) 호출 후 종료 (idempotent).
//  3. ok=false → svc.Append 로 transition entry emit. emit 결과 entry.Seq 를 setter 에 캐시.
//
// 모든 작업은 caller 가 제공한 store.Tx 안에서 실행됨 — bootstrap 이 1 Tx 로 묶어 호출.
//
// emit 되는 entry:
//   - Action  = ActionHashVersionChanged.
//   - Actor   = ActorSystem, ID = "migration:phase-11.c-3" (한 번 marker — migration 일관).
//   - Target  = ("audit_chain", string(tenantID)).
//   - Payload = HashVersionTransitionMeta{FromVersion:1, ToVersion:3} JSON.
//   - Outcome = OutcomeSuccess.
//
// transition entry 자체의 hash 는 v1 (chain link 연속성 — sqliterepo.Repo.Append 가
// transitionSeq 가 SetHashVersionTransitionSeq 호출 전이라 v1 분기를 선택).
//
// 동시성: bootstrap 단일 thread 에서 1회 호출. 후속 Append goroutine 보다 먼저 완료되어야
// 함 (HA replication 결선 전 bootstrap 단에서 보장).
func EnsureHashVersionTransition(
	ctx context.Context,
	tx storage.Tx,
	svc Service,
	locator HashVersionLocator,
	setter HashVersionTransitionSetter,
	tenantID storage.TenantID,
) (Entry, bool, error) {
	if svc == nil || locator == nil || setter == nil {
		return Entry{}, false, fmt.Errorf("audit: EnsureHashVersionTransition: all dependencies required")
	}
	if tenantID == "" {
		return Entry{}, false, storage.ErrTenantMissing
	}

	seq, ok, err := locator.FindHashVersionTransitionSeq(ctx, tx, tenantID)
	if err != nil {
		return Entry{}, false, fmt.Errorf("audit: find transition seq: %w", err)
	}
	if ok {
		setter.SetHashVersionTransitionSeq(seq)
		return Entry{TenantID: tenantID, Seq: seq, Action: ActionHashVersionChanged}, false, nil
	}

	meta := HashVersionTransitionMeta{FromVersion: 1, ToVersion: 3}
	payload, err := json.Marshal(meta)
	if err != nil {
		return Entry{}, false, fmt.Errorf("audit: marshal transition meta: %w", err)
	}

	entry, err := svc.Append(ctx, tx, AppendRequest{
		TenantID: tenantID,
		Actor:    Actor{Type: ActorSystem, ID: "migration:phase-11.c-3"},
		Action:   ActionHashVersionChanged,
		Target:   Target{Type: "audit_chain", ID: string(tenantID)},
		Payload:  payload,
		Outcome:  OutcomeSuccess,
	})
	if err != nil {
		return Entry{}, false, fmt.Errorf("audit: append transition entry: %w", err)
	}
	setter.SetHashVersionTransitionSeq(entry.Seq)
	return entry, true, nil
}
