// Package pdf는 ScanSession 결과를 결정적(byte-identical) PDF로 변환하는 builder를
// 제공합니다.
//
// 본 패키지는 E8 Reporting epic의 Stage B 산출물입니다. Stage A의 도메인 모델
// (`internal/domain/reporting/reporting.go`)이 본 패키지의 `PDFInput`을 채우며, Stage C에서
// 두 산출물(builder + 도메인 service)을 봉합한 뒤 detached `.sig` 서명·번들 결선이 진행
// 됩니다(`docs/design/notes/e8-pdf-signature-research.md`).
//
// 결정성 절대 전제(R10-5):
//
//   - `PDFInput.GeneratedAt` UTC를 PDF Info dict의 CreationDate에 그대로 박는다.
//     (gopdf 내부에서 `time.Now()` 호출 없음 — `pdf_info_obj.go` 의 zero-time 분기 참조.)
//   - `Producer = "rosshield E8 v1"` 고정 상수.
//   - gopdf는 protection 미사용 시 trailer `/ID`를 출력하지 않으므로(자체 검증, gopdf.go
//     §xref 함수), random ID로 인한 비결정성이 발생하지 않는다.
//   - subset prefix는 family name 그대로(`strhelper.go:CreateEmbeddedFontSubsetName`),
//     랜덤 prefix 비결정성 없음.
//   - 모든 map 순회는 sorted slice로 변환 후 직렬 처리. goroutine 미사용.
//
// 콘텐츠 레이아웃(R10-8): 페이지1 = 메타+통계, 페이지2~N = check 상세, 마지막 페이지
// footer = audit anchor 가시화. 자세한 픽셀이 아닌 결정성·가독성을 우선한다.
//
// 도메인 결합 규칙(P5): 본 패키지는 다른 도메인(scan·robot·tenant·audit·evidence) 패키지를
// import하지 않는다. PDFInput은 self-contained DTO이며, 도메인 service가 어댑터한다.
package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/signintech/gopdf"
)

// 결정성·메타 상수. 변경하면 모든 골든 fixture sha256이 깨집니다(의도된 변경 시
// `testdata/golden_*.sha256` 재생성).
const (
	producerName  = "rosshield E8 v1"
	creatorName   = "rosshield-server"
	fontFamilyKR  = "NanumGothic"
	pageMarginPt  = 36.0 // 0.5 inch (72pt = 1in)
	titleFontSize = 18.0
	headerSize    = 12.0
	bodySize      = 10.0
	smallSize     = 9.0
	lineHeightPt  = 14.0
)

// 통계 색상 RGB(R10-8). hex 표기는 디자인 노트 참조용 — 실제 PDF 출력은 RGB 0~1 float.
//
//	PASS  #22c55e  (34, 197, 94)
//	FAIL  #ef4444  (239, 68, 68)
//	ERROR #eab308  (234, 179, 8)
//	IND.  #6b7280  (107, 114, 128)
//	SKIP  #9ca3af  (156, 163, 175)
//
// 흑색 본문과 색상 통계가 한 페이지에 공존하므로 SetTextColor 호출 카운트로
// 회귀 검증한다(builder_test.go `TestBuildAppliesPassFailColors`).
var (
	colorPass    = rgb{0x22, 0xc5, 0x5e}
	colorFail    = rgb{0xef, 0x44, 0x44}
	colorError   = rgb{0xea, 0xb3, 0x08}
	colorInd     = rgb{0x6b, 0x72, 0x80}
	colorSkipped = rgb{0x9c, 0xa3, 0xaf}
	colorBlack   = rgb{0x00, 0x00, 0x00}
)

type rgb struct{ R, G, B uint8 }

// PDFInput은 Stage A 도메인이 채우는 결정적 변환 입력입니다. 본 패키지는 도메인을
// import하지 않으므로 형은 자체 정의하되, Stage A `internal/domain/reporting/reporting.go`
// 와 글자 그대로 동일한 form으로 유지합니다(어댑터에서 1:1 복사).
type PDFInput struct {
	TenantID         string
	TenantName       string
	SessionID        string
	SessionStartedAt time.Time
	SessionEndedAt   time.Time
	PackName         string
	PackVersion      string
	GeneratedAt      time.Time // 결정성: caller가 명시 입력. UTC 권장.
	GeneratedBy      string

	Stats PDFStats
	Rows  []PDFCheckRow

	AuditAnchor PDFAuditAnchor
}

// PDFStats는 outcome 분포 요약 + severity 분포입니다.
//
// SeverityLow/Medium/High/Critical은 row의 severity 분포 (CIS pack 등 base에서). 0 카운트는
// 통계 섹션에서 노출 X (compact). 도메인 어댑터(reporting.PDFStats)와 1:1 mirror.
type PDFStats struct {
	TotalChecks   int
	Pass          int
	Fail          int
	Error         int
	Indeterminate int
	Skipped       int

	SeverityLow      int
	SeverityMedium   int
	SeverityHigh     int
	SeverityCritical int
}

// PDFCheckRow는 single check 결과입니다. 한 row가 한 화면 블록을 차지합니다(긴 텍스트는
// MultiCell이 줄바꿈).
type PDFCheckRow struct {
	Outcome      string
	Severity     string
	CheckCode    string
	Title        string
	RobotID      string
	RobotName    string
	Reason       string
	Rationale    string
	FixGuidance  string
	EvidenceSHAs []string
}

// PDFAuditAnchor는 마지막 페이지 footer에 가시화되는 audit chain head 정보입니다.
type PDFAuditAnchor struct {
	HeadSeq     int64
	HeadHash    string
	SignedAt    time.Time
	SignerKeyID string
}

// Builder는 PDFInput → PDF bytes의 결정적 변환자입니다. 상태 없음(stateless) — 동시 호출
// 가능. 단일 Build 안에서는 단일 goroutine으로 처리합니다(R10-5 함정 5).
type Builder struct{}

// New는 새 Builder를 반환합니다.
func New() *Builder {
	return &Builder{}
}

// 표준 에러.
var (
	// ErrFontUnavailable은 NanumGothic 폰트가 임베드되지 않은 빌드에서 Build를 호출했을
	// 때 반환됩니다. `HasKoreanFont()`로 사전 확인 가능.
	ErrFontUnavailable = errors.New("pdf: korean font (NanumGothic) is not embedded")
)

// Build는 결정적 PDF bytes를 반환합니다. 같은 PDFInput → byte-identical 출력
// (sha256 동일). gopdf의 random source는 이용하지 않으며, GeneratedAt UTC와 고정 상수만
// 사용합니다.
func (b *Builder) Build(input PDFInput) ([]byte, error) {
	if !HasKoreanFont() {
		return nil, ErrFontUnavailable
	}

	doc := &gopdf.GoPdf{}
	doc.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})

	// SetInfo: GeneratedAt UTC를 CreationDate로 박는다 — caller 책임. tz가 다르면 byte가
	// 변하므로 builder 측에서 강제 정규화한다.
	creationDate := input.GeneratedAt.UTC()
	doc.SetInfo(gopdf.PdfInfo{
		Title:        fmt.Sprintf("rosshield report %s", input.SessionID),
		Author:       input.TenantName,
		Subject:      fmt.Sprintf("pack=%s/%s", input.PackName, input.PackVersion),
		Creator:      creatorName,
		Producer:     producerName,
		CreationDate: creationDate,
	})

	// 폰트 등록. AddTTFFontByReader가 외부 io.Reader를 받지만 결정성 위해 byte slice를
	// 직접 전달하는 AddTTFFontData 사용.
	if err := doc.AddTTFFontData(fontFamilyKR, nanumGothicBytes()); err != nil {
		return nil, fmt.Errorf("pdf: register font: %w", err)
	}

	if err := writeMetaAndStatsPage(doc, input); err != nil {
		return nil, fmt.Errorf("pdf: write meta page: %w", err)
	}
	if err := writeCheckRowsPages(doc, input); err != nil {
		return nil, fmt.Errorf("pdf: write check rows: %w", err)
	}
	if err := writeAuditAnchorFooter(doc, input); err != nil {
		return nil, fmt.Errorf("pdf: write footer: %w", err)
	}

	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("pdf: write: %w", err)
	}
	return buf.Bytes(), nil
}

// writeMetaAndStatsPage — 페이지 1: 메타 헤더 + 통계 표.
func writeMetaAndStatsPage(doc *gopdf.GoPdf, in PDFInput) error {
	doc.AddPage()

	// 헤더.
	if err := doc.SetFont(fontFamilyKR, "", titleFontSize); err != nil {
		return err
	}
	setColor(doc, colorBlack)
	doc.SetX(pageMarginPt)
	doc.SetY(pageMarginPt)
	if err := doc.Cell(nil, "rosshield 보안 감사 리포트"); err != nil {
		return err
	}

	// 메타 표.
	doc.Br(lineHeightPt * 2)
	if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
		return err
	}
	metaPairs := [][2]string{
		{"Tenant", fmt.Sprintf("%s (%s)", in.TenantName, in.TenantID)},
		{"Session", in.SessionID},
		{"Pack", fmt.Sprintf("%s v%s", in.PackName, in.PackVersion)},
		{"Started", in.SessionStartedAt.UTC().Format(time.RFC3339)},
		{"Ended", in.SessionEndedAt.UTC().Format(time.RFC3339)},
		{"Generated", fmt.Sprintf("%s by %s", in.GeneratedAt.UTC().Format(time.RFC3339), in.GeneratedBy)},
	}
	for _, mp := range metaPairs {
		doc.SetX(pageMarginPt)
		if err := doc.Cell(nil, fmt.Sprintf("%-12s %s", mp[0]+":", mp[1])); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}

	// 통계 헤더.
	doc.Br(lineHeightPt)
	if err := doc.SetFont(fontFamilyKR, "", headerSize); err != nil {
		return err
	}
	doc.SetX(pageMarginPt)
	if err := doc.Cell(nil, "── 통계 ──"); err != nil {
		return err
	}
	doc.Br(lineHeightPt)
	if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
		return err
	}
	doc.SetX(pageMarginPt)
	if err := doc.Cell(nil, fmt.Sprintf("Total checks: %d", in.Stats.TotalChecks)); err != nil {
		return err
	}
	doc.Br(lineHeightPt)

	statRows := []struct {
		label string
		count int
		color rgb
	}{
		{"PASS", in.Stats.Pass, colorPass},
		{"FAIL", in.Stats.Fail, colorFail},
		{"ERROR", in.Stats.Error, colorError},
		{"INDETERMINATE", in.Stats.Indeterminate, colorInd},
		{"SKIPPED", in.Stats.Skipped, colorSkipped},
	}
	for _, sr := range statRows {
		setColor(doc, sr.color)
		doc.SetX(pageMarginPt + 16) // 살짝 들여쓰기.
		if err := doc.Cell(nil, fmt.Sprintf("%-15s %d", sr.label+":", sr.count)); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}
	setColor(doc, colorBlack)

	// 심각도(severity) 분포 — Phase 5 severity classification(CIS Level 1/2 + critical section).
	// 0 카운트인 tier는 노출 X (compact). 모두 0이면 섹션 자체 skip.
	if hasAnySeverity(in.Stats) {
		doc.Br(lineHeightPt)
		if err := doc.SetFont(fontFamilyKR, "", headerSize); err != nil {
			return err
		}
		doc.SetX(pageMarginPt)
		if err := doc.Cell(nil, "── 심각도 분포 ──"); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
		if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
			return err
		}
		sevRows := []struct {
			label string
			count int
		}{
			{"CRITICAL", in.Stats.SeverityCritical},
			{"HIGH", in.Stats.SeverityHigh},
			{"MEDIUM", in.Stats.SeverityMedium},
			{"LOW", in.Stats.SeverityLow},
		}
		for _, sr := range sevRows {
			if sr.count == 0 {
				continue
			}
			doc.SetX(pageMarginPt + 16)
			if err := doc.Cell(nil, fmt.Sprintf("%-15s %d", sr.label+":", sr.count)); err != nil {
				return err
			}
			doc.Br(lineHeightPt)
		}
	}
	return nil
}

// hasAnySeverity는 Stats에 severity 카운트가 1건 이상 있는지 검사 — 모두 0이면 섹션 skip.
func hasAnySeverity(s PDFStats) bool {
	return s.SeverityLow > 0 || s.SeverityMedium > 0 || s.SeverityHigh > 0 || s.SeverityCritical > 0
}

// writeCheckRowsPages — 페이지 2~N: check 상세.
func writeCheckRowsPages(doc *gopdf.GoPdf, in PDFInput) error {
	if len(in.Rows) == 0 {
		return nil
	}
	doc.AddPage()
	if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
		return err
	}
	const pageHeightA4 = 841.89 // pt
	const bottomLimit = pageHeightA4 - pageMarginPt - 60
	doc.SetX(pageMarginPt)
	doc.SetY(pageMarginPt)

	for i := range in.Rows {
		row := in.Rows[i]
		// 페이지 분기: 다음 row 블록이 안 들어가면 새 페이지.
		if doc.GetY() > bottomLimit {
			doc.AddPage()
			if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
				return err
			}
			doc.SetX(pageMarginPt)
			doc.SetY(pageMarginPt)
		}
		if err := writeOneCheckRow(doc, row); err != nil {
			return err
		}
	}
	return nil
}

func writeOneCheckRow(doc *gopdf.GoPdf, row PDFCheckRow) error {
	// 헤더 행: CheckCode + Outcome(컬러) + Severity.
	doc.SetX(pageMarginPt)
	if err := doc.Cell(nil, row.CheckCode); err != nil {
		return err
	}
	doc.SetX(pageMarginPt + 100)
	setColor(doc, outcomeColor(row.Outcome))
	if err := doc.Cell(nil, fmt.Sprintf("[%s]", row.Outcome)); err != nil {
		return err
	}
	setColor(doc, colorBlack)
	doc.SetX(pageMarginPt + 180)
	if err := doc.Cell(nil, row.Severity); err != nil {
		return err
	}
	doc.Br(lineHeightPt)

	// 상세 본문(들여쓰기). 긴 텍스트는 truncate(결정성 우선, 줄바꿈은 단순화).
	pairs := [][2]string{
		{"Title", row.Title},
		{"Robot", fmt.Sprintf("%s (%s)", row.RobotName, row.RobotID)},
		{"Reason", row.Reason},
		{"Rationale", row.Rationale},
		{"FixGuidance", row.FixGuidance},
	}
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		doc.SetX(pageMarginPt + 16)
		if err := doc.Cell(nil, fmt.Sprintf("%s: %s", p[0], truncateForLine(p[1], 110))); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}
	if len(row.EvidenceSHAs) > 0 {
		// EvidenceSHAs는 caller 순서를 신뢰하지 않고 sort(R10-5 함정 4 — slice 순서 안정).
		evs := append([]string(nil), row.EvidenceSHAs...)
		sort.Strings(evs)
		head := evs[0]
		if len(head) > 16 {
			head = head[:16]
		}
		doc.SetX(pageMarginPt + 16)
		if err := doc.Cell(nil, fmt.Sprintf("Evidence: %s... (%d blob 참조)", head, len(evs))); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}
	doc.Br(lineHeightPt / 2) // 블록 간 간격.
	return nil
}

func writeAuditAnchorFooter(doc *gopdf.GoPdf, in PDFInput) error {
	// 마지막 페이지 안으로 들어가지만, 페이지 부족하면 새 페이지로.
	const pageHeightA4 = 841.89
	const footerHeight = 120.0
	if doc.GetY() > pageHeightA4-pageMarginPt-footerHeight {
		doc.AddPage()
		if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
			return err
		}
	}
	doc.SetX(pageMarginPt)
	doc.Br(lineHeightPt)
	doc.SetX(pageMarginPt)
	if err := doc.Cell(nil, "─────────────────────────────────────────"); err != nil {
		return err
	}
	doc.Br(lineHeightPt)
	doc.SetX(pageMarginPt)
	if err := doc.Cell(nil, "Audit Chain Anchor:"); err != nil {
		return err
	}
	doc.Br(lineHeightPt)
	a := in.AuditAnchor
	footerLines := []string{
		fmt.Sprintf("  Head Seq: %d", a.HeadSeq),
		fmt.Sprintf("  Head Hash: %s", a.HeadHash),
		fmt.Sprintf("  Signed At: %s", a.SignedAt.UTC().Format(time.RFC3339)),
		fmt.Sprintf("  Signer Key ID: %s", a.SignerKeyID),
	}
	for _, ln := range footerLines {
		doc.SetX(pageMarginPt)
		if err := doc.Cell(nil, ln); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}
	doc.Br(lineHeightPt)
	doc.SetX(pageMarginPt)
	if err := doc.SetFont(fontFamilyKR, "", smallSize); err != nil {
		return err
	}
	if err := doc.Cell(nil, "이 리포트의 무결성은 `rosshield-server report verify <bundle>` 명령으로 검증 가능합니다."); err != nil {
		return err
	}
	return nil
}

// outcomeColor — outcome 문자열 → RGB.
func outcomeColor(outcome string) rgb {
	switch outcome {
	case "PASS", "pass":
		return colorPass
	case "FAIL", "fail":
		return colorFail
	case "ERROR", "error":
		return colorError
	case "INDETERMINATE", "indeterminate":
		return colorInd
	case "SKIPPED", "skipped":
		return colorSkipped
	default:
		return colorBlack
	}
}

func setColor(doc *gopdf.GoPdf, c rgb) {
	doc.SetTextColor(c.R, c.G, c.B)
}

// truncateForLine — 너무 긴 한 줄을 잘라내 페이지 밖으로 새지 않게 한다. 결정성 보존
// (rune-safe).
func truncateForLine(s string, max int) string {
	if max <= 3 || len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-3]) + "..."
}
