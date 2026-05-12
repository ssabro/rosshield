package insightautorun_test

// autorun_test.go — E19 Subscriber 테스트.
//
// 검증 시나리오:
//   - 정상 path: scan.completed 이벤트 → RunForFleet 호출 + count 로깅
//   - status != completed: skip (RunForFleet 호출 없음)
//   - empty payload: skip (decode 실패는 warn 로그)
//   - empty tenantID: skip
//   - GetSession 실패: warn 로그 + 이벤트 ack (재시도 없음)
//
// fake scan.Service / insight.Service / storage.Storage로 외부 도메인 의존 격리.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/app/insightautorun"
	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// === fakes ===

type fakeStorage struct {
	mu       sync.Mutex
	tenantID storage.TenantID
}

func (f *fakeStorage) Migrate(_ context.Context) error { return nil }
func (f *fakeStorage) Close() error                    { return nil }

func (f *fakeStorage) Tx(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	f.mu.Lock()
	f.tenantID = storage.TenantIDFromContext(ctx)
	f.mu.Unlock()
	return fn(ctx, &fakeTx{tenantID: f.tenantID})
}

func (f *fakeStorage) Bootstrap(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	return fn(ctx, &fakeTx{tenantID: ""})
}

type fakeTx struct{ tenantID storage.TenantID }

func (t *fakeTx) TenantID() storage.TenantID { return t.tenantID }

// fakeTx의 Exec/Query/QueryRow는 호출되지 않음 — fake services가 tx를 사용하지 않음.
// interface 시그니처만 맞춤 (storage.Tx는 sql.Result/sql.Rows/sql.Row를 직접 노출).
func (t *fakeTx) Exec(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, nil
}
func (t *fakeTx) Query(_ context.Context, _ string, _ ...any) (*sql.Rows, error) { return nil, nil }
func (t *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) *sql.Row        { return nil }

// fakeScan은 GetSession에서 미리 설정된 session을 반환합니다.
type fakeScan struct {
	session    scan.ScanSession
	getErr     error
	getCalled  int
	mu         sync.Mutex
	wantSessID string
}

func (f *fakeScan) GetSession(_ context.Context, _ storage.Tx, id string) (scan.ScanSession, error) {
	f.mu.Lock()
	f.getCalled++
	f.wantSessID = id
	f.mu.Unlock()
	if f.getErr != nil {
		return scan.ScanSession{}, f.getErr
	}
	return f.session, nil
}

// 미사용 메서드 — interface 만족용 (panic으로 호출 안 됨을 표현).
func (f *fakeScan) StartScan(_ context.Context, _ storage.Tx, _ scan.StartScanRequest) (scan.ScanSession, error) {
	panic("not used")
}
func (f *fakeScan) ListSessions(_ context.Context, _ storage.Tx, _ scan.ListSessionsFilter) ([]scan.ScanSession, error) {
	panic("not used")
}
func (f *fakeScan) TransitionSession(_ context.Context, _ storage.Tx, _ string, _ scan.SessionStatus, _ string) (scan.ScanSession, error) {
	panic("not used")
}
func (f *fakeScan) CancelSession(_ context.Context, _ storage.Tx, _, _ string) (scan.ScanSession, error) {
	panic("not used")
}
func (f *fakeScan) RecordResult(_ context.Context, _ storage.Tx, _ scan.RecordResultRequest) (scan.ScanResult, error) {
	panic("not used")
}
func (f *fakeScan) ListResults(_ context.Context, _ storage.Tx, _ string) ([]scan.ScanResult, error) {
	panic("not used")
}
func (f *fakeScan) ListResultsByRobot(_ context.Context, _ storage.Tx, _ string, _ int) ([]scan.ScanResult, error) {
	panic("not used")
}

// fakeInsight는 RunForFleet 호출을 카운트하고 미리 설정된 결과를 반환합니다.
type fakeInsight struct {
	mu       sync.Mutex
	called   int
	lastFlt  string
	produced []insight.Insight
	runErr   error
}

func (f *fakeInsight) RunForFleet(_ context.Context, _ storage.Tx, fleetID string) ([]insight.Insight, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called++
	f.lastFlt = fleetID
	if f.runErr != nil {
		return nil, f.runErr
	}
	return f.produced, nil
}
func (f *fakeInsight) ListActive(_ context.Context, _ storage.Tx, _ insight.ListFilter) ([]insight.Insight, error) {
	panic("not used")
}
func (f *fakeInsight) Dismiss(_ context.Context, _ storage.Tx, _, _, _ string) (insight.Insight, error) {
	panic("not used")
}

func (f *fakeInsight) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}
func (f *fakeInsight) lastFleet() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFlt
}

// === harness ===

func newTestBus(t *testing.T) eventbus.Bus {
	t.Helper()
	return inproc.New(inproc.Deps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Clock:  clock.System(),
		IDGen:  idgen.NewULID(),
	})
}

func publishCompleted(t *testing.T, bus eventbus.Bus, tenantID, sessionID, status string) {
	t.Helper()
	payload, _ := json.Marshal(scan.CompletedEventPayload{
		SessionID: sessionID,
		Status:    status,
		Total:     5, Completed: 5, Failed: 0,
	})
	if err := bus.Publish(context.Background(), eventbus.Event{
		Type:      scan.EventTypeCompleted,
		Version:   1,
		TenantID:  tenantID,
		Aggregate: eventbus.AggregateRef{Type: scan.AggregateTypeScanSession, ID: sessionID},
		Payload:   payload,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

// === tests ===

func TestSubscriberRunsForFleetOnCompleted(t *testing.T) {
	t.Parallel()

	const tenantID, sessionID, fleetID = "tn_E19A", "scan_E19A", "fl_E19A"
	scn := &fakeScan{session: scan.ScanSession{
		ID:       sessionID,
		TenantID: storage.TenantID(tenantID),
		FleetID:  fleetID,
		Status:   scan.StatusCompleted,
	}}
	ins := &fakeInsight{}
	store := &fakeStorage{}

	sub := insightautorun.New(insightautorun.Deps{
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Storage: store,
		Scan:    scn,
		Insight: ins,
	})
	bus := newTestBus(t)
	defer func() { _ = bus.Close(context.Background()) }()

	subscription := sub.Start(context.Background(), bus)
	defer subscription.Cancel()

	publishCompleted(t, bus, tenantID, sessionID, "completed")

	// inproc bus는 동기로 enqueue되며 worker는 별도 goroutine — 짧은 대기.
	if !waitFor(func() bool { return ins.callCount() == 1 }, 500*time.Millisecond) {
		t.Fatalf("RunForFleet not called within 500ms (called=%d)", ins.callCount())
	}
	if got := ins.lastFleet(); got != fleetID {
		t.Errorf("lastFleet = %q, want %q", got, fleetID)
	}
}

func TestSubscriberSkipsWhenStatusNotCompleted(t *testing.T) {
	t.Parallel()

	scn := &fakeScan{}
	ins := &fakeInsight{}
	sub := insightautorun.New(insightautorun.Deps{
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Storage: &fakeStorage{},
		Scan:    scn,
		Insight: ins,
	})
	bus := newTestBus(t)
	defer func() { _ = bus.Close(context.Background()) }()
	subscription := sub.Start(context.Background(), bus)
	defer subscription.Cancel()

	publishCompleted(t, bus, "tn_E19B", "scan_E19B", "failed")
	publishCompleted(t, bus, "tn_E19B", "scan_E19B", "cancelled")

	// 짧은 시간 대기 후 호출 0이어야 함.
	time.Sleep(150 * time.Millisecond)
	if scn.getCalled != 0 {
		t.Errorf("GetSession called %d times for non-completed events, want 0", scn.getCalled)
	}
	if ins.callCount() != 0 {
		t.Errorf("RunForFleet called %d times for non-completed events, want 0", ins.callCount())
	}
}

func TestSubscriberLogsButAcksWhenGetSessionFails(t *testing.T) {
	t.Parallel()

	scn := &fakeScan{getErr: errors.New("session vanished")}
	ins := &fakeInsight{}
	sub := insightautorun.New(insightautorun.Deps{
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Storage: &fakeStorage{},
		Scan:    scn,
		Insight: ins,
	})
	bus := newTestBus(t)
	defer func() { _ = bus.Close(context.Background()) }()
	subscription := sub.Start(context.Background(), bus)
	defer subscription.Cancel()

	publishCompleted(t, bus, "tn_E19C", "scan_E19C", "completed")

	// GetSession은 호출되지만 RunForFleet은 호출 안 됨.
	if !waitFor(func() bool { scn.mu.Lock(); defer scn.mu.Unlock(); return scn.getCalled == 1 }, 500*time.Millisecond) {
		t.Fatalf("GetSession not called")
	}
	if ins.callCount() != 0 {
		t.Errorf("RunForFleet called %d times after GetSession failure, want 0", ins.callCount())
	}
}

func TestSubscriberSkipsEmptyTenantID(t *testing.T) {
	t.Parallel()

	scn := &fakeScan{}
	ins := &fakeInsight{}
	sub := insightautorun.New(insightautorun.Deps{
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Storage: &fakeStorage{},
		Scan:    scn,
		Insight: ins,
	})
	bus := newTestBus(t)
	defer func() { _ = bus.Close(context.Background()) }()
	subscription := sub.Start(context.Background(), bus)
	defer subscription.Cancel()

	publishCompleted(t, bus, "", "scan_E19D", "completed")

	time.Sleep(150 * time.Millisecond)
	if scn.getCalled != 0 {
		t.Errorf("GetSession called for empty tenantID, want 0")
	}
}

// waitFor는 cond가 true가 될 때까지 짧은 polling. 시간 초과면 false.
func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
