// Package sshpool은 원격 SSH 명령 실행과 연결 풀을 제공합니다 (E6).
//
// Phase 1 Stage A — Executor 표면(단일 명령 dial→exec→close, 풀 없음).
// Stage B에서 Pool(per-host/key/tenant limits + idle eviction + dial backoff) 추가.
// 외부 의존: `golang.org/x/crypto/ssh` (이미 E3 argon2 의존성으로 존재).
//
// 결정 (R4-1~R4-7 — `e6-ssh-scan-deepdive.md` §10):
//
//   - 자격증명: Executor는 `ssh.AuthMethod`만 받음(robot 도메인 무지). 호출자(robot.SSHTester 어댑터)가
//     CredentialMaterial → AuthMethod 변환 책임 (R4-1).
//   - known_hosts: Stage A는 호출자가 HostKeyCallback 결정. Stage B에서 first-touch trust 추가 (R4-2).
//   - argv quoting: 팩이 `["bash", "-c", "..."]` 형식 책임 (R4-3). Executor는 POSIX single-quote
//     escape으로 단일 string 직렬화 — shell metachar 전체 차단.
//   - cancel: ctx 만료/cancel 시 session.Close 후 결과 + ctx.Err() 반환. 원격 프로세스
//     강제 중단은 SSH 프로토콜 레벨에서 보장 안 됨 (R4-5 — timeout 신뢰).
package sshpool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// 기본값.
const (
	DefaultDialTimeout    = 10 * time.Second
	DefaultExecTimeout    = 30 * time.Second
	DefaultMaxStdoutBytes = 10 << 20 // 10 MiB (§06.8)
	DefaultMaxStderrBytes = 10 << 20
)

// SudoMode는 sudo wrap 정책입니다 (scanrun SSH 통합 Stage 3 — D-SCAN-3).
//
// SudoNone:           sudo 없이 argv 직접 실행 (기본값, 기존 동작 호환).
// SudoNonInteractive: argv 앞에 ["sudo", "-n", "--"] prefix wrap. -n은 password prompt
//
//	발생 시 즉시 fail — passwordless sudo 운영자 책임.
//	password 메모리 보존 회피(보안). enterprise customer 대부분
//	ansible/ssh key 기반 passwordless sudo 정책 보유.
type SudoMode int

const (
	SudoNone SudoMode = iota
	SudoNonInteractive
)

// Target은 SSH exec 대상 호스트와 인증 정보입니다.
//
// Auth와 HostKeyCallback은 ssh 패키지 타입을 그대로 노출 — robot 도메인에 무지(P5).
// 호출자가 robot.CredentialMaterial → ssh.AuthMethod 변환을 담당.
//
// SudoMode는 zero-value(SudoNone)면 기존 동작 — 회귀 위험 0.
type Target struct {
	Host            string
	Port            int
	Username        string
	Auth            ssh.AuthMethod
	HostKeyCallback ssh.HostKeyCallback
	SudoMode        SudoMode // zero-value = SudoNone (기존 동작)
}

// ExecResult는 Exec 결과입니다.
//
// Stdout·Stderr는 MaxStdoutBytes/MaxStderrBytes 이상이면 잘립니다 (§06.8).
// ExitCode는 ssh.ExitError가 채워진 경우만 의미 — 그 외(연결 실패·timeout)는 0.
type ExecResult struct {
	Stdout, Stderr []byte
	ExitCode       int
	Duration       time.Duration
	Truncated      bool // stdout 또는 stderr이 max 초과해 잘렸는지
}

// Executor는 원격 SSH 명령 실행 표면입니다.
type Executor interface {
	// Exec는 target에 dial하여 argv를 실행하고 결과를 반환합니다.
	//
	// timeout=0이면 DefaultExecTimeout. timeout 초과 또는 ctx cancel 시
	// session.Close 후 (부분 결과, ctx.Err) 반환 — 원격 프로세스 강제 중단은 X (R4-5).
	// argv는 POSIX single-quote escape으로 단일 string 직렬화 후 SSH exec 채널에 전송 — shell metachar 안전.
	Exec(ctx context.Context, target Target, argv []string, timeout time.Duration) (ExecResult, error)
}

// Deps는 Executor 의존성입니다.
type Deps struct {
	Logger *slog.Logger // nil이면 slog.Default() 사용

	DialTimeout    time.Duration // 0 → DefaultDialTimeout
	MaxStdoutBytes int           // 0 → DefaultMaxStdoutBytes
	MaxStderrBytes int           // 0 → DefaultMaxStderrBytes
}

// 공통 에러.
var (
	ErrEmptyArgv      = errors.New("sshpool: argv is empty")
	ErrInvalidPort    = errors.New("sshpool: Port must be 1..65535")
	ErrEmptyHost      = errors.New("sshpool: Host is empty")
	ErrEmptyUser      = errors.New("sshpool: Username is empty")
	ErrAuthRequired   = errors.New("sshpool: Auth is required")
	ErrHostKeyMissing = errors.New("sshpool: HostKeyCallback is required")
)

type executor struct {
	deps Deps
}

// New는 새 Executor를 반환합니다 (기본값 자동 적용).
func New(deps Deps) Executor {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.DialTimeout == 0 {
		deps.DialTimeout = DefaultDialTimeout
	}
	if deps.MaxStdoutBytes == 0 {
		deps.MaxStdoutBytes = DefaultMaxStdoutBytes
	}
	if deps.MaxStderrBytes == 0 {
		deps.MaxStderrBytes = DefaultMaxStderrBytes
	}
	return &executor{deps: deps}
}

// Exec는 Executor.Exec 구현입니다.
//
// 절차:
//  1. Target 검증 + argv 검증.
//  2. ctx에 DialTimeout 적용해 TCP dial.
//  3. SSH handshake.
//  4. session.NewSession() → 1회용 (R4-* — 매 명령마다 새 session).
//  5. argv를 POSIX single-quote escape로 단일 string 직렬화.
//  6. session.Run을 goroutine으로 분리, ctx/timeout select.
//  7. cancel/timeout 시 session+client close + 부분 결과 반환.
func (e *executor) Exec(ctx context.Context, target Target, argv []string, timeout time.Duration) (ExecResult, error) {
	if err := validateTarget(target); err != nil {
		return ExecResult{}, err
	}
	if len(argv) == 0 {
		return ExecResult{}, ErrEmptyArgv
	}
	if timeout == 0 {
		timeout = DefaultExecTimeout
	}

	config := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{target.Auth},
		HostKeyCallback: target.HostKeyCallback,
		Timeout:         e.deps.DialTimeout,
	}

	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	dialer := &net.Dialer{Timeout: e.deps.DialTimeout}
	netConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return ExecResult{}, fmt.Errorf("sshpool: dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, config)
	if err != nil {
		_ = netConn.Close()
		return ExecResult{}, fmt.Errorf("sshpool: handshake: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return ExecResult{}, fmt.Errorf("sshpool: NewSession: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// D-SCAN-3 — SudoNonInteractive 시 ["sudo", "-n", "--"] prefix wrap.
	// `-n`은 password prompt 발생 시 즉시 fail (stdin·tty 미사용).
	// `--`는 argv 시작 보호 (옵션 파싱 종료 마커).
	finalArgv := argv
	if target.SudoMode == SudoNonInteractive {
		finalArgv = append([]string{"sudo", "-n", "--"}, argv...)
	}
	cmd := JoinArgv(finalArgv)

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case <-timeoutCtx.Done():
		_ = session.Close()
		_ = client.Close()
		// goroutine 누수 방지: session.Close 이후 done 채널 비움.
		<-done
		return e.assemble(stdout.Bytes(), stderr.Bytes(), 0, time.Since(start)), timeoutCtx.Err()
	case runErr := <-done:
		dur := time.Since(start)
		exit := 0
		var execErr *ssh.ExitError
		var missingErr *ssh.ExitMissingError
		switch {
		case runErr == nil:
			// success
		case errors.As(runErr, &execErr):
			exit = execErr.ExitStatus()
		case errors.As(runErr, &missingErr):
			// exit-status request 못 받음 — 명령은 끝났으나 status unclear. 0으로 간주 + 로그.
			e.deps.Logger.Warn("sshpool: exit-status missing", "host", target.Host, "argv", argv)
		default:
			return e.assemble(stdout.Bytes(), stderr.Bytes(), 0, dur), fmt.Errorf("sshpool: run: %w", runErr)
		}
		return e.assemble(stdout.Bytes(), stderr.Bytes(), exit, dur), nil
	}
}

func (e *executor) assemble(stdout, stderr []byte, exit int, dur time.Duration) ExecResult {
	res := ExecResult{ExitCode: exit, Duration: dur}
	res.Stdout, res.Truncated = trim(stdout, e.deps.MaxStdoutBytes)
	if stderrTrimmed, t2 := trim(stderr, e.deps.MaxStderrBytes); true {
		res.Stderr = stderrTrimmed
		res.Truncated = res.Truncated || t2
	}
	return res
}

func trim(b []byte, max int) ([]byte, bool) {
	if len(b) <= max {
		return b, false
	}
	return b[:max], true
}

func validateTarget(t Target) error {
	if t.Host == "" {
		return ErrEmptyHost
	}
	if t.Port < 1 || t.Port > 65535 {
		return ErrInvalidPort
	}
	if t.Username == "" {
		return ErrEmptyUser
	}
	if t.Auth == nil {
		return ErrAuthRequired
	}
	if t.HostKeyCallback == nil {
		return ErrHostKeyMissing
	}
	return nil
}

// JoinArgv는 argv를 POSIX single-quote escape으로 단일 string 직렬화합니다 (R4-3).
//
// 결과 예: ["bash", "-c", "ls /etc"] → `'bash' '-c' 'ls /etc'`.
// single-quote 자체는 `'\”` (POSIX 표준 escape pattern)로 처리.
//
// 호출자(팩 정의)는 argv를 의도된 명령으로 정확히 구성 — Executor는 shell metachar
// 해석 없이 그대로 전달. 즉 `["echo", "$HOME"]`은 원격 shell에서 `$HOME` 확장 X.
func JoinArgv(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = singleQuote(a)
	}
	return strings.Join(out, " ")
}

func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
