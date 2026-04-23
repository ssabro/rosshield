// Package scheduler는 정기 작업 등록·취소의 공개 표면을 정의합니다.
//
// Phase 1은 robfig/cron/v3 기반 어댑터(`cronsched`)만 제공합니다.
// 결정론적 테스트가 필요해지면 Clock 인터페이스를 확장하고 자체 스케줄러로 교체할 수 있습니다 (E1.T9 결정 노선 B).
package scheduler

import (
	"context"
	"errors"
)

// Job은 스케줄러가 호출할 작업입니다.
// 반환 error는 어댑터가 로그에 기록하고, 다음 발화는 정상적으로 진행됩니다.
type Job func(ctx context.Context) error

// Scheduler는 ID 기반 작업 등록·취소를 제공합니다.
// New() 시점부터 백그라운드 발화 루프가 시작되며, Close()로 정상 종료합니다.
type Scheduler interface {
	// Schedule은 spec(예: "@every 1m", "0 0 * * *")에 맞춰 job을 등록합니다.
	// 동일 id가 이미 존재하면 ErrJobExists. spec 파싱 실패는 형식 의존 에러를 반환합니다.
	Schedule(id, spec string, job Job) error

	// Cancel은 id로 등록된 job을 제거합니다. 존재하지 않으면 no-op.
	// 이미 발화된 in-flight 호출은 끝까지 진행됩니다.
	Cancel(id string)

	// Close는 새 발화를 멈추고 in-flight 호출이 완료될 때까지 대기합니다.
	// ctx 만료 시 ctx.Err() 반환.
	Close(ctx context.Context) error
}

// 공통 에러.
var (
	ErrJobExists = errors.New("scheduler: job id already registered")
)
