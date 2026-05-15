package handlers

// scope_resolver_peek_test.go — RBAC fleet 정밀화 Stage 2 단위 테스트 (body peek helper).
//
// peekFleetIDFromBody 단위만 분리 — middleware 통합 테스트는 scope_resolver_test.go.
// 본 파일은 invariant 검증에 집중:
//   - body 복원 (NopCloser + bytes.Reader)
//   - 10KB 한계 (D-RBACEX-9 + design doc §3.1.1)
//   - nil body / 빈 body fallback
//   - 재사용 가능성 (io.ReadAll + Close)

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBodyPeekHelperReturnsBodyExactly — peek helper가 body를 그대로 보존하는 단위.
//
// 본 helper는 별도 테스트로 분리 — middleware 통합 테스트는 통신 수준 검증.
func TestBodyPeekHelperReturnsBodyExactly(t *testing.T) {
	t.Parallel()
	original := `{"fleetId":"flt_a","x":1}`
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(original))

	got, err := peekFleetIDFromBody(r, "fleetId")
	if err != nil {
		t.Fatalf("peek error: %v", err)
	}
	if got != "flt_a" {
		t.Errorf("peek fleetId = %q, want 'flt_a'", got)
	}

	// body 복원 검증.
	rest, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("re-read body: %v", err)
	}
	if string(rest) != original {
		t.Errorf("body 복원 = %q, want %q", string(rest), original)
	}
}

// TestBodyPeekHelperOversize — 10KB 초과 시 빈 fleetID + sentinel error + body 복원.
func TestBodyPeekHelperOversize(t *testing.T) {
	t.Parallel()
	original := `{"fleetId":"flt_a","filler":"` + strings.Repeat("X", 11*1024) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(original))

	got, err := peekFleetIDFromBody(r, "fleetId")
	if err == nil {
		t.Errorf("peek 10KB 초과 — error 기대")
	}
	if got != "" {
		t.Errorf("peek fleetId = %q, want '' (oversize fallback)", got)
	}

	rest, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("re-read body: %v", err)
	}
	if string(rest) != original {
		t.Errorf("oversize body 복원 길이 = %d, want %d", len(rest), len(original))
	}
}

// TestBodyPeekHelperNilBodyOK — body nil/http.NoBody 일 때 빈 string + nil 반환.
func TestBodyPeekHelperNilBodyOK(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	got, err := peekFleetIDFromBody(r, "fleetId")
	if err != nil {
		t.Errorf("nil body peek error = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("nil body peek = %q, want ''", got)
	}
}

// TestBodyPeekHelperReusableAfterPeek — peek 후 다시 io.ReadAll 가능 (NopCloser bytes.Reader).
//
// 이 단위는 위와 중복이지만 명시적으로 io.NopCloser 래핑 검증.
func TestBodyPeekHelperReusableAfterPeek(t *testing.T) {
	t.Parallel()
	original := `{"fleetId":"flt_z"}`
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(original))
	if _, err := peekFleetIDFromBody(r, "fleetId"); err != nil {
		t.Fatalf("peek: %v", err)
	}
	// 복원된 body는 ReadAll 가능 + bytes.Reader이므로 닫기 안전.
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read after peek: %v", err)
	}
	if !bytes.Equal(buf, []byte(original)) {
		t.Errorf("body 복원 mismatch")
	}
	if err := r.Body.Close(); err != nil {
		t.Errorf("close 복원된 body: %v", err)
	}
}
