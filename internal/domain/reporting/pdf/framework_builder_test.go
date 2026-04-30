package pdf_test

// framework_builder_test.go — E18 Phase 2 Framework PDF builder 결정성·회귀 테스트.

import (
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting/pdf"
)

// fwFixedTime은 framework PDF 결정성 테스트의 기준 시각.
var fwFixedTime = time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)

func minimalFrameworkInput() pdf.FrameworkPDFInput {
	return pdf.FrameworkPDFInput{
		TenantID:         "tn_acme",
		TenantName:       "Acme Robotics",
		ProfileID:        "cp_ABC",
		Framework:        "isms-p",
		FrameworkVersion: "2024",
		SnapshotID:       "fs_XYZ",
		OverallScore:     0.83,
		Stats: pdf.FrameworkPDFStats{
			TotalControls: 3,
			Pass:          1,
			Fail:          1,
			Partial:       0,
			NotApplicable: 0,
			Unmapped:      1,
		},
		Controls: []pdf.FrameworkPDFControlRow{
			{ControlID: "ISMS-P:2.5.1", Title: "접근 권한", Status: "pass", PassCount: 2},
			{ControlID: "ISMS-P:2.5.2", Title: "패스워드", Status: "fail", FailCount: 3},
			{ControlID: "ISMS-P:2.5.3", Title: "세션 제어", Status: "unmapped"},
		},
		GeneratedAt: fwFixedTime,
		GeneratedBy: "auditor@acme",
		AuditAnchor: pdf.PDFAuditAnchor{
			HeadSeq:     42,
			HeadHash:    "deadbeef",
			SignedAt:    fwFixedTime,
			SignerKeyID: "key_TEST",
		},
	}
}

// TestBuildFrameworkProducesNonEmptyPDF — 결과물이 PDF 형식이고 비어있지 않다.
func TestBuildFrameworkProducesNonEmptyPDF(t *testing.T) {
	t.Parallel()
	if !pdf.HasKoreanFont() {
		t.Skip("NanumGothic font not embedded")
	}

	b := pdf.New()
	out, err := b.BuildFramework(minimalFrameworkInput())
	if err != nil {
		t.Fatalf("BuildFramework: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("output is empty")
	}
	if !strings.HasPrefix(string(out[:8]), "%PDF-") {
		t.Errorf("output does not start with %%PDF-: %q", out[:8])
	}
}

// TestBuildFrameworkIsDeterministic — 같은 입력 → byte-identical 출력.
func TestBuildFrameworkIsDeterministic(t *testing.T) {
	t.Parallel()
	if !pdf.HasKoreanFont() {
		t.Skip("NanumGothic font not embedded")
	}

	b := pdf.New()
	in := minimalFrameworkInput()

	out1, err := b.BuildFramework(in)
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}
	out2, err := b.BuildFramework(in)
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if len(out1) != len(out2) {
		t.Fatalf("len mismatch: %d vs %d", len(out1), len(out2))
	}
	h1 := sha256.Sum256(out1)
	h2 := sha256.Sum256(out2)
	if h1 != h2 {
		t.Errorf("sha256 mismatch — framework PDF not deterministic")
	}
}

// TestBuildFrameworkHandlesEmptyControls — 통제 리스트가 비어도 정상 (메타 + 점수만).
func TestBuildFrameworkHandlesEmptyControls(t *testing.T) {
	t.Parallel()
	if !pdf.HasKoreanFont() {
		t.Skip("NanumGothic font not embedded")
	}

	b := pdf.New()
	in := minimalFrameworkInput()
	in.Controls = nil
	in.Stats.TotalControls = 0
	in.Stats.Pass = 0
	in.Stats.Fail = 0
	in.Stats.Unmapped = 0

	out, err := b.BuildFramework(in)
	if err != nil {
		t.Fatalf("BuildFramework: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("output empty")
	}
}

// TestBuildFrameworkPaginatesManyControls — 100 controls일 때 페이지 분기.
func TestBuildFrameworkPaginatesManyControls(t *testing.T) {
	t.Parallel()
	if !pdf.HasKoreanFont() {
		t.Skip("NanumGothic font not embedded")
	}

	b := pdf.New()
	in := minimalFrameworkInput()

	rows := make([]pdf.FrameworkPDFControlRow, 100)
	statuses := []string{"pass", "fail", "partial", "not_applicable", "unmapped"}
	for i := range rows {
		rows[i] = pdf.FrameworkPDFControlRow{
			ControlID: testFwControlID(i),
			Title:     testFwControlTitle(i),
			Status:    statuses[i%len(statuses)],
			PassCount: i % 5,
			FailCount: (i * 3) % 7,
		}
	}
	in.Controls = rows
	in.Stats.TotalControls = 100

	out, err := b.BuildFramework(in)
	if err != nil {
		t.Fatalf("BuildFramework: %v", err)
	}
	if len(out) < 5_000 {
		t.Errorf("output suspiciously small for 100 controls: %d bytes", len(out))
	}
}

func testFwControlID(i int) string {
	// 결정적 ID — sort 가능하게 zero-pad.
	return "CTL-" + zeroPad4(i)
}

func testFwControlTitle(i int) string {
	return "통제 항목 " + zeroPad4(i)
}

func zeroPad4(n int) string {
	s := []byte{'0', '0', '0', '0'}
	for i := len(s) - 1; i >= 0 && n > 0; i-- {
		s[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(s)
}
