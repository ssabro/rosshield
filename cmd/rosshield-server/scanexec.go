// scanexec.go — E6 Stage D.2 결선 어댑터.
//
// scan 도메인은 외부 도메인을 import하지 않으므로 (P5), bootstrap이 결선 글루:
//
//   - sshExecutorAdapter: scan.SSHExecutor → sshpool.Executor
//     · 매 Exec마다 별도 Tx로 robot.Service.GetCredentialMaterial 호출 → unwrap material
//     · CredentialMaterial → ssh.AuthMethod 변환
//     · sshpool.Target 구성 → sshpool.Executor.Exec 호출 → scan.ExecResult 반환
//     · host key는 KnownHostsManager가 robot 별 callback 생성 (scanrun SSH 통합 Stage 3)
//     · scan.ExecOpts.RequiresSudo → sshpool.Target.SudoMode = SudoNonInteractive 매핑
//
//   - benchmarkEvaluatorAdapter: scan.CheckEvaluator → benchmark.ParseEvalRule + EvalNode.Eval
//     · ruleJSON을 매번 parse — Phase 1 단순화 (cache는 후속)
//     · benchmark.EvalStatus 3-값 → scan.Outcome 3-값 매핑 (PASS/FAIL/INDETERMINATE)
//     · evaluator error는 그대로 반환 — Orchestrator가 OutcomeError로 매핑
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// sshExecutorAdapter는 scan.SSHExecutor를 sshpool.Pool + robot 자격증명 unwrap으로 결선합니다.
//
// scanrun SSH 통합 Stage 5b — Pool.Acquire → ExecOnClient → release 패턴.
// IdleTimeout > 0이면 Pool이 idle conn 재사용(같은 robot 다음 check가 새 dial 회피).
// pool 또는 executor 둘 다 nil이면 ErrAdapterNotConfigured. 둘 다 set이면 pool 우선.
type sshExecutorAdapter struct {
	pool      sshpool.Pool     // scanrun Stage 5b — 결선 우선. nil이면 executor fallback.
	executor  sshpool.Executor // 호환 fallback (Stage 5b 이전 결선·단위 테스트).
	robot     robot.Service
	storage   storage.Storage
	khMgr     *sshpool.KnownHostsManager // scanrun Stage 3 — TOFU host key callback 팩토리. nil 허용.
	hostKeyCB ssh.HostKeyCallback        // 호환 fallback (Stage 3 이전 결선·테스트). khMgr 우선.
	logger    *slog.Logger
	execMetr  sshpool.ExecMetrics // ExecOnClient에 위임 (Pool 경로). executor 경로는 Deps.Metrics 자체 결선.
}

// Exec은 scan.SSHExecutor 구현입니다.
//
// 절차:
//  1. ctx의 TenantID로 별도 Tx 시작 → robot.Service.GetCredentialMaterial → CredentialMaterial unwrap
//  2. CredentialMaterial → ssh.AuthMethod 변환
//  3. host key callback 결정 — khMgr 우선(robot 별 TOFU), fallback hostKeyCB
//  4. opts.RequiresSudo → sshpool.Target.SudoMode 매핑
//  5. Pool 결선 시 — Pool.Acquire → ExecOnClient → release. Executor fallback 시 — sshpool.Executor.Exec.
//  6. sshpool.ExecResult → scan.ExecResult 변환 후 반환
//
// material은 함수 종료 시 GC — 평문 자격증명을 Orchestrator로 노출하지 않음 (보안 측면).
func (a *sshExecutorAdapter) Exec(ctx context.Context, target scan.RobotTarget, argv []string, timeout time.Duration, opts scan.ExecOpts) (scan.ExecResult, error) {
	// 1. material unwrap.
	var mat robot.CredentialMaterial
	if err := a.storage.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		m, err := a.robot.GetCredentialMaterial(c, tx, target.RobotID)
		mat = m
		return err
	}); err != nil {
		return scan.ExecResult{}, fmt.Errorf("scanexec: unwrap credential: %w", err)
	}

	// 2. CredentialMaterial → ssh.AuthMethod.
	authMethod, err := materialToAuthMethod(mat)
	if err != nil {
		return scan.ExecResult{}, fmt.Errorf("scanexec: %w", err)
	}

	// 3. host key callback — khMgr 우선(robot 별 TOFU), fallback hostKeyCB.
	tenantID := storage.TenantIDFromContext(ctx)
	var hostKeyCB ssh.HostKeyCallback
	if a.khMgr != nil {
		hostKeyCB = a.khMgr.HostKeyCallback(ctx, tenantID, target.RobotID)
	} else {
		hostKeyCB = a.hostKeyCB
	}

	// 4. opts.RequiresSudo → SudoMode 매핑 (D-SCAN-3 — non-interactive 고정).
	sudoMode := sshpool.SudoNone
	if opts.RequiresSudo {
		sudoMode = sshpool.SudoNonInteractive
	}

	// 5. sshpool.Target 구성.
	sshTarget := sshpool.Target{
		Host:            target.Host,
		Port:            target.Port,
		Username:        mat.Username,
		Auth:            authMethod,
		HostKeyCallback: hostKeyCB,
		SudoMode:        sudoMode,
	}

	// Stage 5b — Pool 결선 우선. Pool.Acquire → ExecOnClient → release.
	if a.pool != nil {
		// PoolKey는 (TenantID, KeyID, Host, Port) 4-tuple. KeyID는 credential ID 사용
		// (자격증명 변경 시 stale conn 회피). KEK 식별자는 별도 정보 부재 — credential ID로 대체.
		key := sshpool.PoolKey{
			TenantID: string(tenantID),
			KeyID:    target.CredentialID,
			Host:     target.Host,
			Port:     target.Port,
		}
		client, release, err := a.pool.Acquire(ctx, key, sshTarget)
		if err != nil {
			return scan.ExecResult{}, fmt.Errorf("scanexec: pool acquire: %w", err)
		}
		defer release()

		res, err := sshpool.ExecOnClient(ctx, client, sshpool.ExecOnClientOpts{
			Argv:     argv,
			Timeout:  timeout,
			SudoMode: sudoMode,
			Logger:   a.logger,
			Metrics:  a.execMetr,
			LogHost:  target.Host,
		})
		return scan.ExecResult{
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			ExitCode: res.ExitCode,
			Duration: res.Duration,
		}, err
	}

	// Fallback: 기존 executor 경로 (호환).
	res, err := a.executor.Exec(ctx, sshTarget, argv, timeout)
	if err != nil {
		return scan.ExecResult{
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			ExitCode: res.ExitCode,
			Duration: res.Duration,
		}, err
	}
	return scan.ExecResult{
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		ExitCode: res.ExitCode,
		Duration: res.Duration,
	}, nil
}

// materialToAuthMethod는 CredentialMaterial을 ssh.AuthMethod로 변환합니다.
func materialToAuthMethod(mat robot.CredentialMaterial) (ssh.AuthMethod, error) {
	switch mat.Type {
	case robot.CredentialTypePassword:
		if mat.Password == "" {
			return nil, fmt.Errorf("password material is empty")
		}
		return ssh.Password(mat.Password), nil
	case robot.CredentialTypePrivateKey:
		if mat.PrivateKeyPEM == "" {
			return nil, fmt.Errorf("private key material is empty")
		}
		var (
			signer ssh.Signer
			err    error
		)
		if mat.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(mat.PrivateKeyPEM), []byte(mat.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(mat.PrivateKeyPEM))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return ssh.PublicKeys(signer), nil
	default:
		return nil, fmt.Errorf("unsupported credential type: %s", mat.Type)
	}
}

// benchmarkEvaluatorAdapter는 scan.CheckEvaluator를 benchmark의 sealed AST evaluator로 결선합니다.
type benchmarkEvaluatorAdapter struct{}

// Evaluate은 scan.CheckEvaluator 구현입니다.
//
// 절차:
//  1. ruleJSON → benchmark.ParseEvalRule → EvalNode (sealed AST)
//  2. exec → benchmark.EvalInput (Stdout/Stderr는 string으로 변환)
//  3. EvalNode.Eval(input) → EvalResult (PASS/FAIL/INDETERMINATE)
//  4. benchmark.EvalStatus → scan.Outcome 매핑
//
// Phase 1 단순화: ruleJSON을 매번 parse. Stage D.3·후속에서 cache 도입 검토.
func (b *benchmarkEvaluatorAdapter) Evaluate(ruleJSON []byte, exec scan.ExecResult) (scan.EvalResult, error) {
	if len(ruleJSON) == 0 {
		return scan.EvalResult{}, fmt.Errorf("scanexec: evaluation rule JSON is empty")
	}
	node, err := benchmark.ParseEvalRule(json.RawMessage(ruleJSON))
	if err != nil {
		return scan.EvalResult{}, fmt.Errorf("scanexec: parse eval rule: %w", err)
	}
	res, err := node.Eval(benchmark.EvalInput{
		Stdout:   string(exec.Stdout),
		Stderr:   string(exec.Stderr),
		ExitCode: exec.ExitCode,
	})
	if err != nil {
		return scan.EvalResult{}, fmt.Errorf("scanexec: eval: %w", err)
	}
	return scan.EvalResult{
		Outcome: mapEvalStatus(res.Status),
		Reason:  res.Reason,
	}, nil
}

func mapEvalStatus(s benchmark.EvalStatus) scan.Outcome {
	switch s {
	case benchmark.StatusPass:
		return scan.OutcomePass
	case benchmark.StatusFail:
		return scan.OutcomeFail
	case benchmark.StatusIndeterminate:
		return scan.OutcomeIndeterminate
	default:
		// unknown status — error로 분류 (호출자가 OutcomeError로 다시 매핑할 수도).
		return scan.OutcomeError
	}
}
