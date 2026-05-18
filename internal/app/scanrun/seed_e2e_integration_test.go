package scanrun_test

// seed_e2e_integration_test.go — built-in pack(E12)을 InstallPack한 뒤 그 pack의 첫 N개
// check를 fakesshd 1개에 대해 한 cycle 돌려, "seed loader → 실 check → 실 SSH → 실
// evaluator → 실 DB → 실 audit chain"의 모든 결선이 통과한다는 single-shot smoke.
//
// 기존 integration_test.go는 임시 check 정의로 mechanics 검증.
// 본 파일은 실 builtin pack 자산을 사용해 pack converter→evaluator 매핑 회귀 보호.
//
// fakesshd가 모든 명령에 "** PASS **" stdout 반환 → 모든 selftest fixture check가
// `{op:contains, value:** PASS **}` 패턴이라 모두 PASS.
//
// embed _archives 비어있으면 t.Skip — make pack-archive 미실행.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ssabro/rosshield/internal/app/scanrun"
	builtinpacks "github.com/ssabro/rosshield/internal/builtin/packs"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	benchmarkrepo "github.com/ssabro/rosshield/internal/domain/benchmark/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/sshpool/sshpooltest"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// nullBenchmarkAuditEmitter — benchmark.AuditEmitter no-op (audit emit은 본 e2e의
// 핵심이 아니라 scan.* entry만 검증).
type nullBenchmarkAuditEmitter struct{}

func (nullBenchmarkAuditEmitter) EmitPackInstalled(_ context.Context, _ storage.Tx, _ benchmark.Pack, _ string) error {
	return nil
}
func (nullBenchmarkAuditEmitter) EmitPackLifecycleChanged(_ context.Context, _ storage.Tx, _ string, _, _ benchmark.State, _, _ string) error {
	return nil
}

// extractFirstCheck는 archive .tar.gz에서 checks/<id>.yaml 파일 1개를 골라 raw bytes를 반환합니다.
//
// pack converter가 파일명 알파벳 정렬로 push하므로 사실상 결정적.
// archive 내부의 checks/ 디렉터리의 첫 항목을 반환.
func extractFirstCheck(t *testing.T, tarGz []byte) (filename string, content []byte) {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(tarGz))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		if !strings.HasPrefix(h.Name, "checks/") || !strings.HasSuffix(h.Name, ".yaml") {
			continue
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		return h.Name, buf
	}
	t.Fatal("no checks/*.yaml in archive")
	return "", nil
}

// parseCheckYAML는 pack의 check yaml 1개를 scan.CheckDef로 변환합니다.
//
// pack converter가 출력하는 형식: apiVersion + kind=Check + metadata + spec.
// 여기서 spec.auditCommand(string, bash 직번역)와 spec.evaluationRule(any)을 추출.
func parseCheckYAML(t *testing.T, content []byte, packCheckID string) scan.CheckDef {
	t.Helper()
	var raw struct {
		Metadata struct {
			ID       string `yaml:"id"`
			Severity string `yaml:"severity"`
		} `yaml:"metadata"`
		Spec struct {
			AuditCommand   string                 `yaml:"auditCommand"`
			EvaluationRule map[string]interface{} `yaml:"evaluationRule"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(content, &raw); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if raw.Spec.AuditCommand == "" {
		t.Fatal("auditCommand empty")
	}
	if raw.Spec.EvaluationRule == nil {
		t.Fatal("evaluationRule empty")
	}
	ruleJSON, err := json.Marshal(raw.Spec.EvaluationRule)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}
	// pack converter가 만드는 auditCommand는 bash one-liner(string).
	// scan.CheckDef.AuditCommand는 argv []string — bash -c <cmd>으로 wrap.
	return scan.CheckDef{
		PackCheckID:  packCheckID,
		Code:         raw.Metadata.ID,
		AuditCommand: []string{"bash", "-c", raw.Spec.AuditCommand},
		TimeoutSec:   10,
		EvalRuleJSON: ruleJSON,
	}
}

// TestE2EWithSeededBuiltinPack — built-in pack 자산 → InstallPack → fakesshd → orchestrator → DB.
//
// 시나리오:
//  1. builtinpacks.Builtins() 첫 pack을 호출자 tenant에 InstallPack
//  2. archive에서 checks/<first>.yaml 한 개 추출 + scan.CheckDef로 변환
//  3. fakesshd 1개(모든 cmd에 "** PASS **" 응답) + robot 1개 시드
//  4. scanrun.Orchestrator.Run 한 cycle
//  5. session=completed, 결과 1개 PASS, audit started+completed 2건 검증
func TestE2EWithSeededBuiltinPack(t *testing.T) {
	verifyNoLeak(t)

	// 0. embed pack 가져오기
	packs, err := builtinpacks.Builtins()
	if err != nil {
		t.Skipf("no built-in packs embedded: %v (run 'make pack-archive')", err)
	}
	if len(packs) == 0 {
		t.Skip("Builtins() returned empty")
	}
	first := packs[0]

	// 1. harness — store/scanSvc/bus + tenant/fleet/robot 시드.
	h := newHarness(t, 1)
	h.seedFleetAndPack("tn_E2E", "fl_E2E", "pk_PLACEHOLDER") // pk_PLACEHOLDER는 InstallPack이 만든 pack과 별개라 무시
	h.seedRobots(1)

	// 2. benchmark.Service 결선 + InstallPack(호출자 tenant).
	benchSvc := benchmarkrepo.New(benchmarkrepo.Deps{
		Clock:              clock.System(),
		IDGen:              idgen.NewULID(),
		Audit:              nullBenchmarkAuditEmitter{},
		DefaultSignerKeyID: "test-pack-signer",
	})
	tenantCtx := storage.WithTenantID(context.Background(), h.tenantID)
	var installedPack benchmark.Pack
	if err := h.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		// 첫 trust 키(dev signer)로 install — dev 빌드 archive와 매칭.
		te := first.TrustBundle[0]
		p, e := benchSvc.InstallPack(ctx, tx, h.tenantID, first.TarGz, te.PublicKey, te.SignerKeyID, "test-actor")
		if e != nil {
			return e
		}
		installedPack = p
		return nil
	}); err != nil {
		t.Fatalf("InstallPack: %v", err)
	}
	if len(installedPack.Checks) == 0 {
		t.Fatalf("installed pack has 0 checks (filename=%s)", first.Filename)
	}

	// 3. archive에서 첫 check yaml 추출 + scan.CheckDef로 변환.
	// PackCheckID는 InstallPack이 만든 첫 check의 ID와 매칭.
	_, checkYAML := extractFirstCheck(t, first.TarGz)
	checkDef := parseCheckYAML(t, checkYAML, installedPack.Checks[0].ID)

	// 4. fakesshd — 모든 명령에 "** PASS **" 응답.
	fake := sshpooltest.New(t, func(_ string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{Stdout: "** PASS **\n"}
	})

	// 5. 진짜 sshpool.Executor + integration adapter로 Orchestrator 재구성.
	pool := sshpool.New(sshpool.Deps{})
	orch := scanrun.New(scanrun.Deps{
		Scan:        h.scanSvc,
		Storage:     h.store,
		Executor:    &integrationSSHAdapter{pool: pool},
		Evaluator:   integrationBenchmarkAdapter{},
		Bus:         h.bus,
		Clock:       clock.System(),
		WorkerLimit: 1,
	})

	// 6. session start + Run.
	sessionID := h.startSession(1)
	targets := makeRobotTargetsForEndpoints(h, []*sshpooltest.FakeSSHD{fake})
	if err := orch.Run(context.Background(), h.tenantID, sessionID, targets, []scan.CheckDef{checkDef}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 7. session 상태 검증 — Total 1, Completed 1, Failed 0.
	final := h.reload(sessionID)
	if final.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed", final.Status)
	}
	if final.Progress.Total != 1 {
		t.Errorf("Total = %d, want 1", final.Progress.Total)
	}
	if final.Progress.Completed != 1 {
		t.Errorf("Completed = %d, want 1", final.Progress.Completed)
	}
	if final.Progress.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (** PASS ** marker)", final.Progress.Failed)
	}

	// 8. scan_results 1 row + outcome=PASS 검증.
	results := h.listResults(sessionID)
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Outcome != scan.OutcomePass {
		t.Errorf("outcome = %s, want PASS (got reason=%q)", results[0].Outcome, results[0].EvalReason)
	}

	// 9. fakesshd가 정확히 1번의 명령 수신 + auditCommand 포함 검증.
	cmds := fake.ReceivedCmds()
	if len(cmds) != 1 {
		t.Errorf("fakesshd received %d cmds, want 1", len(cmds))
	}
	// pack converter는 auditCommand를 bash one-liner로 만들고 scan.CheckDef는 [bash -c <line>]
	// → sshpool.JoinArgv가 single-quote escape → 받은 cmd에 'bash' '-c' '<line escape>' 포함.
	if len(cmds) > 0 && !strings.Contains(cmds[0], "'bash'") {
		t.Errorf("cmd[0] = %q, want to contain 'bash' (argv quoting)", cmds[0])
	}

	// 10. audit chain — scan.started + scan.completed 정확히 1건씩.
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

	// 11. 메트릭 — installedPack.Checks 카운트는 yaml 자체 수와 일치.
	if installedPack.Checks[0].ID != checkDef.PackCheckID {
		t.Errorf("PackCheckID drift: installed=%q, used=%q", installedPack.Checks[0].ID, checkDef.PackCheckID)
	}
}
