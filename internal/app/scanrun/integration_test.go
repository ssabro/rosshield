// integration_test.go — E6 Stage D.3 Exit 검증 (R7-2~R7-7).
//
// mock 0 — 진짜 결선만 사용:
//
//   - sshpool.Executor  ← in-proc fake SSH 서버 (sshpooltest.FakeSSHD × 3)
//   - benchmark.ParseEvalRule + EvalNode.Eval  ← sealed AST evaluator
//   - scan.Service 결선 (sqliterepo + auditAdapter)
//   - scanrun.Orchestrator
//
// 시나리오 (R7-5):
//
//	3 robot × 3 check = 9 work item
//	check 1 (T7 단일 contains): "PermitRootLogin no"
//	check 2 (T7 단일 regex)   : "(?m)^Port\s+22$"
//	check 3 (T8 composite)   : and(contains "Match Address", not(contains "AllowUsers root"))
//
//	robot1 (good profile)    : 3 PASS
//	robot2 (partial profile) : 2 PASS, 1 FAIL (Port 2222)
//	robot3 (bad profile)     : 3 FAIL (root 허용 + 다른 포트 + 잘못된 sshd_config)
//
//	총 5 PASS + 4 FAIL.
//
// 검증 (R7-6):
//
//   - session.Status = completed, Progress = {Total: 9, Completed: 9, Failed: 4}
//   - scan_results 9 row, outcome 분포 정확
//   - audit_entries 정확한 action 두 건 (scan.started + scan.completed)
//   - EventBus: progress 9건 + completed 1건
//   - goleak: orchestrator 종료 후 누수 0 (R7-4)
package scanrun_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
	"golang.org/x/crypto/ssh"

	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/sshpool/sshpooltest"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// === 진짜 sshpool.Executor를 scan.SSHExecutor로 결선하는 통합 어댑터 ===
//
// 통합 테스트 단순화: fakesshd가 NoClientAuth=true이므로 비밀번호 검증 우회.
// 호스트 키도 InsecureIgnoreHostKey — production은 first-touch trust(R4-2)이지만
// 통합 검증의 핵심은 "Orchestrator → sshpool.Executor → fake → evaluator → DB" 결선이지
// 호스트 키 회수 메커니즘이 아님 (그건 sshpool 단위 테스트가 검증).
type integrationSSHAdapter struct {
	pool sshpool.Executor
}

func (a *integrationSSHAdapter) Exec(ctx context.Context, target scan.RobotTarget, argv []string, timeout time.Duration) (scan.ExecResult, error) {
	res, err := a.pool.Exec(ctx, sshpool.Target{
		Host:            target.Host,
		Port:            target.Port,
		Username:        "u",
		Auth:            ssh.Password("ignored"),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}, argv, timeout)
	return scan.ExecResult{
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		ExitCode: res.ExitCode,
		Duration: res.Duration,
	}, err
}

// === 진짜 benchmark sealed AST evaluator를 scan.CheckEvaluator로 결선 ===
//
// cmd/rosshield-server/scanexec.go의 benchmarkEvaluatorAdapter와 동일 로직 — 통합 테스트가
// import할 수 없으므로 같은 결선 코드를 작성 (P5: 도메인 결선은 호출자 책임).
type integrationBenchmarkAdapter struct{}

func (integrationBenchmarkAdapter) Evaluate(ruleJSON []byte, exec scan.ExecResult) (scan.EvalResult, error) {
	node, err := benchmark.ParseEvalRule(json.RawMessage(ruleJSON))
	if err != nil {
		return scan.EvalResult{}, fmt.Errorf("integ eval parse: %w", err)
	}
	res, err := node.Eval(benchmark.EvalInput{
		Stdout:   string(exec.Stdout),
		Stderr:   string(exec.Stderr),
		ExitCode: exec.ExitCode,
	})
	if err != nil {
		return scan.EvalResult{}, fmt.Errorf("integ eval: %w", err)
	}
	return scan.EvalResult{Outcome: integrationMapStatus(res.Status), Reason: res.Reason}, nil
}

// verifyNoLeak는 테스트 시작 시 호출하면 모든 t.Cleanup 실행 후 goroutine 누수를 검증합니다.
//
// IgnoreCurrent()로 호출 시점의 active goroutine snapshot을 무시 — process-wide
// modernc.org/sqlite 백그라운드 등은 자연 제외. 이후 새로 만든 fakesshd/store/bus의
// cleanup이 모두 끝난 시점에 verify가 실행되어야 의미 있음.
//
// t.Cleanup LIFO 특성: 첫 등록이 마지막 실행 → 다른 cleanup이 모두 끝난 뒤 verify.
func verifyNoLeak(t *testing.T) {
	t.Helper()
	snapshot := goleak.IgnoreCurrent()
	t.Cleanup(func() {
		goleak.VerifyNone(t, snapshot)
	})
}

func integrationMapStatus(s benchmark.EvalStatus) scan.Outcome {
	switch s {
	case benchmark.StatusPass:
		return scan.OutcomePass
	case benchmark.StatusFail:
		return scan.OutcomeFail
	case benchmark.StatusIndeterminate:
		return scan.OutcomeIndeterminate
	default:
		return scan.OutcomeError
	}
}

// === fake SSHD 응답 핸들러 ===
//
// argv를 sshpool.JoinArgv가 single-quote 직렬화하므로 cmd 문자열 안에서
// 특정 키워드를 찾아 robot profile에 맞는 stdout 반환.
func makeProfileHandler(profile string) sshpooltest.ExecHandler {
	return func(cmd string) sshpooltest.ExecResponse {
		switch {
		case strings.Contains(cmd, "PermitRootLogin"):
			if profile == "good" || profile == "partial" {
				return sshpooltest.ExecResponse{Stdout: "PermitRootLogin no\n"}
			}
			return sshpooltest.ExecResponse{Stdout: "PermitRootLogin yes\n"}
		case strings.Contains(cmd, "'^Port'"): // grep -E '^Port' 직렬화 결과
			if profile == "good" {
				return sshpooltest.ExecResponse{Stdout: "Port 22\n"}
			}
			return sshpooltest.ExecResponse{Stdout: "Port 2222\n"}
		case strings.Contains(cmd, "'cat'"):
			if profile == "good" || profile == "partial" {
				return sshpooltest.ExecResponse{Stdout: "Match Address 10.0.0.0/8\nAllowGroups admins\n"}
			}
			// bad: AllowUsers root 포함 → composite check fail
			return sshpooltest.ExecResponse{Stdout: "AllowUsers root\nMatch Address 0.0.0.0/0\n"}
		}
		return sshpooltest.ExecResponse{}
	}
}

// makeIntegrationChecks는 T7(단일 op 2종) + T8(composite) check 3개를 정의합니다.
//
// PackCheckID는 harness.seedChecks(3)이 만드는 "ck_000"·"ck_001"·"ck_002"와 매칭 — DB에 없는
// PackCheckID로 RecordResult하면 silently 누락됨.
func makeIntegrationChecks() []scan.CheckDef {
	return []scan.CheckDef{
		{
			PackCheckID:  "ck_000",
			Code:         "CIS-1.1",
			AuditCommand: []string{"sudo", "grep", "-i", "PermitRootLogin", "/etc/ssh/sshd_config"},
			TimeoutSec:   2,
			EvalRuleJSON: []byte(`{"op":"contains","value":"PermitRootLogin no"}`),
		},
		{
			PackCheckID:  "ck_001",
			Code:         "CIS-1.2",
			AuditCommand: []string{"sudo", "grep", "-E", "^Port", "/etc/ssh/sshd_config"},
			TimeoutSec:   2,
			EvalRuleJSON: []byte(`{"op":"regex","pattern":"(?m)^Port\\s+22$"}`),
		},
		{
			PackCheckID:  "ck_002",
			Code:         "CIS-1.3",
			AuditCommand: []string{"sudo", "cat", "/etc/ssh/sshd_config"},
			TimeoutSec:   2,
			EvalRuleJSON: []byte(`{"op":"and","args":[{"op":"contains","value":"Match Address"},{"op":"not","arg":{"op":"contains","value":"AllowUsers root"}}]}`),
		},
	}
}

// makeRobotTargetsForEndpoints는 fakesshd 엔드포인트별 RobotTarget을 생성합니다.
//
// h.robotIDs와 endpoints는 동일 인덱스 매핑 — robotIDs[i] ↔ endpoints[i].Host:Port.
func makeRobotTargetsForEndpoints(h *harness, endpoints []*sshpooltest.FakeSSHD) []scan.RobotTarget {
	if len(endpoints) != len(h.robotIDs) {
		h.t.Fatalf("endpoints and robots count mismatch: %d vs %d", len(endpoints), len(h.robotIDs))
	}
	out := make([]scan.RobotTarget, 0, len(h.robotIDs))
	for i, id := range h.robotIDs {
		out = append(out, scan.RobotTarget{
			RobotID:      id,
			Host:         endpoints[i].Host,
			Port:         endpoints[i].Port,
			AuthType:     "password",
			CredentialID: "cr_x",
		})
	}
	return out
}

// listAuditEntries는 sqlite store에서 audit entries를 직접 조회합니다.
//
// sqliterepo.audit는 List 미구현이므로 raw SELECT — 통합 검증 한정.
func listAuditEntries(t *testing.T, h *harness, sessionID string) []string {
	t.Helper()
	var actions []string
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			rows, err := tx.Query(ctx, `SELECT action FROM audit_entries WHERE target_type=? AND target_id=? ORDER BY seq ASC`,
				"scan_session", sessionID)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var a string
				if err := rows.Scan(&a); err != nil {
					return err
				}
				actions = append(actions, a)
			}
			return rows.Err()
		}); err != nil {
		t.Fatalf("listAuditEntries: %v", err)
	}
	return actions
}

// === Stage D.3 핵심 시나리오 — 3 robot × 3 check end-to-end ===
//
// 전체 결선이 통과한 뒤 goleak으로 누수 검증 (R7-4).
//
// goleak 호출 순서 주의: defer는 t.Cleanup *전*에 실행되므로 사용 X — 대신 t.Cleanup으로
// 가장 먼저 등록(LIFO 마지막 실행)해서 fakesshd/store/bus cleanup이 모두 끝난 뒤 검증.
func TestIntegration3RobotsX3ChecksEndToEnd(t *testing.T) {
	verifyNoLeak(t)

	// 0. fakesshd 3개 (good/partial/bad profile).
	endpoints := []*sshpooltest.FakeSSHD{
		sshpooltest.New(t, makeProfileHandler("good")),
		sshpooltest.New(t, makeProfileHandler("partial")),
		sshpooltest.New(t, makeProfileHandler("bad")),
	}

	// 1. 표준 harness — scanSvc·storage·bus 결선. orch는 mock인데 곧 교체.
	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_INT", "fl_INT", "pk_INT")
	h.seedRobots(3) // robot ID 3개를 순서대로 endpoints[0..2]와 매핑.
	h.seedChecks(3) // check 3개 — eval rule은 mock용 더미라 통합 테스트엔 안 씀.

	// 2. 진짜 sshpool.Executor + 통합 어댑터로 Orchestrator 재구성.
	// (h.scanSvc는 이미 newHarness에서 audit 결선 완료 — auditAdapter가 audit_entries 기록)
	pool := sshpool.New(sshpool.Deps{}) // Executor (실 sshpool.Pool은 R7 통합 핵심 아님)
	realOrch := scanrun.New(scanrun.Deps{
		Scan:        h.scanSvc,
		Storage:     h.store,
		Executor:    &integrationSSHAdapter{pool: pool},
		Evaluator:   integrationBenchmarkAdapter{},
		Bus:         h.bus,
		Clock:       clock.System(),
		WorkerLimit: 4,
	})

	// 3. 이벤트 수집 — progress 9건 + completed 1건 검증.
	var (
		evMu       sync.Mutex
		progresses []scan.ProgressEventPayload
		completed  scan.CompletedEventPayload
		doneCh     = make(chan struct{})
	)
	progSub := h.bus.Subscribe(context.Background(), scan.EventTypeProgress,
		func(_ context.Context, evt eventbus.Event) error {
			var p scan.ProgressEventPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return err
			}
			evMu.Lock()
			progresses = append(progresses, p)
			evMu.Unlock()
			return nil
		})
	t.Cleanup(progSub.Cancel)

	compSub := h.bus.Subscribe(context.Background(), scan.EventTypeCompleted,
		func(_ context.Context, evt eventbus.Event) error {
			evMu.Lock()
			defer evMu.Unlock()
			_ = json.Unmarshal(evt.Payload, &completed)
			close(doneCh)
			return nil
		})
	t.Cleanup(compSub.Cancel)

	// 4. session start + Run.
	sessionID := h.startSession(9)
	targets := makeRobotTargetsForEndpoints(h, endpoints)
	checks := makeIntegrationChecks()

	if err := realOrch.Run(context.Background(), h.tenantID, sessionID, targets, checks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 5. completed 이벤트 도착 대기 (publish는 Run 동기 종료 전 발생하지만 구독자 fan-out은 비동기).
	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("scan.completed event not received in 3s")
	}

	// 6. session 상태 검증 — Total 9, Completed 9, Failed 4.
	final := h.reload(sessionID)
	if final.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed", final.Status)
	}
	if final.Progress.Total != 9 {
		t.Errorf("Progress.Total = %d, want 9", final.Progress.Total)
	}
	if final.Progress.Completed != 9 {
		t.Errorf("Progress.Completed = %d, want 9", final.Progress.Completed)
	}
	if final.Progress.Failed != 4 {
		t.Errorf("Progress.Failed = %d, want 4 (robot2 ck_002 + robot3 all 3)", final.Progress.Failed)
	}

	// 7. scan_results 9 row + outcome 분포 검증.
	results := h.listResults(sessionID)
	if len(results) != 9 {
		t.Fatalf("results = %d, want 9", len(results))
	}
	dist := map[scan.Outcome]int{}
	for _, r := range results {
		dist[r.Outcome]++
	}
	if dist[scan.OutcomePass] != 5 {
		t.Errorf("pass count = %d, want 5", dist[scan.OutcomePass])
	}
	if dist[scan.OutcomeFail] != 4 {
		t.Errorf("fail count = %d, want 4", dist[scan.OutcomeFail])
	}
	if dist[scan.OutcomeError] != 0 {
		t.Errorf("error count = %d, want 0 (no SSH/eval errors expected)", dist[scan.OutcomeError])
	}

	// 8. audit chain — scan.started + scan.completed 정확히 1건씩 (Stage C audit 결선 검증).
	auditActions := listAuditEntries(t, h, sessionID)
	wantActions := []string{"scan.started", "scan.completed"}
	if len(auditActions) != len(wantActions) {
		t.Errorf("audit actions = %v, want %v", auditActions, wantActions)
	} else {
		for i := range wantActions {
			if auditActions[i] != wantActions[i] {
				t.Errorf("audit[%d] = %q, want %q", i, auditActions[i], wantActions[i])
			}
		}
	}

	// 9. EventBus — progress 9건 + completed 1건.
	evMu.Lock()
	defer evMu.Unlock()
	if len(progresses) != 9 {
		t.Errorf("progress events = %d, want 9", len(progresses))
	}
	if completed.SessionID != sessionID {
		t.Errorf("completed.SessionID = %q, want %q", completed.SessionID, sessionID)
	}
	if completed.Status != "completed" {
		t.Errorf("completed.Status = %q, want completed", completed.Status)
	}
	if completed.Total != 9 || completed.Completed != 9 || completed.Failed != 4 {
		t.Errorf("completed payload = {Total:%d Completed:%d Failed:%d}, want {9 9 4}",
			completed.Total, completed.Completed, completed.Failed)
	}
}

// === Stage D.3 보조 — fake SSHD가 audit chain의 결선까지 검증 ===
//
// 단일 robot × 단일 check를 2번 연속 실행해서 두 session의 audit chain이
// seq 연속(같은 tenant 한 head)인지 raw 검증.
func TestIntegrationAuditChainAcrossSessions(t *testing.T) {
	verifyNoLeak(t)

	endpoint := sshpooltest.New(t, makeProfileHandler("good"))

	h := newHarness(t, 1)
	h.seedFleetAndPack("tn_AC", "fl_AC", "pk_AC")
	h.seedRobots(1)
	h.seedChecks(1)

	pool := sshpool.New(sshpool.Deps{})
	orch := scanrun.New(scanrun.Deps{
		Scan: h.scanSvc, Storage: h.store,
		Executor: &integrationSSHAdapter{pool: pool}, Evaluator: integrationBenchmarkAdapter{},
		Bus: h.bus, Clock: clock.System(), WorkerLimit: 1,
	})

	checks := []scan.CheckDef{{
		PackCheckID:  "ck_000", // seedChecks(1) → ck_000 단일
		Code:         "CIS-AC",
		AuditCommand: []string{"sudo", "grep", "-i", "PermitRootLogin", "/etc/ssh/sshd_config"},
		TimeoutSec:   2,
		EvalRuleJSON: []byte(`{"op":"contains","value":"PermitRootLogin no"}`),
	}}
	targets := makeRobotTargetsForEndpoints(h, []*sshpooltest.FakeSSHD{endpoint})

	for i := 0; i < 2; i++ {
		sessionID := h.startSession(1)
		if err := orch.Run(context.Background(), h.tenantID, sessionID, targets, checks); err != nil {
			t.Fatalf("Run #%d: %v", i, err)
		}
	}

	// audit chain — 두 session에서 4 entry (started·completed × 2) 모두 같은 head 기준.
	var seqs []int
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			rows, err := tx.Query(ctx, `SELECT seq FROM audit_entries WHERE tenant_id=? ORDER BY seq ASC`, string(h.tenantID))
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var s int
				if err := rows.Scan(&s); err != nil {
					return err
				}
				seqs = append(seqs, s)
			}
			return rows.Err()
		}); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(seqs) != 4 {
		t.Fatalf("audit entries = %d, want 4 (2 sessions × started+completed)", len(seqs))
	}
	for i, s := range seqs {
		if s != i+1 {
			t.Errorf("seq[%d] = %d, want %d (chain monotonic)", i, s, i+1)
		}
	}
}
