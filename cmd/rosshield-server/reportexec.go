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
	"fmt"

	"github.com/ssabro/rosshield/internal/domain/compliance"
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
			TotalChecks:      in.Stats.TotalChecks,
			Pass:             in.Stats.Pass,
			Fail:             in.Stats.Fail,
			Error:            in.Stats.Error,
			Indeterminate:    in.Stats.Indeterminate,
			Skipped:          in.Stats.Skipped,
			SeverityLow:      in.Stats.SeverityLow,
			SeverityMedium:   in.Stats.SeverityMedium,
			SeverityHigh:     in.Stats.SeverityHigh,
			SeverityCritical: in.Stats.SeverityCritical,
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

// === E18 — Framework PDF builder + Compliance reader 어댑터 ===

// frameworkPdfBuilderAdapter는 pdf.Builder를 reporting.FrameworkContentBuilder로 매핑합니다.
//
// reporting.FrameworkPDFInput와 pdf.FrameworkPDFInput은 같은 spec이지만 별 type
// (P5: pdf 패키지가 reporting 도메인을 import하지 않음). 1:1 변환.
type frameworkPdfBuilderAdapter struct{ inner *pdf.Builder }

func (a *frameworkPdfBuilderAdapter) BuildFramework(input reporting.FrameworkPDFInput) ([]byte, error) {
	return a.inner.BuildFramework(toFrameworkPDFBuilderInput(input))
}

func toFrameworkPDFBuilderInput(in reporting.FrameworkPDFInput) pdf.FrameworkPDFInput {
	controls := make([]pdf.FrameworkPDFControlRow, len(in.Controls))
	for i, c := range in.Controls {
		controls[i] = pdf.FrameworkPDFControlRow{
			ControlID: c.ControlID,
			Title:     c.Title,
			Status:    c.Status,
			PassCount: c.PassCount,
			FailCount: c.FailCount,
			Notes:     c.Notes,
		}
	}
	return pdf.FrameworkPDFInput{
		TenantID:         in.TenantID,
		TenantName:       in.TenantName,
		ProfileID:        in.ProfileID,
		Framework:        in.Framework,
		FrameworkVersion: in.FrameworkVersion,
		SnapshotID:       in.SnapshotID,
		OverallScore:     in.OverallScore,
		Stats: pdf.FrameworkPDFStats{
			TotalControls: in.Stats.TotalControls,
			Pass:          in.Stats.Pass,
			Fail:          in.Stats.Fail,
			Partial:       in.Stats.Partial,
			NotApplicable: in.Stats.NotApplicable,
			Unmapped:      in.Stats.Unmapped,
		},
		Controls:    controls,
		GeneratedAt: in.GeneratedAt,
		GeneratedBy: in.GeneratedBy,
		AuditAnchor: pdf.PDFAuditAnchor{
			HeadSeq:     in.AuditAnchor.HeadSeq,
			HeadHash:    in.AuditAnchor.HeadHash,
			SignedAt:    in.AuditAnchor.SignedAt,
			SignerKeyID: in.AuditAnchor.SignerKeyID,
		},
	}
}

// complianceReaderAdapter는 compliance.Service를 reporting.ComplianceReader로 매핑합니다.
//
// 흐름: ListProfiles + ListSnapshots → profileID/snapshotID 매칭 → ControlID→Title을
// LoadFramework(YAML embed)로 보강 → FrameworkComplianceView 조립.
//
// 단순화: 데이터셋이 작아 List 후 in-memory 필터. Phase 3에서 필요 시 GetProfile/GetSnapshot 추가.
type complianceReaderAdapter struct{ svc compliance.Service }

func (a *complianceReaderAdapter) LoadProfileSnapshot(ctx context.Context, tx storage.Tx, profileID, snapshotID string) (reporting.FrameworkComplianceView, error) {
	// 1) profile 찾기.
	profiles, err := a.svc.ListProfiles(ctx, tx)
	if err != nil {
		return reporting.FrameworkComplianceView{}, fmt.Errorf("compliance reader: list profiles: %w", err)
	}
	var profile compliance.ComplianceProfile
	var found bool
	for _, p := range profiles {
		if p.ID == profileID {
			profile = p
			found = true
			break
		}
	}
	if !found {
		return reporting.FrameworkComplianceView{}, reporting.ErrFrameworkSnapshotNotFound
	}

	// 2) snapshot 찾기 (profile scope).
	snapshots, err := a.svc.ListSnapshots(ctx, tx, profileID, 0)
	if err != nil {
		return reporting.FrameworkComplianceView{}, fmt.Errorf("compliance reader: list snapshots: %w", err)
	}
	var snapshot compliance.FrameworkSnapshot
	found = false
	for _, s := range snapshots {
		if s.ID == snapshotID {
			snapshot = s
			found = true
			break
		}
	}
	if !found {
		return reporting.FrameworkComplianceView{}, reporting.ErrFrameworkSnapshotNotFound
	}

	// 3) ControlID → Title 보강 (YAML로 메모리 캐시).
	titleByID := map[string]string{}
	if defs, _, err := compliance.LoadFramework(profile.Framework); err == nil {
		for _, d := range defs {
			titleByID[d.ID] = d.Title
		}
	}
	statuses := make([]reporting.FrameworkControlStatusView, 0, len(snapshot.Statuses))
	for _, st := range snapshot.Statuses {
		statuses = append(statuses, reporting.FrameworkControlStatusView{
			ControlID: st.ControlID,
			Title:     titleByID[st.ControlID], // 없으면 빈 문자열
			Status:    string(st.Status),
			PassCount: st.PassCount,
			FailCount: st.FailCount,
			Notes:     st.Notes,
		})
	}

	return reporting.FrameworkComplianceView{
		Profile: reporting.FrameworkProfileView{
			ID:               profile.ID,
			Framework:        string(profile.Framework),
			FrameworkVersion: profile.FrameworkVersion,
		},
		Snapshot: reporting.FrameworkSnapshotView{
			ID:                 snapshot.ID,
			OverallScore:       snapshot.OverallScore,
			PassCount:          snapshot.PassCount,
			FailCount:          snapshot.FailCount,
			PartialCount:       snapshot.PartialCount,
			NotApplicableCount: snapshot.NotApplicableCount,
			UnmappedCount:      snapshot.UnmappedCount,
			ChainHeadSeq:       snapshot.ChainHeadSeq,
			ChainHeadHash:      snapshot.ChainHeadHash,
			CreatedAt:          snapshot.CreatedAt,
			Statuses:           statuses,
		},
	}, nil
}
