//go:build rosshield_enterprise

package wasmrt

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// newTestRuntime 는 테스트당 1 회 Runtime 을 생성하고 cleanup 을 등록합니다.
//
// 모든 테스트는 본 helper 를 통해 Runtime 을 얻습니다 — 메모리 한도는 default 1024p.
func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt, err := NewRuntime(context.Background(), 0) // 0 = default
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(func() {
		_ = rt.Close(context.Background())
	})
	return rt
}

// --- compile / 입력 검증 경로 -------------------------------------------------

func TestEvaluate_invalid_policy_빈_입력_거부(t *testing.T) {
	rt := newTestRuntime(t)
	_, err := rt.Evaluate(context.Background(), nil, nil, Limits{})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("빈 정책: got %v, want ErrInvalidPolicy", err)
	}
}

func TestEvaluate_invalid_policy_나쁜_magic_거부(t *testing.T) {
	rt := newTestRuntime(t)
	bad := []byte{0xff, 0xff, 0xff, 0xff, 0x01, 0x00, 0x00, 0x00}
	_, err := rt.Evaluate(context.Background(), bad, nil, Limits{})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("나쁜 magic: got %v, want ErrInvalidPolicy", err)
	}
}

func TestEvaluate_invalid_policy_magic만_있고_section_손상(t *testing.T) {
	rt := newTestRuntime(t)
	broken := append([]byte{}, wasmMagic...)
	broken = append(broken, 0xff, 0xff, 0xff) // 알 수 없는 section id + 잘못된 길이
	_, err := rt.Evaluate(context.Background(), broken, nil, Limits{})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("손상된 section: got %v, want ErrInvalidPolicy", err)
	}
}

// --- happy path: pass JSON 출력 -----------------------------------------------

func TestEvaluate_pass_정상_JSON_출력(t *testing.T) {
	rt := newTestRuntime(t)
	res, err := rt.Evaluate(context.Background(), wasmFdWriteJSON, nil, Limits{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Status != StatusPass {
		t.Errorf("Status: got %q, want %q", res.Status, StatusPass)
	}
}

func TestEvaluate_pass_결정론_같은_입력_같은_결과(t *testing.T) {
	rt := newTestRuntime(t)
	res1, err1 := rt.Evaluate(context.Background(), wasmFdWriteJSON, []byte("input"), Limits{})
	if err1 != nil {
		t.Fatalf("Evaluate 1: %v", err1)
	}
	res2, err2 := rt.Evaluate(context.Background(), wasmFdWriteJSON, []byte("input"), Limits{})
	if err2 != nil {
		t.Fatalf("Evaluate 2: %v", err2)
	}
	if res1.Status != res2.Status {
		t.Errorf("결정론 위반 Status: %q vs %q", res1.Status, res2.Status)
	}
	if string(res1.Evidence) != string(res2.Evidence) {
		t.Errorf("결정론 위반 Evidence: %q vs %q", res1.Evidence, res2.Evidence)
	}
	if res1.Reasoning != res2.Reasoning {
		t.Errorf("결정론 위반 Reasoning: %q vs %q", res1.Reasoning, res2.Reasoning)
	}
}

// --- CPU timeout --------------------------------------------------------------

func TestEvaluate_CPU_timeout_무한loop_차단(t *testing.T) {
	rt := newTestRuntime(t)
	start := time.Now()
	_, err := rt.Evaluate(context.Background(), wasmInfiniteLoop, nil, Limits{
		CPUTimeout: 200 * time.Millisecond,
	})
	elapsed := time.Since(start)
	if !errors.Is(err, ErrCPUTimeout) {
		t.Errorf("무한 loop: got %v, want ErrCPUTimeout", err)
	}
	// 한도 + wazero 정리 마진 — 2초 안에는 무조건 끝나야 함.
	if elapsed > 2*time.Second {
		t.Errorf("CPU timeout 효과 부족: %v 경과", elapsed)
	}
}

// --- stdout truncate ---------------------------------------------------------

func TestEvaluate_stdout_truncated_한도_초과(t *testing.T) {
	rt := newTestRuntime(t)
	_, err := rt.Evaluate(context.Background(), wasmFdWriteLarge, nil, Limits{
		StdoutBytes: 1024, // 1KB 한도 — 64KB 시도가 잘림.
	})
	if !errors.Is(err, ErrStdoutTruncated) {
		t.Errorf("large stdout: got %v, want ErrStdoutTruncated", err)
	}
}

// --- invalid output (non-JSON) ----------------------------------------------

func TestEvaluate_invalid_output_비JSON_거부(t *testing.T) {
	rt := newTestRuntime(t)
	_, err := rt.Evaluate(context.Background(), wasmFdWriteInvalidJSON, nil, Limits{})
	if !errors.Is(err, ErrInvalidOutput) {
		t.Errorf("비-JSON 출력: got %v, want ErrInvalidOutput", err)
	}
}

func TestEvaluate_invalid_output_빈_stdout_거부(t *testing.T) {
	rt := newTestRuntime(t)
	// wasmMinimalEmpty 는 _start 가 없어 stdout 출력 0 → ErrInvalidOutput.
	_, err := rt.Evaluate(context.Background(), wasmMinimalEmpty, nil, Limits{})
	if !errors.Is(err, ErrInvalidOutput) {
		t.Errorf("빈 stdout: got %v, want ErrInvalidOutput", err)
	}
}

// --- verifier 통합 ------------------------------------------------------------

func TestEvaluateWithVerifier_nop_통과(t *testing.T) {
	rt := newTestRuntime(t)
	res, err := rt.EvaluateWithVerifier(context.Background(), wasmFdWriteJSON, []byte("sig"), nil, Limits{}, NopVerifier{})
	if err != nil {
		t.Fatalf("NopVerifier 통과 실패: %v", err)
	}
	if res.Status != StatusPass {
		t.Errorf("Status: %q", res.Status)
	}
}

func TestEvaluateWithVerifier_err_거부(t *testing.T) {
	rt := newTestRuntime(t)
	_, err := rt.EvaluateWithVerifier(
		context.Background(),
		wasmFdWriteJSON,
		[]byte("sig"),
		nil,
		Limits{},
		errVerifier{},
	)
	if !errors.Is(err, ErrPolicySignatureInvalid) {
		t.Errorf("errVerifier: got %v, want ErrPolicySignatureInvalid", err)
	}
}

func TestEvaluateWithVerifier_nil_verifier는_nop_대체(t *testing.T) {
	rt := newTestRuntime(t)
	res, err := rt.EvaluateWithVerifier(context.Background(), wasmFdWriteJSON, nil, nil, Limits{}, nil)
	if err != nil {
		t.Fatalf("nil verifier: %v", err)
	}
	if res.Status != StatusPass {
		t.Errorf("Status: %q", res.Status)
	}
}

// --- Runtime 수명 -----------------------------------------------------------

func TestRuntime_Close_idempotent(t *testing.T) {
	rt, err := NewRuntime(context.Background(), 0)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Errorf("1차 Close: %v", err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Errorf("2차 Close (idempotent): %v", err)
	}
}

func TestRuntime_Close후_Evaluate는_ErrRuntimeClosed(t *testing.T) {
	rt, err := NewRuntime(context.Background(), 0)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = rt.Evaluate(context.Background(), wasmFdWriteJSON, nil, Limits{})
	if !errors.Is(err, ErrRuntimeClosed) {
		t.Errorf("close 후 Evaluate: got %v, want ErrRuntimeClosed", err)
	}
}

// --- 결과 직렬화 sanity -------------------------------------------------------

func TestResult_status_검증(t *testing.T) {
	tests := []struct {
		s    Status
		want bool
	}{
		{StatusPass, true},
		{StatusFail, true},
		{StatusError, true},
		{Status("unknown"), false},
		{Status(""), false},
	}
	for _, tc := range tests {
		if got := tc.s.IsValid(); got != tc.want {
			t.Errorf("%q.IsValid() = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestResult_unmarshal_pass(t *testing.T) {
	var res Result
	if err := json.Unmarshal([]byte(`{"status":"pass"}`), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Status != StatusPass {
		t.Errorf("Status: %q", res.Status)
	}
}

func TestResult_unmarshal_evidence_reasoning_보존(t *testing.T) {
	raw := []byte(`{"status":"fail","evidence":{"count":3},"reasoning":"3 ports open"}`)
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Status != StatusFail {
		t.Errorf("Status: %q", res.Status)
	}
	if string(res.Evidence) != `{"count":3}` {
		t.Errorf("Evidence: %q", res.Evidence)
	}
	if res.Reasoning != "3 ports open" {
		t.Errorf("Reasoning: %q", res.Reasoning)
	}
}

// --- cappedWriter 단위 -------------------------------------------------------

func TestCappedWriter_한도_안에서_정상_누적(t *testing.T) {
	w := newCappedWriter(100)
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Errorf("n: got %d, want 5", n)
	}
	if w.Truncated() {
		t.Error("Truncated 거짓 true")
	}
	if string(w.Bytes()) != "hello" {
		t.Errorf("Bytes: %q", w.Bytes())
	}
}

func TestCappedWriter_한도_초과_시_truncate_set(t *testing.T) {
	w := newCappedWriter(3)
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Errorf("n (호출자 인식용): got %d, want 5", n)
	}
	if !w.Truncated() {
		t.Error("Truncated 가 set 되지 않음")
	}
	if string(w.Bytes()) != "hel" {
		t.Errorf("Bytes: %q, want %q", w.Bytes(), "hel")
	}
}

func TestCappedWriter_한도_도달_후_추가_write_drop(t *testing.T) {
	w := newCappedWriter(3)
	_, _ = w.Write([]byte("abc"))
	if w.Truncated() {
		// 정확히 3바이트는 truncate 아님 (한도 == 길이).
		t.Error("3바이트 == 한도인데 Truncated true")
	}
	_, _ = w.Write([]byte("d"))
	if !w.Truncated() {
		t.Error("4번째 바이트가 한도 초과인데 Truncated 가 false")
	}
	if string(w.Bytes()) != "abc" {
		t.Errorf("Bytes: %q", w.Bytes())
	}
}

func TestCappedWriter_연속_write_누적(t *testing.T) {
	w := newCappedWriter(100)
	_, _ = w.Write([]byte("foo"))
	_, _ = w.Write([]byte("bar"))
	if string(w.Bytes()) != "foobar" {
		t.Errorf("Bytes: %q", w.Bytes())
	}
}

// --- zeroReader / zeroWalltime sanity ---------------------------------------

func TestZeroReader_언제나_0(t *testing.T) {
	r := zeroReader{}
	buf := make([]byte, 16)
	for i := range buf {
		buf[i] = 0xff
	}
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 16 {
		t.Errorf("n: %d", n)
	}
	for i, b := range buf {
		if b != 0 {
			t.Errorf("buf[%d] = %#x, want 0", i, b)
		}
	}
}

func TestZeroWalltime_0_0(t *testing.T) {
	sec, nsec := zeroWalltime()
	if sec != 0 || nsec != 0 {
		t.Errorf("walltime: (%d, %d)", sec, nsec)
	}
}

func TestZeroNanotime_0(t *testing.T) {
	if got := zeroNanotime(); got != 0 {
		t.Errorf("nanotime: %d", got)
	}
}

// --- containsAny sanity ------------------------------------------------------

func TestContainsAny_일치(t *testing.T) {
	if !containsAny("out of bounds memory access", []string{"out of bounds", "other"}) {
		t.Error("일치 검출 실패")
	}
}

func TestContainsAny_불일치(t *testing.T) {
	if containsAny("nothing here", []string{"absent", "other"}) {
		t.Error("불일치인데 true")
	}
}

// --- isMemoryError sanity ----------------------------------------------------

func TestIsMemoryError_nil_false(t *testing.T) {
	if isMemoryError(nil) {
		t.Error("nil이 true")
	}
}

func TestIsMemoryError_관련_메시지_true(t *testing.T) {
	if !isMemoryError(errors.New("wasm trap: out of bounds memory access")) {
		t.Error("관련 메시지가 false")
	}
}

func TestIsMemoryError_무관_메시지_false(t *testing.T) {
	if isMemoryError(errors.New("some other trap")) {
		t.Error("무관 메시지가 true")
	}
}
