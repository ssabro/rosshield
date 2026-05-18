//go:build rosshield_enterprise

// limits.go — Evaluate 호출당 자원 한도 정의 (C-1 sandboxed evaluator).
//
// 본 파일은 Limits 구조와 기본 값을 정의합니다. 실제 한도 적용은:
//   - CPUTimeout : context.WithTimeout 으로 Runtime.Evaluate 안 강제.
//   - MemoryPages: wazero.RuntimeConfig.WithMemoryLimitPages — Runtime 생성 시 적용.
//   - StdoutBytes: cappedWriter (runtime.go) — fd_write 누적 바이트가 한도 초과 시 truncate
//                  + ErrStdoutTruncated 반환.
//
// 한도는 spec-C(잠재 청구권) 권장 default 기준:
//   - CPUTimeout 5초: 결정론 check 한 건은 보통 ms 수준, 5초면 충분 + 악성 loop 차단.
//   - MemoryPages 1024 (= 64MB): jsonpath/string ops 여유 + 64MB 초과 시 OOM trap.
//   - StdoutBytes 64KB: pass/fail JSON + evidence base64 압축 가정 충분.
//
// 참조:
//   - docs/design/notes/phase7-public-transition-design.md §6.4 C-1
//   - docs/design/13-patent-strategy.md §13.3 D8-3 wazero 결정

package wasmrt

import "time"

// Limits는 단일 Evaluate 호출에 강제할 자원 한도입니다.
//
// 모든 필드 0 값은 DefaultLimits에서 정의한 기본값으로 대체됩니다 (resolveLimits).
// 이는 호출자가 부분 override(예: CPUTimeout만 지정)를 안전하게 할 수 있도록 합니다.
type Limits struct {
	// CPUTimeout 은 단일 Evaluate 호출이 소비할 수 있는 wall-clock 최대 시간입니다.
	// 0 이면 DefaultCPUTimeout(5초) 사용.
	CPUTimeout time.Duration

	// MemoryPages 는 WASM 모듈이 보유할 수 있는 최대 메모리 페이지 수(1 page = 64KB)입니다.
	// 0 이면 DefaultMemoryPages(1024 = 64MB) 사용.
	MemoryPages uint32

	// StdoutBytes 는 WASM 모듈이 stdout(fd 1) 에 누적 쓸 수 있는 최대 바이트 수입니다.
	// 한도 초과 시 cappedWriter가 ErrStdoutTruncated 를 발생시켜 호출자에게 알립니다.
	// 0 이면 DefaultStdoutBytes(64KB) 사용.
	StdoutBytes int64
}

// 기본 한도 — Limits 필드가 0 일 때 채워 넣는 값입니다.
const (
	// DefaultCPUTimeout 은 결정론 check 한 건의 통상 ms 수준 대비 충분히 큰 wall-clock
	// 한도이며, 동시에 무한 loop 류의 악성 정책을 빠르게 차단할 수 있는 균형 값입니다.
	DefaultCPUTimeout = 5 * time.Second

	// DefaultMemoryPages 는 64MB (1024 페이지 × 64KB) 입니다.
	// jsonpath/string 처리 여유 + 통상 check 메모리 사용 (수십~수 MB) 충분.
	DefaultMemoryPages uint32 = 1024

	// DefaultStdoutBytes 는 64KB 입니다. pass/fail JSON + base64 압축된 evidence 충분.
	DefaultStdoutBytes int64 = 64 * 1024
)

// resolveLimits 는 입력 Limits 의 0 필드를 DefaultLimits 값으로 채워 반환합니다.
//
// 본 함수는 입력을 mutate 하지 않습니다 (원칙 §11 불변성). 호출자는 항상 새 사본을 받습니다.
//
// 의도:
//   - 호출자가 Limits{CPUTimeout: 2*time.Second}만 지정해도 나머지(memory/stdout)는 default.
//   - Runtime 본체는 0 값 처리 분기를 갖지 않도록 한도 정규화를 단일 지점에 집중.
func resolveLimits(in Limits) Limits {
	out := in
	if out.CPUTimeout <= 0 {
		out.CPUTimeout = DefaultCPUTimeout
	}
	if out.MemoryPages == 0 {
		out.MemoryPages = DefaultMemoryPages
	}
	if out.StdoutBytes <= 0 {
		out.StdoutBytes = DefaultStdoutBytes
	}
	return out
}
