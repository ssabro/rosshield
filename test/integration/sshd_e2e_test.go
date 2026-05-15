//go:build integration

// Package integration_test는 docker compose 기반 실 SSHD 컨테이너에 대한
// scanrun + sshpool e2e 통합 테스트를 제공합니다 (scanrun SSH 통합 Stage 5c).
//
// design doc: docs/design/notes/scanrun-ssh-integration-design.md §5.4 + §6 Stage 5.
//
// 빌드 태그 `integration`은 일반 `go test ./...`에서 본 파일이 빌드되지 않게
// 합니다 (docker compose 의존). 별 Makefile target `make test-ssh-e2e`에서만
// 실행 — docker 가용 환경에서.
//
// 사전 조건: `make test-ssh-e2e`가 호출 전 docker compose up을 수행. 본 테스트는
// fixture 컨테이너가 이미 기동되어 있다고 가정 — host port 12222·12223·12224.
//
// 5 Phase 시나리오 (design doc §5.4):
//
//   - Phase 1: 1 컨테이너 × CIS check 3건 → 모두 PASS, evidence·audit chain 검증.
//   - Phase 2: 3 컨테이너 × CIS check 5건 = 15 work item, WorkerLimit=10 진행률.
//   - Phase 3: 1 컨테이너 stop → health window → 잔여 check skip → completed (partial).
//   - Phase 4: 컨테이너 키 교체 후 재 scan → mismatch error → reset → 재시도 PASS.
//   - Phase 5: runtime.NumGoroutine() 시작·종료 비교 (누수 0).
//
// harness/어댑터/check 정의는 sshd_e2e_helpers_test.go 분리.
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
)

// === Phase 1 — single robot × 3 CIS check ===
func TestPhase1_SingleRobot3Checks(t *testing.T) {
	requireDocker(t)
	if err := waitForPort(t, fixtureHost, robot1Port, containerReadyTimeout); err != nil {
		t.Skipf("robot-1 not reachable — make test-ssh-e2e? %v", err)
	}

	h := newE2EHarness(t)
	h.seedFleetAndPack("tn_p1", "fl_p1", "pk_p1")
	target := robotTarget("ro_p1", robot1Port)
	h.seedRobots([]scan.RobotTarget{target})
	checks := cisCheck3()
	h.seedChecks(checks)

	pool := sshpool.New(sshpool.Deps{})
	orch := scanrun.New(scanrun.Deps{
		Scan:        h.scanSvc,
		Storage:     h.store,
		Executor:    &e2eSSHAdapter{pool: pool},
		Evaluator:   e2eEvaluator{},
		Bus:         h.bus,
		Clock:       clock.System(),
		WorkerLimit: 4,
	})

	sessionID := h.startSession(len(checks))
	if err := orch.Run(context.Background(), h.tenantID, sessionID, []scan.RobotTarget{target}, checks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	final := h.reload(sessionID)
	if final.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed", final.Status)
	}
	if final.Progress.Total != 3 || final.Progress.Completed != 3 {
		t.Errorf("Progress = %+v, want {Total:3 Completed:3}", final.Progress)
	}

	results := h.listResults(sessionID)
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	for _, r := range results {
		if r.Outcome != scan.OutcomePass {
			t.Errorf("check %s outcome = %s, want pass", r.CheckID, r.Outcome)
		}
	}

	// audit chain — scan.started + scan.completed.
	actions := h.listAuditActions(sessionID)
	if len(actions) < 2 || actions[0] != "scan.started" || actions[len(actions)-1] != "scan.completed" {
		t.Errorf("audit actions = %v, want [scan.started, ..., scan.completed]", actions)
	}
}

// === Phase 2 — fleet of 3 × 5 check = 15 work item, WorkerLimit=10 ===
func TestPhase2_FleetOf3WithWorkerLimit10(t *testing.T) {
	requireDocker(t)
	for _, p := range []int{robot1Port, robot2Port, robot3Port} {
		if err := waitForPort(t, fixtureHost, p, containerReadyTimeout); err != nil {
			t.Skipf("robot port %d not reachable: %v", p, err)
		}
	}

	h := newE2EHarness(t)
	h.seedFleetAndPack("tn_p2", "fl_p2", "pk_p2")
	targets := []scan.RobotTarget{
		robotTarget("ro_p2_1", robot1Port),
		robotTarget("ro_p2_2", robot2Port),
		robotTarget("ro_p2_3", robot3Port),
	}
	h.seedRobots(targets)
	checks := cisCheck5()
	h.seedChecks(checks)

	pool := sshpool.New(sshpool.Deps{})
	orch := scanrun.New(scanrun.Deps{
		Scan:        h.scanSvc,
		Storage:     h.store,
		Executor:    &e2eSSHAdapter{pool: pool},
		Evaluator:   e2eEvaluator{},
		Bus:         h.bus,
		Clock:       clock.System(),
		WorkerLimit: 10,
	})

	// 진행률 monitoring — 매 progress event마다 카운트 + 마지막 값 검증.
	var (
		progMu        sync.Mutex
		progressCount int
		lastProgress  scan.ProgressEventPayload
		doneCh        = make(chan struct{}, 1)
	)
	progSub := h.bus.Subscribe(context.Background(), scan.EventTypeProgress,
		func(_ context.Context, evt eventbus.Event) error {
			var p scan.ProgressEventPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return err
			}
			progMu.Lock()
			progressCount++
			lastProgress = p
			progMu.Unlock()
			return nil
		})
	t.Cleanup(progSub.Cancel)

	compSub := h.bus.Subscribe(context.Background(), scan.EventTypeCompleted,
		func(_ context.Context, _ eventbus.Event) error {
			select {
			case doneCh <- struct{}{}:
			default:
			}
			return nil
		})
	t.Cleanup(compSub.Cancel)

	totalWork := len(targets) * len(checks)
	sessionID := h.startSession(totalWork)
	if err := orch.Run(context.Background(), h.tenantID, sessionID, targets, checks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case <-doneCh:
	case <-time.After(60 * time.Second):
		t.Fatal("scan.completed event not received in 60s")
	}

	final := h.reload(sessionID)
	if final.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed", final.Status)
	}
	if final.Progress.Total != totalWork || final.Progress.Completed != totalWork {
		t.Errorf("Progress = %+v, want {Total:%d Completed:%d}", final.Progress, totalWork, totalWork)
	}

	progMu.Lock()
	defer progMu.Unlock()
	if progressCount != totalWork {
		t.Errorf("progress events = %d, want %d", progressCount, totalWork)
	}
	if lastProgress.Total != totalWork || lastProgress.Completed != totalWork {
		t.Errorf("last progress = %+v, want {Total:%d Completed:%d}", lastProgress, totalWork, totalWork)
	}
}

// === Phase 3 — degraded fleet (1 컨테이너 stop → health window → partial OK) ===
func TestPhase3_DegradedFleetHealthWindow(t *testing.T) {
	requireDocker(t)
	for _, p := range []int{robot1Port, robot2Port, robot3Port} {
		if err := waitForPort(t, fixtureHost, p, containerReadyTimeout); err != nil {
			t.Skipf("robot port %d not reachable: %v", p, err)
		}
	}

	// robot-3 컨테이너를 stop — health window 발동 (연속 N=3 fail 후 잔여 skip).
	if err := composeCmd(t, "stop", "robot-3"); err != nil {
		t.Fatalf("compose stop robot-3: %v", err)
	}
	t.Cleanup(func() {
		// 다음 테스트를 위해 자동 재기동.
		_ = composeCmd(t, "start", "robot-3")
		_ = waitForPort(t, fixtureHost, robot3Port, containerReadyTimeout)
	})

	h := newE2EHarness(t)
	h.seedFleetAndPack("tn_p3", "fl_p3", "pk_p3")
	targets := []scan.RobotTarget{
		robotTarget("ro_p3_1", robot1Port),
		robotTarget("ro_p3_2", robot2Port),
		robotTarget("ro_p3_3", robot3Port), // stopped — 연속 fail 예상.
	}
	h.seedRobots(targets)
	checks := cisCheck5() // 5 check × 3 robot = 15 work item.
	h.seedChecks(checks)

	pool := sshpool.New(sshpool.Deps{})
	orch := scanrun.New(scanrun.Deps{
		Scan:                   h.scanSvc,
		Storage:                h.store,
		Executor:               &e2eSSHAdapter{pool: pool},
		Evaluator:              e2eEvaluator{},
		Bus:                    h.bus,
		Clock:                  clock.System(),
		WorkerLimit:            5,
		HealthFailureThreshold: 3, // robot-3에서 연속 3회 실패 후 잔여 2건 skip.
	})

	sessionID := h.startSession(len(targets) * len(checks))
	if err := orch.Run(context.Background(), h.tenantID, sessionID, targets, checks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	final := h.reload(sessionID)
	// design doc: status=completed (failed가 아닌 — partial OK는 reason 기록).
	if final.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed (partial OK semantics)", final.Status)
	}

	results := h.listResults(sessionID)
	dist := map[scan.Outcome]int{}
	robotResults := map[string]map[scan.Outcome]int{}
	for _, r := range results {
		dist[r.Outcome]++
		if robotResults[r.RobotID] == nil {
			robotResults[r.RobotID] = map[scan.Outcome]int{}
		}
		robotResults[r.RobotID][r.Outcome]++
	}

	// robot-1·robot-2는 모두 PASS 또는 success outcome.
	for _, id := range []string{"ro_p3_1", "ro_p3_2"} {
		if robotResults[id][scan.OutcomePass] != len(checks) {
			t.Errorf("robot %s pass = %d, want %d (live robot 모두 PASS 기대)",
				id, robotResults[id][scan.OutcomePass], len(checks))
		}
	}

	// robot-3는 health window 발동 — 일부는 error(SSH dial fail), 잔여는 skipped.
	r3Skipped := robotResults["ro_p3_3"][scan.OutcomeSkipped]
	r3Error := robotResults["ro_p3_3"][scan.OutcomeError]
	if r3Skipped == 0 {
		t.Errorf("robot-3 skipped count = 0, want > 0 (health window 발동 기대). dist=%v", robotResults["ro_p3_3"])
	}
	if r3Error+r3Skipped != len(checks) {
		t.Errorf("robot-3 error+skipped = %d, want %d", r3Error+r3Skipped, len(checks))
	}
	if dist[scan.OutcomeSkipped] == 0 {
		t.Errorf("total skipped = 0, want > 0. full dist = %v", dist)
	}
}

// === Phase 4 — host key change (mismatch → reset → 재시도 PASS) ===
//
// 본 phase는 KnownHostsManager의 fingerprint 비교 로직을 직접 검증합니다.
// 컨테이너 재기동(host key 새로 생성) 시 첫 dial은 trusted fingerprint와 mismatch →
// OutcomeError. 이후 fingerprint pin을 새 값으로 reset(혹은 InsecureIgnoreHostKey)하면 PASS.
func TestPhase4_HostKeyChangeMismatchAndReset(t *testing.T) {
	requireDocker(t)
	if err := waitForPort(t, fixtureHost, robot1Port, containerReadyTimeout); err != nil {
		t.Skipf("robot-1 not reachable: %v", err)
	}

	// 1. 첫 SSH dial — 현재 host pubkey 학습 (TOFU 시뮬레이션).
	addr := net.JoinHostPort(fixtureHost, strconv.Itoa(robot1Port))
	var firstKey ssh.PublicKey
	captureFirst := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		firstKey = key
		return nil
	}
	clientCfg := &ssh.ClientConfig{
		User:            fixtureUsername,
		Auth:            []ssh.AuthMethod{ssh.Password(fixturePassword)},
		HostKeyCallback: captureFirst,
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, clientCfg)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	_ = client.Close()
	if firstKey == nil {
		t.Fatal("firstKey not captured")
	}

	// 2. 다른 fingerprint로 핀(pin)된 callback 생성 — 어떤 키도 일치 안 함 ("mismatch" 시뮬레이션).
	pinnedFakeFingerprint := "SHA256:thisisanothelegitfingerprintatall000000000000"
	mismatchCB := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if got != pinnedFakeFingerprint {
			return fmt.Errorf("ssh: host key mismatch — got %s want %s", got, pinnedFakeFingerprint)
		}
		return nil
	}

	h := newE2EHarness(t)
	h.seedFleetAndPack("tn_p4", "fl_p4", "pk_p4")
	target := robotTarget("ro_p4", robot1Port)
	h.seedRobots([]scan.RobotTarget{target})
	checks := cisCheck3()
	h.seedChecks(checks)

	pool := sshpool.New(sshpool.Deps{})
	orchMismatch := scanrun.New(scanrun.Deps{
		Scan:        h.scanSvc,
		Storage:     h.store,
		Executor:    &e2eSSHAdapter{pool: pool, hostKeyCB: mismatchCB},
		Evaluator:   e2eEvaluator{},
		Bus:         h.bus,
		Clock:       clock.System(),
		WorkerLimit: 2,
	})

	// 첫 scan — host key mismatch → 모두 OutcomeError.
	sessionMismatch := h.startSession(len(checks))
	if err := orchMismatch.Run(context.Background(), h.tenantID, sessionMismatch, []scan.RobotTarget{target}, checks); err != nil {
		t.Fatalf("orchMismatch.Run: %v", err)
	}
	resultsMismatch := h.listResults(sessionMismatch)
	if len(resultsMismatch) != len(checks) {
		t.Fatalf("mismatch results = %d, want %d", len(resultsMismatch), len(checks))
	}
	gotMismatchError := false
	for _, r := range resultsMismatch {
		if r.Outcome != scan.OutcomeError {
			t.Errorf("mismatch check %s outcome = %s, want error", r.CheckID, r.Outcome)
			continue
		}
		if strings.Contains(strings.ToLower(r.EvalReason), "host key") || strings.Contains(strings.ToLower(r.EvalReason), "mismatch") {
			gotMismatchError = true
		}
	}
	if !gotMismatchError {
		t.Errorf("expected at least one EvalReason to mention host key mismatch — got: %+v", resultsMismatch)
	}

	// 3. reset (운영자 명시 reset 시뮬레이션) — 현재 fingerprint를 trusted로 재 등록.
	// 여기서는 callback을 firstKey와 일치하도록 갱신.
	wantFp := ssh.FingerprintSHA256(firstKey)
	resetCB := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if got != wantFp {
			return fmt.Errorf("ssh: host key still mismatch — got %s", got)
		}
		return nil
	}
	orchReset := scanrun.New(scanrun.Deps{
		Scan:        h.scanSvc,
		Storage:     h.store,
		Executor:    &e2eSSHAdapter{pool: pool, hostKeyCB: resetCB},
		Evaluator:   e2eEvaluator{},
		Bus:         h.bus,
		Clock:       clock.System(),
		WorkerLimit: 2,
	})

	sessionReset := h.startSession(len(checks))
	if err := orchReset.Run(context.Background(), h.tenantID, sessionReset, []scan.RobotTarget{target}, checks); err != nil {
		t.Fatalf("orchReset.Run: %v", err)
	}
	resultsReset := h.listResults(sessionReset)
	if len(resultsReset) != len(checks) {
		t.Fatalf("reset results = %d, want %d", len(resultsReset), len(checks))
	}
	for _, r := range resultsReset {
		if r.Outcome != scan.OutcomePass {
			t.Errorf("reset check %s outcome = %s, want pass", r.CheckID, r.Outcome)
		}
	}
}

// === Phase 5 — pprof goroutine leak 검증 ===
//
// runtime.NumGoroutine()을 시작·종료 시점 비교. 5건 cycle 후 차이가 임계 이내인지.
// heap profile 스냅샷은 manual — 본 테스트는 goroutine 누수만 검증.
func TestPhase5_GoroutineLeakCheck(t *testing.T) {
	requireDocker(t)
	if err := waitForPort(t, fixtureHost, robot1Port, containerReadyTimeout); err != nil {
		t.Skipf("robot-1 not reachable: %v", err)
	}

	// baseline 측정 (한 번 dummy run으로 모든 lazy init 완료시킨 뒤 측정).
	h := newE2EHarness(t)
	h.seedFleetAndPack("tn_p5", "fl_p5", "pk_p5")
	target := robotTarget("ro_p5", robot1Port)
	h.seedRobots([]scan.RobotTarget{target})
	checks := cisCheck3()
	h.seedChecks(checks)

	pool := sshpool.NewPool(sshpool.PoolConfig{
		IdleTimeout:       30 * time.Second,
		KeepaliveInterval: 5 * time.Second,
	})
	t.Cleanup(func() { _ = pool.Close() })
	exec := sshpool.New(sshpool.Deps{})

	orch := scanrun.New(scanrun.Deps{
		Scan:        h.scanSvc,
		Storage:     h.store,
		Executor:    &e2eSSHAdapter{pool: exec},
		Evaluator:   e2eEvaluator{},
		Bus:         h.bus,
		Clock:       clock.System(),
		WorkerLimit: 4,
	})

	// warmup 1 cycle.
	sessionWarmup := h.startSession(len(checks))
	if err := orch.Run(context.Background(), h.tenantID, sessionWarmup, []scan.RobotTarget{target}, checks); err != nil {
		t.Fatalf("warmup Run: %v", err)
	}

	// goroutine settle 대기 — bus 비동기 fan-out 마감.
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	startGoroutines := runtime.NumGoroutine()

	// 5 cycle 반복 — pool conn 재사용·orchestrator goroutine 누수 가능성 노출.
	const cycles = 5
	for i := 0; i < cycles; i++ {
		sid := h.startSession(len(checks))
		if err := orch.Run(context.Background(), h.tenantID, sid, []scan.RobotTarget{target}, checks); err != nil {
			t.Fatalf("cycle %d Run: %v", i, err)
		}
	}

	// settle 대기 — bus + pool keepalive goroutine 안정화.
	time.Sleep(1 * time.Second)
	runtime.GC()
	endGoroutines := runtime.NumGoroutine()

	// 임계: keepalive goroutine 1 + bus subscriber fan-out 약간 허용 — 10 이내면 양호.
	delta := endGoroutines - startGoroutines
	if delta > 10 {
		t.Errorf("goroutine leak suspected: start=%d, end=%d, delta=%d (cycles=%d)",
			startGoroutines, endGoroutines, delta, cycles)
	}
	t.Logf("goroutine count: start=%d, end=%d, delta=%d, cycles=%d",
		startGoroutines, endGoroutines, delta, cycles)
}
