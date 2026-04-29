package main

// reportexec.go — reporting 도메인의 외부 의존(scan·evidence·tenant·pdf builder)을 결선하는
// 어댑터 모음 (E8 Stage D, R10).
//
// reporting 도메인은 P5 격리를 위해 다른 도메인을 직접 import하지 않고 minimal Reader
// 인터페이스(`sqliterepo.ScanReader`·`EvidenceReader`·`TenantReader`)를 받습니다 — 본 파일이
// 각 Service를 view DTO로 어댑팅. 동일 패턴: pdf.Builder의 PDFInput과 reporting.PDFInput은
// 별 type이지만 spec이 동일하므로 1:1 변환 어댑터.

import (
	"context"

	"github.com/ssabro/rosshield/internal/domain/evidence"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/reporting/pdf"
	reportingrepo "github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// reportingScanAdapter는 scan.Service를 reporting/sqliterepo.ScanReader로 매핑.
type reportingScanAdapter struct{ svc scan.Service }

func (a *reportingScanAdapter) GetSession(ctx context.Context, tx storage.Tx, id string) (reportingrepo.ScanSessionView, error) {
	s, err := a.svc.GetSession(ctx, tx, id)
	if err != nil {
		return reportingrepo.ScanSessionView{}, err
	}
	return reportingrepo.ScanSessionView{
		ID:          s.ID,
		TenantID:    s.TenantID,
		FleetID:     s.FleetID,
		PackID:      s.PackID,
		Status:      string(s.Status),
		StartedAt:   s.StartedAt,
		CompletedAt: s.CompletedAt,
	}, nil
}

func (a *reportingScanAdapter) ListResults(ctx context.Context, tx storage.Tx, sessionID string) ([]reportingrepo.ScanResultView, error) {
	results, err := a.svc.ListResults(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]reportingrepo.ScanResultView, 0, len(results))
	for _, r := range results {
		out = append(out, reportingrepo.ScanResultView{
			ID:         r.ID,
			RobotID:    r.RobotID,
			CheckID:    r.CheckID,
			Outcome:    string(r.Outcome),
			EvalReason: r.EvalReason,
		})
	}
	return out, nil
}

// reportingEvidenceAdapter는 evidence.Service를 reporting/sqliterepo.EvidenceReader로 매핑.
type reportingEvidenceAdapter struct{ svc evidence.Service }

func (a *reportingEvidenceAdapter) ListForResult(ctx context.Context, tx storage.Tx, scanResultID string) ([]reportingrepo.EvidenceView, error) {
	records, err := a.svc.ListForResult(ctx, tx, scanResultID)
	if err != nil {
		return nil, err
	}
	out := make([]reportingrepo.EvidenceView, 0, len(records))
	for _, r := range records {
		out = append(out, reportingrepo.EvidenceView{SHA256: r.SHA256})
	}
	return out, nil
}

// reportingTenantAdapter는 tenant.Service를 reporting/sqliterepo.TenantReader로 매핑.
type reportingTenantAdapter struct{ svc tenant.Service }

func (a *reportingTenantAdapter) GetTenant(ctx context.Context, tx storage.Tx, id storage.TenantID) (reportingrepo.TenantView, error) {
	t, err := a.svc.GetTenant(ctx, tx, id)
	if err != nil {
		return reportingrepo.TenantView{}, err
	}
	return reportingrepo.TenantView{ID: t.ID, Name: t.Name}, nil
}

// pdfBuilderAdapter는 pdf.Builder를 reporting.ContentBuilder로 매핑합니다.
//
// 두 패키지의 PDFInput·PDFStats·PDFCheckRow·PDFAuditAnchor는 같은 spec이지만 별 type
// (P5: pdf 패키지가 reporting 도메인을 import하지 않음). 본 어댑터가 1:1 복사.
type pdfBuilderAdapter struct{ inner *pdf.Builder }

func (a *pdfBuilderAdapter) Build(input reporting.PDFInput) ([]byte, error) {
	return a.inner.Build(toPDFBuilderInput(input))
}

func toPDFBuilderInput(in reporting.PDFInput) pdf.PDFInput {
	rows := make([]pdf.PDFCheckRow, len(in.Rows))
	for i, r := range in.Rows {
		evs := make([]string, len(r.EvidenceSHAs))
		copy(evs, r.EvidenceSHAs)
		rows[i] = pdf.PDFCheckRow{
			Outcome:      r.Outcome,
			Severity:     r.Severity,
			CheckCode:    r.CheckCode,
			Title:        r.Title,
			RobotID:      r.RobotID,
			RobotName:    r.RobotName,
			Reason:       r.Reason,
			Rationale:    r.Rationale,
			FixGuidance:  r.FixGuidance,
			EvidenceSHAs: evs,
		}
	}
	return pdf.PDFInput{
		TenantID:         in.TenantID,
		TenantName:       in.TenantName,
		SessionID:        in.SessionID,
		SessionStartedAt: in.SessionStartedAt,
		SessionEndedAt:   in.SessionEndedAt,
		PackName:         in.PackName,
		PackVersion:      in.PackVersion,
		GeneratedAt:      in.GeneratedAt,
		GeneratedBy:      in.GeneratedBy,
		Stats: pdf.PDFStats{
			TotalChecks:   in.Stats.TotalChecks,
			Pass:          in.Stats.Pass,
			Fail:          in.Stats.Fail,
			Error:         in.Stats.Error,
			Indeterminate: in.Stats.Indeterminate,
			Skipped:       in.Stats.Skipped,
		},
		Rows: rows,
		AuditAnchor: pdf.PDFAuditAnchor{
			HeadSeq:     in.AuditAnchor.HeadSeq,
			HeadHash:    in.AuditAnchor.HeadHash,
			SignedAt:    in.AuditAnchor.SignedAt,
			SignerKeyID: in.AuditAnchor.SignerKeyID,
		},
	}
}
