//go:build rosshield_enterprise

// scheduler.go — A-1 cross-witness interval fold-in scheduler (enterprise edition).
//
// 본 파일은 spec-candidate-A-draft.md [0019]~[0020-4]와
// docs/design/notes/phase7-public-transition-design.md §6.2의 정기 fold-in
// 시점을 단일 goroutine + ticker로 구현합니다:
//
//   - 매 Interval마다 WitnessProvider.GetWitnesses(ctx)로 다른 테넌트 최신
//     checkpoint 집합을 가져옴.
//   - 직전 fold-in hash(또는 zero hash)를 prev로, payloadDigest는 zero,
//     canonicalMeta는 빈 객체로 전달해 ComputeFoldInHash를 호출.
//   - 산출된 hash를 LastHash로 저장하고 OnFoldIn callback을 발사.
//
// 실제 prev hash 공급·payloadDigest·meta는 호출 측(audit 도메인 어댑터)이
// 주입할 수 있도록 SchedulerOptions의 callback 시그니처를 (prev, next, witnesses, err)로 제공.
//
// 본 scheduler는 단일 goroutine·in-flight tick 1건만 보장하며, 중복 Start는
// ErrSchedulerAlreadyRunning으로 거부합니다.

package crosswitness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// 오류 정의.
var (
	// ErrSchedulerAlreadyRunning은 Start가 이미 활성 상태인 Scheduler에 다시 호출될 때 반환됩니다.
	ErrSchedulerAlreadyRunning = errors.New("crosswitness: scheduler already running")

	// ErrSchedulerInvalidOptions는 SchedulerOptions의 필수 필드가 누락됐을 때 반환됩니다
	// (WitnessProvider nil 또는 Interval ≤ 0).
	ErrSchedulerInvalidOptions = errors.New("crosswitness: scheduler invalid options")
)

// WitnessProvider는 fold-in 시점에 다른 테넌트들의 최신 checkpoint를 공급하는
// 인터페이스입니다. 코어 audit 도메인의 어댑터가 이를 구현합니다 (E32 후속 통합).
type WitnessProvider interface {
	GetWitnesses(ctx context.Context) ([]TenantCheckpoint, error)
}

// SchedulerOptions는 NewScheduler 생성 시 필수·선택 옵션입니다.
type SchedulerOptions struct {
	// WitnessProvider는 다른 테넌트의 최신 checkpoint를 공급합니다 (필수).
	WitnessProvider WitnessProvider

	// Interval은 fold-in tick 간격입니다 (필수, > 0).
	Interval time.Duration

	// OnFoldIn은 매 tick의 결과를 호출자에게 통보하는 callback입니다.
	// err가 nil이면 next가 새 fold-in hash이고 witnesses는 정렬된 사본입니다.
	// err가 non-nil이면 provider 또는 compute 실패 — next는 의미 없음.
	// callback은 scheduler goroutine에서 동기 호출되므로 빠르게 반환해야 하며,
	// 무거운 작업은 별 goroutine으로 분리하는 책임이 호출자에게 있습니다.
	OnFoldIn func(prev, next Hash, witnesses []TenantCheckpoint, err error)

	// Logger는 scheduler 내부 로깅용(선택). nil이면 slog.Default()를 사용합니다.
	Logger *slog.Logger
}

// Scheduler는 정기 fold-in scheduler입니다. 인스턴스당 1개의 활성 Start만 허용.
type Scheduler struct {
	opts SchedulerOptions

	mu       sync.Mutex
	lastHash Hash

	running atomic.Bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewScheduler는 옵션을 받아 Scheduler 인스턴스를 생성합니다. 실 검증은 Start 시점에 수행.
func NewScheduler(opts SchedulerOptions) *Scheduler {
	return &Scheduler{opts: opts}
}

// Start는 goroutine + ticker를 띄워 매 Interval마다 fold-in을 수행합니다.
// 이미 실행 중이면 ErrSchedulerAlreadyRunning, 필수 옵션 누락이면
// ErrSchedulerInvalidOptions를 반환합니다.
func (s *Scheduler) Start(ctx context.Context) error {
	if s.opts.WitnessProvider == nil {
		return fmt.Errorf("%w: WitnessProvider is nil", ErrSchedulerInvalidOptions)
	}
	if s.opts.Interval <= 0 {
		return fmt.Errorf("%w: Interval must be > 0", ErrSchedulerInvalidOptions)
	}
	if !s.running.CompareAndSwap(false, true) {
		return ErrSchedulerAlreadyRunning
	}
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	go s.loop(ctx)
	return nil
}

// Stop은 in-flight tick 완료를 기다린 후 graceful 종료합니다.
// Start가 호출된 적 없으면 즉시 nil 반환.
func (s *Scheduler) Stop() error {
	if !s.running.CompareAndSwap(true, false) {
		return nil
	}
	close(s.stopCh)
	<-s.doneCh
	return nil
}

// LastHash는 마지막 fold-in으로 산출된 hash를 반환합니다 (한 번도 fold-in 안 했으면 zero).
func (s *Scheduler) LastHash() Hash {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHash
}

// loop는 ticker tick + ctx cancel + Stop signal을 처리하는 메인 loop입니다.
func (s *Scheduler) loop(ctx context.Context) {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()

	logger := s.opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	for {
		select {
		case <-ctx.Done():
			logger.Debug("crosswitness scheduler: ctx done, exit")
			return
		case <-s.stopCh:
			logger.Debug("crosswitness scheduler: stop signal, exit")
			return
		case <-ticker.C:
			s.tick(ctx, logger)
		}
	}
}

// tick은 단일 fold-in cycle을 수행합니다.
func (s *Scheduler) tick(ctx context.Context, logger *slog.Logger) {
	witnesses, err := s.opts.WitnessProvider.GetWitnesses(ctx)
	if err != nil {
		logger.Warn("crosswitness scheduler: provider error", slog.String("err", err.Error()))
		s.fireCallback(Hash{}, Hash{}, nil, fmt.Errorf("crosswitness: provider: %w", err))
		return
	}

	s.mu.Lock()
	prev := s.lastHash
	s.mu.Unlock()

	// payloadDigest와 canonicalMeta는 scheduler 자체로는 의미가 없어 zero/`{}`를 사용 —
	// 실 audit 어댑터(E32)가 entry 보강 시 자기 입력으로 ComputeFoldInHash를 다시 호출.
	var payloadDigest Hash
	meta := []byte("{}")

	next, err := ComputeFoldInHash(prev, payloadDigest, meta, witnesses)
	if err != nil {
		logger.Warn("crosswitness scheduler: compute error", slog.String("err", err.Error()))
		s.fireCallback(prev, Hash{}, witnesses, fmt.Errorf("crosswitness: compute: %w", err))
		return
	}

	s.mu.Lock()
	s.lastHash = next
	s.mu.Unlock()

	s.fireCallback(prev, next, witnesses, nil)
}

// fireCallback은 OnFoldIn이 nil이 아닐 때만 호출합니다.
func (s *Scheduler) fireCallback(prev, next Hash, witnesses []TenantCheckpoint, err error) {
	if s.opts.OnFoldIn == nil {
		return
	}
	s.opts.OnFoldIn(prev, next, witnesses, err)
}
