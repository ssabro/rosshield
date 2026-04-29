// E7 Stage A — Redaction 엔진 단위 테스트.
//
// 8개 패턴(R9-5) + 함정 9종(노트 §함정) 회귀 가드.
// Phase 1 entropy 패턴은 미포함 — Phase 2 opt-in.
package evidence

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// containsMarker는 out에 [REDACTED:type:N] 마커가 들어있는지 확인합니다.
func containsMarker(t *testing.T, out []byte, typ string) bool {
	t.Helper()
	return bytes.Contains(out, []byte("[REDACTED:"+typ+":"))
}

// markerCount는 out에 등장한 임의의 [REDACTED:...] 마커 개수입니다.
func markerCount(out []byte) int {
	return bytes.Count(out, []byte("[REDACTED:"))
}

func TestRedactRemovesPasswordEqualsValue(t *testing.T) {
	in := []byte("config: password=secret123 verified")
	out, marks := Redact(in)

	if !containsMarker(t, out, "password") {
		t.Fatalf("expected password marker, got: %q", out)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d (%v)", len(marks), marks)
	}
	if marks[0].Type != "password" {
		t.Fatalf("expected type=password, got %q", marks[0].Type)
	}
	// 원본의 "password=secret123" 위치 확인 (offset 8부터 18 길이).
	wantOff := bytes.Index(in, []byte("password=secret123"))
	if marks[0].Offset != wantOff {
		t.Fatalf("offset mismatch: want %d got %d", wantOff, marks[0].Offset)
	}
	if marks[0].Length != len("password=secret123") {
		t.Fatalf("length mismatch: want %d got %d", len("password=secret123"), marks[0].Length)
	}
}

func TestRedactPreservesCRLFAroundMatches(t *testing.T) {
	in := []byte("first\r\npassword=hunter2X\r\nlast\r\n")
	out, marks := Redact(in)

	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d", len(marks))
	}
	// CRLF가 마커 양쪽에 그대로 살아있는지 확인.
	if !bytes.HasPrefix(out, []byte("first\r\n")) {
		t.Fatalf("leading CRLF lost: %q", out)
	}
	if !bytes.HasSuffix(out, []byte("\r\nlast\r\n")) {
		t.Fatalf("trailing CRLF lost: %q", out)
	}
}

func TestRedactRemovesAuthorizationBearerToken(t *testing.T) {
	in := []byte("GET /api HTTP/1.1\r\nAuthorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9\r\n")
	out, marks := Redact(in)

	if !containsMarker(t, out, "bearer-token") {
		t.Fatalf("expected bearer-token marker, got: %q", out)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d (%v)", len(marks), marks)
	}
	// Authorization 헤더 키 자체는 보존.
	if !bytes.Contains(out, []byte("Authorization: ")) {
		t.Fatalf("Authorization header label removed: %q", out)
	}
}

func TestRedactRemovesPEMPrivateKeyBlock(t *testing.T) {
	pem := "-----BEGIN OPENSSH PRIVATE KEY-----\n" +
		"b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW\n" +
		"QyNTUxOQAAACBwAAAAQwAAAAtzc2gtZWQyNTUxOQAAACAFAKEYWRESTOFLINES==\n" +
		"-----END OPENSSH PRIVATE KEY-----\n"
	in := []byte("before\n" + pem + "after\n")
	out, marks := Redact(in)

	if !containsMarker(t, out, "pem") {
		t.Fatalf("expected pem marker, got: %q", out)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d (%v)", len(marks), marks)
	}
	// 본문(중간 base64 라인)이 평문으로 남아 있으면 안 됨.
	if bytes.Contains(out, []byte("FAKEYWRESTOFLINES")) {
		t.Fatalf("PEM body leaked into output: %q", out)
	}
	// before/after 양쪽 컨텍스트는 살아있어야 함.
	if !bytes.HasPrefix(out, []byte("before\n")) {
		t.Fatalf("leading context lost: %q", out)
	}
	if !bytes.HasSuffix(out, []byte("after\n")) {
		t.Fatalf("trailing context lost: %q", out)
	}
}

func TestRedactRemovesAWSAccessKey(t *testing.T) {
	in := []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE in env\n")
	out, marks := Redact(in)

	if !containsMarker(t, out, "aws-access-key") {
		t.Fatalf("expected aws-access-key marker, got: %q", out)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d (%v)", len(marks), marks)
	}
	if bytes.Contains(out, []byte("AKIAIOSFODNN7EXAMPLE")) {
		t.Fatalf("AWS key leaked: %q", out)
	}
}

func TestRedactRemovesGitHubToken(t *testing.T) {
	cases := []string{
		"ghp_" + strings.Repeat("A", 36),
		"gho_" + strings.Repeat("B", 36),
		"ghs_" + strings.Repeat("C", 36),
	}
	for _, tok := range cases {
		t.Run(tok[:4], func(t *testing.T) {
			in := []byte("token=" + tok + " end")
			out, marks := Redact(in)
			if !containsMarker(t, out, "github-token") {
				t.Fatalf("expected github-token marker for %q, got: %q", tok, out)
			}
			if len(marks) != 1 {
				t.Fatalf("expected 1 mark, got %d", len(marks))
			}
			if bytes.Contains(out, []byte(tok)) {
				t.Fatalf("github token leaked: %q", out)
			}
		})
	}
}

func TestRedactRemovesSlackToken(t *testing.T) {
	in := []byte("slack=xoxb-1234567890-1234567890-AbCdEfGhIjKlMnOpQrStUvWx done")
	out, marks := Redact(in)

	if !containsMarker(t, out, "slack-token") {
		t.Fatalf("expected slack-token marker, got: %q", out)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d", len(marks))
	}
}

func TestRedactRemovesAPIKeyEqualsValue(t *testing.T) {
	in := []byte("config api_key=ABCDEF1234567890XYZ end")
	out, marks := Redact(in)

	if !containsMarker(t, out, "api-key") {
		t.Fatalf("expected api-key marker, got: %q", out)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d (%v)", len(marks), marks)
	}
	if bytes.Contains(out, []byte("ABCDEF1234567890XYZ")) {
		t.Fatalf("api-key value leaked: %q", out)
	}
}

func TestRedactRemovesPrivateKeyEqualsValue(t *testing.T) {
	in := []byte("env private_key=verysecretvaluehere done")
	out, marks := Redact(in)

	if !containsMarker(t, out, "private-key") {
		t.Fatalf("expected private-key marker, got: %q", out)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d (%v)", len(marks), marks)
	}
}

func TestRedactReturnsMarksInOffsetOrder(t *testing.T) {
	in := []byte("a password=onesecret1 b api_key=twosecret2 c ghp_" + strings.Repeat("Z", 36) + " d")
	_, marks := Redact(in)

	if len(marks) < 3 {
		t.Fatalf("expected 3+ marks, got %d (%v)", len(marks), marks)
	}
	if !sort.SliceIsSorted(marks, func(i, j int) bool { return marks[i].Offset < marks[j].Offset }) {
		t.Fatalf("marks not sorted by offset: %v", marks)
	}
}

func TestRedactMergesAdjacentMarks(t *testing.T) {
	// Authorization Bearer eyJ... 는 bearer-token 룰이 잡고, eyJ 본문은 PEM 영향 없음.
	// 대신 같은 영역에 password=value 를 인접시켜 병합 거동을 확인한다.
	// "password=AAA" 직후 ghp_... 토큰을 붙여 두 룰 매치 영역이 인접·겹치도록 구성.
	tok := "ghp_" + strings.Repeat("Q", 36)
	in := []byte("password=" + tok + " trailing")
	out, marks := Redact(in)

	// 단일 마커로 병합되었는지 확인.
	if got := markerCount(out); got != 1 {
		t.Fatalf("expected 1 merged marker, got %d in %q", got, out)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 merged mark, got %d (%v)", len(marks), marks)
	}
	// 우선순위에 따라 github-token이 이긴다 (password보다 specific).
	if marks[0].Type != "github-token" {
		t.Fatalf("expected merged type=github-token, got %q", marks[0].Type)
	}
}

func TestRedactPreservesNonSecretText(t *testing.T) {
	in := []byte("ros2 topic list\n/cmd_vel\n/odom\nINFO: hello world\n")
	out, marks := Redact(in)

	if len(marks) != 0 {
		t.Fatalf("expected 0 marks on benign text, got %d (%v)", len(marks), marks)
	}
	if !bytes.Equal(out, in) {
		t.Fatalf("benign text mutated:\nwant %q\ngot  %q", in, out)
	}
}

func TestRedactRejectsTruthyPasswordValue(t *testing.T) {
	cases := []string{
		"password=true",
		"password=false",
		"password=yes",
		"password=no",
		"password=enabled",
		"password=disabled",
		"password=null",
		"password=none",
		"api_key=true",
		"private_key=disabled",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			in := []byte("config: " + c + " ok")
			out, marks := Redact(in)
			if len(marks) != 0 {
				t.Fatalf("false positive: %q produced marks %v -> %q", c, marks, out)
			}
		})
	}
}

func TestRedactPreservesByteIndicesWithUTF8(t *testing.T) {
	prefix := "한글로그시작 " // UTF-8 멀티바이트
	secret := "password=koreanSecret9"
	suffix := " 한글로그끝"
	in := []byte(prefix + secret + suffix)

	out, marks := Redact(in)
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d", len(marks))
	}
	wantOff := bytes.Index(in, []byte(secret))
	if marks[0].Offset != wantOff {
		t.Fatalf("offset mismatch with utf-8 prefix: want %d got %d", wantOff, marks[0].Offset)
	}
	if marks[0].Length != len(secret) {
		t.Fatalf("length mismatch: want %d got %d", len(secret), marks[0].Length)
	}
	// UTF-8 한글 구간은 출력에도 그대로 보존되어야 한다.
	if !bytes.HasPrefix(out, []byte(prefix)) {
		t.Fatalf("utf-8 prefix lost: %q", out)
	}
	if !bytes.HasSuffix(out, []byte(suffix)) {
		t.Fatalf("utf-8 suffix lost: %q", out)
	}
}

func TestRedactPreservesByteIndicesWithInvalidUTF8(t *testing.T) {
	// invalid byte 사이에 비밀이 끼어 있어도 panic 없이 byte offset 정확.
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 0xfe, 0x00})
	off := buf.Len()
	buf.WriteString("password=invalidUTF8s")
	tail := []byte{0xfd, 0xfc}
	buf.Write(tail)
	in := buf.Bytes()

	_, marks := Redact(in)
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark in invalid utf-8 stream, got %d", len(marks))
	}
	if marks[0].Offset != off {
		t.Fatalf("offset mismatch: want %d got %d", off, marks[0].Offset)
	}
}

func TestRedactRespectsMatchCap(t *testing.T) {
	// 룰당 매치 상한(maxMatchesPerRule = 10000)을 초과하는 거대 입력에서도
	// 정상 종료하고 적어도 상한 만큼은 처리한다.
	one := []byte("password=cap_secret_value\n")
	repeats := 12000
	in := bytes.Repeat(one, repeats)

	out, marks := Redact(in)
	if len(marks) == 0 {
		t.Fatalf("expected some marks at cap, got 0")
	}
	if len(marks) > maxMatchesPerRule {
		t.Fatalf("marks exceeded cap: got %d, cap %d", len(marks), maxMatchesPerRule)
	}
	// 출력은 입력보다 짧거나 같지는 않을 수 있지만(마커가 더 길 수 있음), 정상적으로 끝나야 한다.
	if len(out) == 0 {
		t.Fatalf("output empty for large input")
	}
}

func TestRedactEmptyInputReturnsNil(t *testing.T) {
	out, marks := Redact(nil)
	if out != nil {
		t.Fatalf("expected nil out, got %v", out)
	}
	if marks != nil {
		t.Fatalf("expected nil marks, got %v", marks)
	}

	out, marks = Redact([]byte{})
	if len(out) != 0 {
		t.Fatalf("expected empty out, got %q", out)
	}
	if len(marks) != 0 {
		t.Fatalf("expected no marks, got %v", marks)
	}
}

func TestRedactDoesNotMutateInput(t *testing.T) {
	in := []byte("password=originalvalue ok")
	original := append([]byte(nil), in...)

	_, _ = Redact(in)
	if !bytes.Equal(in, original) {
		t.Fatalf("input mutated: was %q now %q", original, in)
	}
}

func TestRedactPlaceholderEncodesOriginalLength(t *testing.T) {
	in := []byte("password=lengthcheck1")
	out, marks := Redact(in)
	if len(marks) != 1 {
		t.Fatalf("expected 1 mark, got %d", len(marks))
	}
	want := fmt.Sprintf("[REDACTED:password:%d]", marks[0].Length)
	if !bytes.Contains(out, []byte(want)) {
		t.Fatalf("placeholder length encoding missing: want %q in %q", want, out)
	}
}
