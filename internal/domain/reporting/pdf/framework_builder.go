// framework_builder.go — E18 Phase 2 Framework 리포트 결정적 PDF 빌더.
//
// 기존 builder.go의 lower-level 헬퍼(setColor·outcomeColor·truncateForLine·fontFamilyKR·
// nanumGothicBytes 등)를 같은 패키지에서 재사용. 결정성 정책(R10-5)도 동일:
//   - GeneratedAt UTC만 사용 (time.Now() 호출 0)
//   - Producer 고정, gopdf protection 미사용 → /ID 비포함
//   - controls 입력은 caller가 ControlID 알파벳순 정렬 (sqliterepo가 보장)
//   - 단일 goroutine
//
// 콘텐츠 레이아웃:
//   - 페이지1: 헤더 + Profile 메타 + 점수 카드(OverallScore + 5종 status 카운트)
//   - 페이지2~N: control row 블록 (ControlID·Status·Title·Counts·Notes)
//   - 마지막 footer: audit anchor (chain head seq/hash + signedAt + signerKeyId)

package pdf

import (
	"bytes"
	"fmt"
	"time"

	"github.com/signintech/gopdf"
)

// frameworkTitle은 페이지 1 상단 큰 글씨입니다.
const frameworkTitle = "rosshield 컴플라이언스 리포트"

// frameworkOutcomeColor — framework status 문자열 → RGB.
func frameworkStatusColor(status string) rgb {
	switch status {
	case "pass":
		return colorPass
	case "fail":
		return colorFail
	case "partial":
		return colorError // 노란색 재활용
	case "not_applicable":
		return colorInd
	case "unmapped":
		return colorSkipped
	default:
		return colorBlack
	}
}

// BuildFramework는 결정적 framework PDF bytes를 반환합니다.
//
// 같은 FrameworkPDFInput → byte-identical 출력 (sha256 동일).
func (b *Builder) BuildFramework(input FrameworkPDFInput) ([]byte, error) {
	if !HasKoreanFont() {
		return nil, ErrFontUnavailable
	}

	doc := &gopdf.GoPdf{}
	doc.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})

	creationDate := input.GeneratedAt.UTC()
	doc.SetInfo(gopdf.PdfInfo{
		Title:        fmt.Sprintf("rosshield framework report %s", input.SnapshotID),
		Author:       input.TenantName,
		Subject:      fmt.Sprintf("framework=%s/%s", input.Framework, input.FrameworkVersion),
		Creator:      creatorName,
		Producer:     producerName,
		CreationDate: creationDate,
	})

	if err := doc.AddTTFFontData(fontFamilyKR, nanumGothicBytes()); err != nil {
		return nil, fmt.Errorf("pdf: register font: %w", err)
	}

	if err := writeFrameworkMetaPage(doc, input); err != nil {
		return nil, fmt.Errorf("pdf: write framework meta page: %w", err)
	}
	if err := writeFrameworkControlPages(doc, input); err != nil {
		return nil, fmt.Errorf("pdf: write framework control rows: %w", err)
	}
	if err := writeFrameworkAuditFooter(doc, input); err != nil {
		return nil, fmt.Errorf("pdf: write framework footer: %w", err)
	}

	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("pdf: write: %w", err)
	}
	return buf.Bytes(), nil
}

// FrameworkPDFInput·FrameworkPDFStats·FrameworkPDFControlRow·FrameworkAuditAnchor는
// 같은 reporting 도메인의 minimal DTO를 본 패키지에서 재정의 (P5 — pdf 패키지가 reporting을 import 안 함).
type FrameworkPDFInput struct {
	TenantID         string
	TenantName       string
	ProfileID        string
	Framework        string
	FrameworkVersion string
	SnapshotID       string
	OverallScore     float64
	Stats            FrameworkPDFStats
	Controls         []FrameworkPDFControlRow
	GeneratedAt      time.Time
	GeneratedBy      string
	AuditAnchor      PDFAuditAnchor // 기존 builder.go에 정의된 PDFAuditAnchor 재사용
}

// FrameworkPDFStats는 통제 status 분포입니다.
type FrameworkPDFStats struct {
	TotalControls int
	Pass          int
	Fail          int
	Partial       int
	NotApplicable int
	Unmapped      int
}

// FrameworkPDFControlRow는 PDF 본문 한 row입니다.
type FrameworkPDFControlRow struct {
	ControlID string
	Title     string
	Status    string
	PassCount int
	FailCount int
	Notes     string
}

func writeFrameworkMetaPage(doc *gopdf.GoPdf, in FrameworkPDFInput) error {
	doc.AddPage()

	// 헤더.
	if err := doc.SetFont(fontFamilyKR, "", titleFontSize); err != nil {
		return err
	}
	setColor(doc, colorBlack)
	doc.SetX(pageMarginPt)
	doc.SetY(pageMarginPt)
	if err := doc.Cell(nil, frameworkTitle); err != nil {
		return err
	}

	// 메타 표.
	doc.Br(lineHeightPt * 2)
	if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
		return err
	}
	metaPairs := [][2]string{
		{"Tenant", fmt.Sprintf("%s (%s)", in.TenantName, in.TenantID)},
		{"Framework", fmt.Sprintf("%s v%s", in.Framework, in.FrameworkVersion)},
		{"Profile", in.ProfileID},
		{"Snapshot", in.SnapshotID},
		{"Generated", fmt.Sprintf("%s by %s", in.GeneratedAt.UTC().Format(time.RFC3339), in.GeneratedBy)},
	}
	for _, mp := range metaPairs {
		doc.SetX(pageMarginPt)
		if err := doc.Cell(nil, fmt.Sprintf("%-12s %s", mp[0]+":", mp[1])); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}

	// 점수 카드.
	doc.Br(lineHeightPt)
	if err := doc.SetFont(fontFamilyKR, "", headerSize); err != nil {
		return err
	}
	doc.SetX(pageMarginPt)
	if err := doc.Cell(nil, "── 종합 점수 ──"); err != nil {
		return err
	}
	doc.Br(lineHeightPt)

	// OverallScore: 색상은 0.7 미만 fail, 0.9 미만 partial, 그 외 pass.
	scoreColor := colorPass
	switch {
	case in.OverallScore < 0.7:
		scoreColor = colorFail
	case in.OverallScore < 0.9:
		scoreColor = colorError
	}
	if err := doc.SetFont(fontFamilyKR, "", titleFontSize); err != nil {
		return err
	}
	setColor(doc, scoreColor)
	doc.SetX(pageMarginPt + 16)
	if err := doc.Cell(nil, fmt.Sprintf("%.1f%% (%d/%d controls)", in.OverallScore*100, in.Stats.Pass, in.Stats.TotalControls)); err != nil {
		return err
	}
	setColor(doc, colorBlack)
	doc.Br(lineHeightPt * 2)

	// Status 분포 표.
	if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
		return err
	}
	statRows := []struct {
		label string
		count int
		color rgb
	}{
		{"PASS", in.Stats.Pass, colorPass},
		{"FAIL", in.Stats.Fail, colorFail},
		{"PARTIAL", in.Stats.Partial, colorError},
		{"NOT_APPLICABLE", in.Stats.NotApplicable, colorInd},
		{"UNMAPPED", in.Stats.Unmapped, colorSkipped},
	}
	for _, sr := range statRows {
		setColor(doc, sr.color)
		doc.SetX(pageMarginPt + 16)
		if err := doc.Cell(nil, fmt.Sprintf("%-15s %d", sr.label+":", sr.count)); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}
	setColor(doc, colorBlack)
	return nil
}

func writeFrameworkControlPages(doc *gopdf.GoPdf, in FrameworkPDFInput) error {
	if len(in.Controls) == 0 {
		return nil
	}
	doc.AddPage()
	if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
		return err
	}
	const pageHeightA4 = 841.89
	const bottomLimit = pageHeightA4 - pageMarginPt - 60
	doc.SetX(pageMarginPt)
	doc.SetY(pageMarginPt)

	for i := range in.Controls {
		row := in.Controls[i]
		if doc.GetY() > bottomLimit {
			doc.AddPage()
			if err := doc.SetFont(fontFamilyKR, "", bodySize); err != nil {
				return err
			}
			doc.SetX(pageMarginPt)
			doc.SetY(pageMarginPt)
		}
		if err := writeOneFrameworkControl(doc, row); err != nil {
			return err
		}
	}
	return nil
}

func writeOneFrameworkControl(doc *gopdf.GoPdf, row FrameworkPDFControlRow) error {
	// 헤더 행: ControlID + Status(컬러) + counts.
	doc.SetX(pageMarginPt)
	if err := doc.Cell(nil, row.ControlID); err != nil {
		return err
	}
	doc.SetX(pageMarginPt + 140)
	setColor(doc, frameworkStatusColor(row.Status))
	if err := doc.Cell(nil, fmt.Sprintf("[%s]", row.Status)); err != nil {
		return err
	}
	setColor(doc, colorBlack)
	doc.SetX(pageMarginPt + 240)
	if err := doc.Cell(nil, fmt.Sprintf("pass=%d fail=%d", row.PassCount, row.FailCount)); err != nil {
		return err
	}
	doc.Br(lineHeightPt)

	// Title (있으면) + Notes (있으면).
	if row.Title != "" {
		doc.SetX(pageMarginPt + 16)
		if err := doc.Cell(nil, fmt.Sprintf("Title: %s", truncateForLine(row.Title, 110))); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}
	if row.Notes != "" {
		doc.SetX(pageMarginPt + 16)
		if err := doc.Cell(nil, fmt.Sprintf("Notes: %s", truncateForLine(row.Notes, 110))); err != nil {
			return err
		}
		doc.Br(lineHeightPt)
	}
	doc.Br(lineHeightPt / 2)
	return nil
}

func writeFrameworkAuditFooter(doc *gopdf.GoPdf, in FrameworkPDFInput) error {
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
	if err := doc.Cell(nil, "이 리포트의 무결성은 audit chain anchor로 외부 검증 가능합니다."); err != nil {
		return err
	}
	return nil
}
