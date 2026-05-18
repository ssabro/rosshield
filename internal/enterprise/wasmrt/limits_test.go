//go:build rosshield_enterprise

package wasmrt

import (
	"testing"
	"time"
)

func TestResolveLimits_zero_입력_default_채움(t *testing.T) {
	got := resolveLimits(Limits{})
	if got.CPUTimeout != DefaultCPUTimeout {
		t.Errorf("CPUTimeout: got %v, want %v", got.CPUTimeout, DefaultCPUTimeout)
	}
	if got.MemoryPages != DefaultMemoryPages {
		t.Errorf("MemoryPages: got %d, want %d", got.MemoryPages, DefaultMemoryPages)
	}
	if got.StdoutBytes != DefaultStdoutBytes {
		t.Errorf("StdoutBytes: got %d, want %d", got.StdoutBytes, DefaultStdoutBytes)
	}
}

func TestResolveLimits_부분_override_보존(t *testing.T) {
	in := Limits{CPUTimeout: 2 * time.Second}
	got := resolveLimits(in)
	if got.CPUTimeout != 2*time.Second {
		t.Errorf("CPUTimeout override 손실: got %v", got.CPUTimeout)
	}
	if got.MemoryPages != DefaultMemoryPages {
		t.Errorf("MemoryPages default 누락: got %d", got.MemoryPages)
	}
	if got.StdoutBytes != DefaultStdoutBytes {
		t.Errorf("StdoutBytes default 누락: got %d", got.StdoutBytes)
	}
}

func TestResolveLimits_모두_override_보존(t *testing.T) {
	in := Limits{
		CPUTimeout:  100 * time.Millisecond,
		MemoryPages: 32,
		StdoutBytes: 1024,
	}
	got := resolveLimits(in)
	if got != in {
		t.Errorf("override 손실: got %+v, want %+v", got, in)
	}
}

func TestResolveLimits_negative_CPUTimeout_default로_치환(t *testing.T) {
	got := resolveLimits(Limits{CPUTimeout: -1 * time.Second})
	if got.CPUTimeout != DefaultCPUTimeout {
		t.Errorf("negative CPUTimeout이 default로 치환되지 않음: got %v", got.CPUTimeout)
	}
}

func TestResolveLimits_negative_StdoutBytes_default로_치환(t *testing.T) {
	got := resolveLimits(Limits{StdoutBytes: -1})
	if got.StdoutBytes != DefaultStdoutBytes {
		t.Errorf("negative StdoutBytes가 default로 치환되지 않음: got %d", got.StdoutBytes)
	}
}

func TestResolveLimits_입력_mutation_금지(t *testing.T) {
	in := Limits{}
	_ = resolveLimits(in)
	if in.CPUTimeout != 0 || in.MemoryPages != 0 || in.StdoutBytes != 0 {
		t.Errorf("입력 mutation 발생: %+v", in)
	}
}

func TestDefaultLimits_상수_sanity(t *testing.T) {
	if DefaultCPUTimeout <= 0 {
		t.Error("DefaultCPUTimeout 양수여야 함")
	}
	if DefaultMemoryPages == 0 {
		t.Error("DefaultMemoryPages 비0이어야 함")
	}
	if DefaultStdoutBytes <= 0 {
		t.Error("DefaultStdoutBytes 양수여야 함")
	}
	// 64MB = 1024 페이지 × 64KB 검증.
	if int64(DefaultMemoryPages)*64*1024 != 64*1024*1024 {
		t.Errorf("DefaultMemoryPages가 64MB와 일치하지 않음: %d 페이지", DefaultMemoryPages)
	}
}
