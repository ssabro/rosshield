// Package cronsched는 robfig/cron/v3 기반 Scheduler 어댑터입니다.
//
// 시간 처리는 robfig/cron 내부 `time.Now()`를 그대로 사용합니다 (E1.T9 결정 노선 A).
// 결정론적 테스트가 필요해지면 Clock 인터페이스 확장 + 자체 스케줄러 루프로 swap.
package cronsched

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"

	"github.com/ssabro/rosshield/internal/platform/scheduler"
)

// Deps는 cron 어댑터 의존성입니다.
type Deps struct {
	Logger *slog.Logger
}

// RoleProvider는 cronsched가 leader 여부를 질의할 수 있는 minimal interface입니다.
//
// E25 — HA 활성 시 follower는 cron tick을 silent skip. ha.Manager가 본 인터페이스를
// 자동 만족 (duck typing — cronsched는 ha 패키지 미import).
//
// nil 가능 — 그 경우 모든 tick이 실행됩니다 (HA 비활성 / 단일 인스턴스 가정).
type RoleProvider interface {
	IsLeader() bool
}

// Scheduler는 robfig/cron 기반 어댑터입니다.
type Scheduler struct {
	deps Deps
	cron *cron.Cron

	mu      sync.Mutex
	entries map[string]cron.EntryID
	role    RoleProvider // E25 — nil이면 모든 tick 실행 (HA 비활성).
}

// New는 새 Scheduler를 만들고 백그라운드 발화 루프를 즉시 시작합니다.
// `@every 1s` 같은 sub-minute 표현을 지원하기 위해 기본 파서 그대로 사용
// (robfig/cron v3 기본 파서는 5-field standard cron + descriptors 지원).
func New(deps Deps) *Scheduler {
	c := cron.New()
	c.Start()
	return &Scheduler{
		deps:    deps,
		cron:    c,
		entries: make(map[string]cron.EntryID),
	}
}

// Schedule은 id·spec·job을 등록합니다.
func (s *Scheduler) Schedule(id, spec string, job scheduler.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.entries[id]; exists {
		return scheduler.ErrJobExists
	}

	entryID, err := s.cron.AddFunc(spec, func() {
		s.runJob(id, job)
	})
	if err != nil {
		return fmt.Errorf("scheduler: parse spec %q: %w", spec, err)
	}
	s.entries[id] = entryID
	return nil
}

// runJob은 단일 발화. error 로그·panic recover.
func (s *Scheduler) runJob(id string, job scheduler.Job) {
	// E25 — HA 활성 시 follower는 silent skip. PG advisory lock leader만 실행.
	s.mu.Lock()
	rp := s.role
	s.mu.Unlock()
	if rp != nil && !rp.IsLeader() {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			s.deps.Logger.Error("scheduler: job panic",
				"id", id,
				"recovered", fmt.Sprint(r))
		}
	}()
	if err := job(context.Background()); err != nil {
		s.deps.Logger.Warn("scheduler: job error",
			"id", id,
			"err", err.Error())
	}
}

// SetRoleProvider는 HA RoleProvider를 lazy 주입합니다 (E25 Stage 4a).
//
// bootstrap에서 Scheduler 생성 후 HAManager가 만들어지므로, 본 메서드로 후속 주입.
// nil이면 unset (= HA 비활성).
//
// 동시성: bootstrap 단일 thread에서 한 번만 호출. heartbeat goroutine 시작 전에
// 호출되어야 함.
func (s *Scheduler) SetRoleProvider(rp RoleProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.role = rp
}

// Cancel은 등록된 id의 job을 제거합니다. 없으면 no-op.
func (s *Scheduler) Cancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entryID, ok := s.entries[id]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, id)
	}
}

// Close는 cron loop를 정지하고 in-flight job 완료까지 대기합니다.
func (s *Scheduler) Close(ctx context.Context) error {
	stopCtx := s.cron.Stop()
	select {
	case <-stopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
