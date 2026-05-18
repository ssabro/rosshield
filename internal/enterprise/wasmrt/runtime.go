//go:build rosshield_enterprise

// runtime.go — wazero 기반 WASM 정책 sandboxed evaluator (C-1 본체).
//
// 본 패키지는 다음 모델을 구현합니다 (design doc §6.4):
//
//  1. 호출자가 WASM 바이트(.wasm) 정책 + 입력 바이트 + Limits 를 제공.
//  2. Runtime 은 wazero 인스턴스에서 다음 격리를 강제하며 _start 함수를 실행:
//       · filesystem : WASI FS config 미설정 → 호스트 파일 접근 0 (path_open 시 ENOSYS)
//       · network    : WASI sock_* 미설정 → 소켓 호출 0
//       · CPU time   : context.WithTimeout(Limits.CPUTimeout) → 만료 시 모듈 강제 종료
//       · memory     : wazero RuntimeConfig.WithMemoryLimitPages → 초과 시 trap
//       · stdin      : 호출자가 제공한 input 바이트가 fd 0 으로 노출
//       · stdout     : cappedWriter (한도 = Limits.StdoutBytes) — 한도 초과 시 truncate
//       · stderr     : io.Discard (결정론 — 외부 로그 누출 방지)
//       · env        : 빈 set (호스트 환경 0)
//       · rand       : 결정론 zero-byte reader (외부 entropy 0 — 결정론 + 정책 결과 재현 가능)
//       · clocks     : zero walltime + zero nanotime (결정론)
//
//  3. 모듈 종료 후 stdout 바이트를 JSON Result 로 파싱하여 반환.
//
// 결정론 보장:
//   - 같은 (policy bytes, input bytes, Limits) → 같은 Result 가 산출되어야 합니다.
//   - 이를 위해 randSource = 0, walltime = 0, nanotime = 0, environ = ∅.
//   - WASI clock_time_get 은 zero walltime/nanotime 를 사용 (sandboxing).
//
// 호출 모델:
//
//	rt, _ := NewRuntime(ctx)
//	defer rt.Close(ctx)
//	res, err := rt.Evaluate(ctx, policy, input, Limits{})
//
// 본 round 는 코어 audit/scan 도메인과 통합되지 않습니다. R-D8 4.5 통합 e2e 에서
// check evaluator 어댑터 경유로 연결됩니다.

package wasmrt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// Status 는 정책 평가 결과의 한 가지 분류입니다.
type Status string

// 허용된 Status 값입니다. 정책 JSON 의 "status" 필드는 정확히 이 셋 중 하나여야 합니다.
const (
	StatusPass  Status = "pass"
	StatusFail  Status = "fail"
	StatusError Status = "error"
)

// IsValid 는 Status 가 알려진 값인지 확인합니다.
func (s Status) IsValid() bool {
	switch s {
	case StatusPass, StatusFail, StatusError:
		return true
	default:
		return false
	}
}

// Result 는 WASM 정책의 평가 결과입니다. JSON 으로 stdout 에 직렬화된 형태로
// 호스트에 반환됩니다.
//
// 필드:
//   - Status    : pass / fail / error 중 하나 (필수).
//   - Evidence  : 자유 형식 JSON (선택). 본 패키지는 내용을 해석하지 않고 그대로 전달.
//   - Reasoning : 자연어 설명 (선택). 감사 리포트에 표시.
type Result struct {
	Status    Status          `json:"status"`
	Evidence  json.RawMessage `json:"evidence,omitempty"`
	Reasoning string          `json:"reasoning,omitempty"`
}

// Runtime 은 wazero 인스턴스를 wrap 한 정책 평가기입니다.
//
// 하나의 Runtime 인스턴스는 여러 Evaluate 호출에 재사용 가능합니다 (스레드-safe).
// Close 후 Evaluate 호출은 ErrRuntimeClosed 를 반환합니다.
//
// memory limit 은 Runtime 생성 시점에 고정됩니다 (wazero 의 RuntimeConfig 가
// runtime-level 설정이므로). 따라서 Evaluate 의 Limits.MemoryPages 는 Runtime
// 생성 시 지정한 값보다 작아질 수만 있습니다 (큰 값을 요구하면 silently capped).
type Runtime struct {
	mu          sync.Mutex
	wazeroRT    wazero.Runtime
	memoryPages uint32 // Runtime 생성 시 고정된 max pages
	closed      bool
}

// NewRuntime 은 새 Runtime 을 생성합니다. ctx 는 wazero 의 compilation cache 초기화에
// 사용됩니다 (cancel 되어도 본 함수 자체는 영향받지 않음).
//
// memoryPages 인자는 향후 Evaluate 호출에서 사용할 최대 메모리 페이지 수입니다.
// 0 을 전달하면 DefaultMemoryPages(1024) 가 사용됩니다.
//
// 본 함수는 WASI 호스트 모듈을 한 번만 등록하여 이후 Evaluate 호출에서 재사용합니다.
func NewRuntime(ctx context.Context, memoryPages uint32) (*Runtime, error) {
	if memoryPages == 0 {
		memoryPages = DefaultMemoryPages
	}

	rtConfig := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(memoryPages).
		WithCloseOnContextDone(true) // ctx cancel → 모듈 강제 종료 (CPU timeout 매커니즘).

	wzRT := wazero.NewRuntimeWithConfig(ctx, rtConfig)

	// WASI snapshot_preview1 호스트 모듈 등록.
	// 본 모듈은 fd_read/fd_write/clock_time_get 등 표준 WASI 함수를 노출.
	// 단, 본 패키지가 FSConfig/SockConfig 를 설정하지 않으므로 파일·소켓 호출은 ENOSYS.
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, wzRT); err != nil {
		_ = wzRT.Close(ctx)
		return nil, fmt.Errorf("wasmrt: instantiate wasi: %w", err)
	}

	return &Runtime{
		wazeroRT:    wzRT,
		memoryPages: memoryPages,
	}, nil
}

// Close 는 Runtime 이 보유한 wazero 자원을 해제합니다. 본 함수 호출 후 모든
// Evaluate 호출은 ErrRuntimeClosed 를 반환합니다. 본 함수는 idempotent 합니다.
func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if err := r.wazeroRT.Close(ctx); err != nil {
		return fmt.Errorf("wasmrt: close runtime: %w", err)
	}
	return nil
}

// Evaluate 는 NopVerifier 로 정책을 평가합니다 (서명 검증 없음, 개발/테스트 경로).
//
// 실 운영에서는 EvaluateWithVerifier 를 사용해 cosign 어댑터를 주입해야 합니다.
func (r *Runtime) Evaluate(ctx context.Context, policy, input []byte, limits Limits) (Result, error) {
	return r.EvaluateWithVerifier(ctx, policy, nil, input, limits, NopVerifier{})
}

// EvaluateWithVerifier 는 정책 서명을 verifier 로 검증한 뒤 sandbox 안에서 평가를 수행합니다.
//
// 절차:
//  1. validatePolicyBytes : magic+version prefix 빠른 사전 거부.
//  2. verifier.Verify     : 서명 검증 (NopVerifier 이면 항상 통과).
//  3. limits 정규화        : resolveLimits — 0 값을 default 로 채움.
//  4. context 가공         : Limits.CPUTimeout 으로 timeout 컨텍스트 생성.
//  5. wazero CompileModule : WASM 디코딩 — 실패 시 ErrInvalidPolicy.
//  6. InstantiateModule    : ModuleConfig 로 sandbox 적용 + _start 호출.
//  7. stdout 파싱          : Result JSON 으로 unmarshal — 실패 시 ErrInvalidOutput.
func (r *Runtime) EvaluateWithVerifier(
	ctx context.Context,
	policy, sig, input []byte,
	limits Limits,
	verifier PolicyVerifier,
) (Result, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return Result{}, ErrRuntimeClosed
	}
	r.mu.Unlock()

	if verifier == nil {
		verifier = NopVerifier{}
	}

	if err := validatePolicyBytes(policy); err != nil {
		return Result{}, err
	}
	if err := verifier.Verify(policy, sig); err != nil {
		if errors.Is(err, ErrPolicySignatureInvalid) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("%w: %v", ErrPolicySignatureInvalid, err)
	}

	limits = resolveLimits(limits)

	// CPU timeout 컨텍스트 — wazero 가 ctx.Done 을 polling 하여 모듈을 강제 종료.
	evalCtx, cancel := context.WithTimeout(ctx, limits.CPUTimeout)
	defer cancel()

	// stdout cappedWriter — Limits.StdoutBytes 초과 시 truncate + sentinel 기록.
	stdout := newCappedWriter(limits.StdoutBytes)

	// stdin — 호출자가 제공한 input 바이트.
	stdin := bytes.NewReader(input)

	// ModuleConfig — sandbox 강제.
	//
	// 결정론 보장:
	//   - WithRandSource(zeroReader{}) : crypto 비결정성 0.
	//   - WithWalltime(0,...)          : 정책이 시간 의존 분기를 갖지 않도록.
	//   - WithNanotime(0,...)          : 위와 동일.
	//   - WithNanosleep(noop)          : 정책이 sleep 으로 timing 회피 시도 시 무효.
	//   - WithEnv 호출 0               : 빈 환경 변수.
	// 보안 보장:
	//   - WithFSConfig 호출 0          : 호스트 파일 시스템 접근 0 (path_open → ENOSYS).
	//   - WithArgs 호출 0              : argv 비공개.
	//   - sock_* 미연결                : WASI sock_* 시도 → ENOSYS.
	// ClockResolution 은 wazero 내부에서 uint32 로 strict 검증 (resolution <= 1 hour 의 ns, 단
	// uint32 truncate 후 비교). 안전한 default 는 1us = 1000 ns — 결정론 zero clock 와
	// 일관됩니다 (정책이 본 값을 직접 사용하지 않음).
	clockResNS := sys.ClockResolution(time.Microsecond.Nanoseconds())

	modConfig := wazero.NewModuleConfig().
		WithStdin(stdin).
		WithStdout(stdout).
		WithStderr(io.Discard).
		WithRandSource(zeroReader{}).
		WithWalltime(zeroWalltime, clockResNS).
		WithNanotime(zeroNanotime, clockResNS).
		WithNanosleep(noopNanosleep).
		WithName("") // anonymous — 같은 Runtime 안 여러 instantiate 허용.

	// CompileModule — 디코딩 단계 오류 분리.
	compiled, err := r.wazeroRT.CompileModule(evalCtx, policy)
	if err != nil {
		return Result{}, fmt.Errorf("%w: compile: %v", ErrInvalidPolicy, err)
	}
	defer func() { _ = compiled.Close(evalCtx) }()

	// InstantiateModule — _start 호출 포함. 정상 종료는 ExitError(0) 또는 nil.
	mod, err := r.wazeroRT.InstantiateModule(evalCtx, compiled, modConfig)
	if mod != nil {
		defer func() { _ = mod.Close(evalCtx) }()
	}
	if err != nil {
		// classify 1: context cancel/timeout → CPU timeout.
		if errors.Is(evalCtx.Err(), context.DeadlineExceeded) {
			return Result{}, fmt.Errorf("%w: %v", ErrCPUTimeout, err)
		}
		// classify 2: WASI ExitError code 0 = 정상 종료 (모듈이 명시적으로 _exit(0)).
		var exitErr *sys.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 0 {
				// 정상 종료 — stdout 파싱으로 진행.
				return parseResult(stdout)
			}
			return Result{}, fmt.Errorf("%w: wasm exit code %d", ErrInvalidPolicy, exitErr.ExitCode())
		}
		// classify 3: memory.grow trap 또는 OOM.
		if isMemoryError(err) {
			return Result{}, fmt.Errorf("%w: %v", ErrMemoryExceeded, err)
		}
		// classify 4: 그 외 trap → ErrInvalidPolicy (정책 자체 결함).
		return Result{}, fmt.Errorf("%w: instantiate: %v", ErrInvalidPolicy, err)
	}

	return parseResult(stdout)
}

// parseResult 는 cappedWriter 의 누적 바이트를 Result JSON 으로 파싱합니다.
//
// truncated 플래그가 set 이면 우선 ErrStdoutTruncated 반환 — JSON 파싱 결과와 무관하게
// 호출자에게 알려야 합니다 (Result 가 잘려 신뢰 불가).
//
// 빈 stdout 는 ErrInvalidOutput 입니다 — 정책이 결과를 쓰지 않은 경우.
func parseResult(stdout *cappedWriter) (Result, error) {
	if stdout.Truncated() {
		return Result{}, ErrStdoutTruncated
	}
	raw := stdout.Bytes()
	if len(bytes.TrimSpace(raw)) == 0 {
		return Result{}, fmt.Errorf("%w: empty stdout", ErrInvalidOutput)
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidOutput, err)
	}
	if !res.Status.IsValid() {
		return Result{}, fmt.Errorf("%w: invalid status %q", ErrInvalidOutput, res.Status)
	}
	return res, nil
}

// isMemoryError 는 wazero 가 반환하는 오류 메시지에서 memory.grow / out of bounds
// trap 을 식별합니다. wazero 의 sentinel 노출이 제한적이므로 문자열 분류를 보조로 사용.
func isMemoryError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, []string{
		"out of bounds memory access",
		"memory size exceeds",
		"out of memory",
	})
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if bytes.Contains([]byte(s), []byte(sub)) {
			return true
		}
	}
	return false
}

// cappedWriter 는 누적 바이트 한도를 강제하는 io.Writer 입니다.
//
// 한도 초과 분 은 silently drop 되며 Truncated() 가 true 가 됩니다. WASM 모듈은
// fd_write 호출이 부분 성공한 것으로 인식할 수 있으나, 본 패키지는 Truncated() 가
// set 되면 결과 파싱 단계에서 ErrStdoutTruncated 를 반환합니다.
type cappedWriter struct {
	limit     int64
	buf       bytes.Buffer
	written   int64
	truncated bool
}

func newCappedWriter(limit int64) *cappedWriter {
	return &cappedWriter{limit: limit}
}

// Write 는 한도 안에서만 byte 를 누적합니다.
//
// 반환은 항상 (len(p), nil) — WASM 모듈이 단축 write 로 인식하면 오류 처리 분기를
// 타고 들어가 결정론을 깨뜨릴 수 있어 호스트 측에서 흡수.
func (w *cappedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.written
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		w.buf.Write(p[:remaining])
		w.written += remaining
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	w.written += int64(len(p))
	return len(p), nil
}

// Bytes 는 누적된 바이트의 사본을 반환합니다 (한도까지만 — 한도 초과분은 drop).
func (w *cappedWriter) Bytes() []byte {
	return w.buf.Bytes()
}

// Truncated 는 한도 초과로 일부 데이터가 drop 됐는지 보고합니다.
func (w *cappedWriter) Truncated() bool {
	return w.truncated
}

// zeroReader 는 항상 0 바이트를 반환하는 io.Reader 입니다 (결정론 rand 소스).
//
// crypto/rand.Reader 대신 본 reader 를 사용하면 정책 평가가 결정론적으로 재현
// 가능합니다 (외부 검증자가 같은 입력으로 같은 결과를 산출).
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// zeroWalltime 는 항상 (0, 0) 을 반환하는 sys.Walltime 입니다 (결정론 시간).
//
// WASI clock_time_get(realtime) 결과로 노출됩니다. 정책이 시간 기반 분기를 가지면
// 본 함수의 결정성 덕에 같은 입력으로 같은 결과가 산출됩니다.
func zeroWalltime() (sec int64, nsec int32) {
	return 0, 0
}

// zeroNanotime 는 항상 0 을 반환하는 sys.Nanotime 입니다 (결정론 monotonic).
func zeroNanotime() int64 {
	return 0
}

// noopNanosleep 는 즉시 반환하는 sys.Nanosleep 입니다 (sleep 효과 무효화).
//
// 정책이 sleep 으로 timing 회피를 시도해도 본 hook 이 즉시 반환하므로 CPU timeout
// 강제는 wall clock 기반(context.WithTimeout)으로 유지됩니다.
func noopNanosleep(int64) {}
