// framework_repo.go — E18 Phase 2 Framework 리포트 sqlite 어댑터 메서드.
//
// reports와 별도 테이블(framework_reports)에 영속하나 ReportSignature 패턴은 동일.
//
// 흐름 (Generate):
//
//	ComplianceReader.LoadProfileSnapshot → FrameworkPDFInput 조립 →
//	FrameworkContentBuilder.BuildFramework → framework_reports INSERT (sig 64B zero placeholder)
//	→ AuditEmitter.EmitFrameworkReportGenerated.
//
// 흐름 (Sign):
//
//	GetFrameworkReport → IsZero 검증 → UPDATE sig_* + audit emit.

package sqliterepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// GenerateFramework는 (profileID, snapshotID)로 framework PDF를 생성·영속합니다.
func (r *Repo) GenerateFramework(ctx context.Context, tx storage.Tx, req reporting.GenerateFrameworkRequest) (reporting.FrameworkReport, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return reporting.FrameworkReport{}, storage.ErrTenantMissing
	}
	if req.TenantID != "" && req.TenantID != tenantID {
		return reporting.FrameworkReport{}, fmt.Errorf("reporting: input tenant=%q != tx tenant=%q", req.TenantID, tenantID)
	}
	if r.deps.FrameworkBuilder == nil {
		return reporting.FrameworkReport{}, reporting.ErrFrameworkBuilderNil
	}
	if r.deps.Compliance == nil {
		return reporting.FrameworkReport{}, reporting.ErrFrameworkComplianceReaderNil
	}
	req.ProfileID = strings.TrimSpace(req.ProfileID)
	if req.ProfileID == "" {
		return reporting.FrameworkReport{}, reporting.ErrFrameworkProfileMissing
	}
	req.SnapshotID = strings.TrimSpace(req.SnapshotID)
	if req.SnapshotID == "" {
		return reporting.FrameworkReport{}, reporting.ErrFrameworkSnapshotMissing
	}
	generatedBy := strings.TrimSpace(req.GeneratedBy)
	if generatedBy == "" {
		generatedBy = "system"
	}

	// 1) compliance reader로 입력 데이터 회수.
	view, err := r.deps.Compliance.LoadProfileSnapshot(ctx, tx, req.ProfileID, req.SnapshotID)
	if err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("reporting: load profile snapshot: %w", err)
	}

	// 2) tenant 메타 (옵션 — 실패 시 빈 문자열).
	var tenantName string
	if r.deps.Tenant != nil {
		if tv, terr := r.deps.Tenant.GetTenant(ctx, tx, tenantID); terr == nil {
			tenantName = tv.Name
		}
	}

	// 3) GeneratedAt 결정.
	generatedAt := req.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = r.deps.Clock.Now().UTC()
	}
	generatedAt = generatedAt.UTC()

	// 4) FrameworkPDFInput 조립 — controls는 ControlID 알파벳순 안정 정렬(결정성).
	// FrameworkControlStatusView·FrameworkPDFControlRow는 동일 필드라 type conversion 가능 (S1016).
	controls := make([]reporting.FrameworkPDFControlRow, 0, len(view.Snapshot.Statuses))
	for _, st := range view.Snapshot.Statuses {
		controls = append(controls, reporting.FrameworkPDFControlRow(st))
	}
	sort.SliceStable(controls, func(i, j int) bool {
		return controls[i].ControlID < controls[j].ControlID
	})

	input := reporting.FrameworkPDFInput{
		TenantID:         string(tenantID),
		TenantName:       tenantName,
		ProfileID:        view.Profile.ID,
		Framework:        view.Profile.Framework,
		FrameworkVersion: view.Profile.FrameworkVersion,
		SnapshotID:       view.Snapshot.ID,
		OverallScore:     view.Snapshot.OverallScore,
		Stats: reporting.FrameworkPDFStats{
			TotalControls: len(view.Snapshot.Statuses),
			Pass:          view.Snapshot.PassCount,
			Fail:          view.Snapshot.FailCount,
			Partial:       view.Snapshot.PartialCount,
			NotApplicable: view.Snapshot.NotApplicableCount,
			Unmapped:      view.Snapshot.UnmappedCount,
		},
		Controls:    controls,
		GeneratedAt: generatedAt,
		GeneratedBy: generatedBy,
		// AuditAnchor는 Sign 시점에 주입 — Generate는 zero anchor.
	}

	// 5) PDF 생성.
	pdfBytes, err := r.deps.FrameworkBuilder.BuildFramework(input)
	if err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("reporting: build framework PDF: %w", err)
	}
	hash := sha256.Sum256(pdfBytes)

	// 6) 영속 + audit emit (sig는 64B zero placeholder).
	report := reporting.FrameworkReport{
		ID:           r.deps.IDGen.New("frep"),
		TenantID:     tenantID,
		ProfileID:    view.Profile.ID,
		SnapshotID:   view.Snapshot.ID,
		PDFSHA256:    hex.EncodeToString(hash[:]),
		PDFSizeBytes: int64(len(pdfBytes)),
		PDF:          pdfBytes,
		GeneratedAt:  generatedAt,
		GeneratedBy:  generatedBy,
		Signature: reporting.ReportSignature{
			Algorithm:    reporting.SignatureAlgorithmEd25519,
			Signature:    make([]byte, reporting.Ed25519SignatureSize),
			SignedAt:     generatedAt, // placeholder (Sign에서 갱신)
			ChainHeadSeq: 0,
		},
	}

	if _, err := tx.Exec(ctx, `INSERT INTO framework_reports (
    id, tenant_id, profile_id, snapshot_id,
    pdf_sha256, pdf_size_bytes, pdf_blob,
    generated_at, generated_by,
    sig_algorithm, sig_key_id, sig_bytes, sig_signed_at,
    sig_chain_head_seq, sig_chain_head_hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.ID, string(report.TenantID), report.ProfileID, report.SnapshotID,
		report.PDFSHA256, report.PDFSizeBytes, report.PDF,
		report.GeneratedAt.Format(rfc3339Nano), report.GeneratedBy,
		report.Signature.Algorithm, "", report.Signature.Signature,
		report.Signature.SignedAt.Format(rfc3339Nano), int64(0), "",
	); err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("reporting: insert framework_reports: %w", err)
	}

	if err := r.deps.Audit.EmitFrameworkReportGenerated(ctx, tx, report); err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("reporting: emit framework.generated: %w", err)
	}
	return report, nil
}

// SignFramework는 framework 리포트에 Ed25519 서명을 부착합니다.
func (r *Repo) SignFramework(ctx context.Context, tx storage.Tx, reportID, signerKeyID string, sigBytes []byte,
	chainHeadSeq int64, chainHeadHash string, signedAt time.Time) (reporting.FrameworkReport, error) {
	if len(sigBytes) != reporting.Ed25519SignatureSize {
		return reporting.FrameworkReport{}, reporting.ErrInvalidSignature
	}
	report, err := r.GetFrameworkReport(ctx, tx, reportID)
	if err != nil {
		return reporting.FrameworkReport{}, err
	}
	if !report.Signature.IsZero() {
		return reporting.FrameworkReport{}, reporting.ErrAlreadySigned
	}

	signedAt = signedAt.UTC()
	if _, err := tx.Exec(ctx, `UPDATE framework_reports
SET sig_key_id = ?, sig_bytes = ?, sig_signed_at = ?,
    sig_chain_head_seq = ?, sig_chain_head_hash = ?
WHERE id = ? AND tenant_id = ?`,
		signerKeyID, sigBytes, signedAt.Format(rfc3339Nano),
		chainHeadSeq, chainHeadHash,
		report.ID, string(report.TenantID),
	); err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("reporting: update sig: %w", err)
	}

	report.Signature = reporting.ReportSignature{
		Algorithm:     reporting.SignatureAlgorithmEd25519,
		SignerKeyID:   signerKeyID,
		Signature:     append([]byte(nil), sigBytes...),
		SignedAt:      signedAt,
		ChainHeadSeq:  chainHeadSeq,
		ChainHeadHash: chainHeadHash,
	}
	if err := r.deps.Audit.EmitFrameworkReportSigned(ctx, tx, report); err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("reporting: emit framework.signed: %w", err)
	}
	return report, nil
}

// GetFrameworkReport는 ID로 framework 리포트 메타 + PDF body를 반환합니다.
func (r *Repo) GetFrameworkReport(ctx context.Context, tx storage.Tx, reportID string) (reporting.FrameworkReport, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return reporting.FrameworkReport{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, `SELECT id, tenant_id, profile_id, snapshot_id,
       pdf_sha256, pdf_size_bytes, pdf_blob,
       generated_at, generated_by,
       sig_algorithm, sig_key_id, sig_bytes, sig_signed_at,
       sig_chain_head_seq, sig_chain_head_hash
FROM framework_reports
WHERE id = ? AND tenant_id = ?`, reportID, string(tenantID))
	report, err := scanFrameworkReportFull(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return reporting.FrameworkReport{}, reporting.ErrFrameworkReportNotFound
		}
		return reporting.FrameworkReport{}, fmt.Errorf("reporting: get framework report: %w", err)
	}
	return report, nil
}

// ListFrameworkReports는 tenant 또는 profile 내 framework 리포트 메타(PDF nil)를 반환합니다.
func (r *Repo) ListFrameworkReports(ctx context.Context, tx storage.Tx, filter reporting.FrameworkListFilter) ([]reporting.FrameworkReport, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}

	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`SELECT id, tenant_id, profile_id, snapshot_id,
       pdf_sha256, pdf_size_bytes,
       generated_at, generated_by,
       sig_algorithm, sig_key_id, sig_bytes, sig_signed_at,
       sig_chain_head_seq, sig_chain_head_hash
FROM framework_reports
WHERE tenant_id = ?`)
	args = append(args, string(tenantID))
	if pid := strings.TrimSpace(filter.ProfileID); pid != "" {
		query.WriteString(` AND profile_id = ?`)
		args = append(args, pid)
	}
	query.WriteString(` ORDER BY generated_at DESC LIMIT ?`)
	args = append(args, limit)

	rows, err := tx.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("reporting: list framework reports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []reporting.FrameworkReport
	for rows.Next() {
		report, err := scanFrameworkReportMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("reporting: scan framework report: %w", err)
		}
		out = append(out, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reporting: rows err: %w", err)
	}
	return out, nil
}

// scanFrameworkReportFull은 PDF body 포함 row를 디코딩합니다 (GetFrameworkReport).
func scanFrameworkReportFull(row interface {
	Scan(...any) error
}) (reporting.FrameworkReport, error) {
	var (
		id, tenantID, profileID, snapshotID                  string
		pdfSHA, generatedBy, sigAlgo, sigKeyID, sigChainHash string
		generatedAtStr, sigSignedAtStr                       string
		pdfSize, sigChainSeq                                 int64
		pdfBlob, sigBytes                                    []byte
	)
	if err := row.Scan(&id, &tenantID, &profileID, &snapshotID,
		&pdfSHA, &pdfSize, &pdfBlob,
		&generatedAtStr, &generatedBy,
		&sigAlgo, &sigKeyID, &sigBytes, &sigSignedAtStr,
		&sigChainSeq, &sigChainHash,
	); err != nil {
		return reporting.FrameworkReport{}, err
	}
	generatedAt, err := time.Parse(rfc3339Nano, generatedAtStr)
	if err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("parse generated_at: %w", err)
	}
	sigSignedAt, err := time.Parse(rfc3339Nano, sigSignedAtStr)
	if err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("parse sig_signed_at: %w", err)
	}
	return reporting.FrameworkReport{
		ID:           id,
		TenantID:     storage.TenantID(tenantID),
		ProfileID:    profileID,
		SnapshotID:   snapshotID,
		PDFSHA256:    pdfSHA,
		PDFSizeBytes: pdfSize,
		PDF:          pdfBlob,
		GeneratedAt:  generatedAt,
		GeneratedBy:  generatedBy,
		Signature: reporting.ReportSignature{
			Algorithm:     sigAlgo,
			SignerKeyID:   sigKeyID,
			Signature:     sigBytes,
			SignedAt:      sigSignedAt,
			ChainHeadSeq:  sigChainSeq,
			ChainHeadHash: sigChainHash,
		},
	}, nil
}

// scanFrameworkReportMeta은 PDF body 미포함 row를 디코딩합니다 (ListFrameworkReports).
func scanFrameworkReportMeta(rows *sql.Rows) (reporting.FrameworkReport, error) {
	var (
		id, tenantID, profileID, snapshotID                  string
		pdfSHA, generatedBy, sigAlgo, sigKeyID, sigChainHash string
		generatedAtStr, sigSignedAtStr                       string
		pdfSize, sigChainSeq                                 int64
		sigBytes                                             []byte
	)
	if err := rows.Scan(&id, &tenantID, &profileID, &snapshotID,
		&pdfSHA, &pdfSize,
		&generatedAtStr, &generatedBy,
		&sigAlgo, &sigKeyID, &sigBytes, &sigSignedAtStr,
		&sigChainSeq, &sigChainHash,
	); err != nil {
		return reporting.FrameworkReport{}, err
	}
	generatedAt, err := time.Parse(rfc3339Nano, generatedAtStr)
	if err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("parse generated_at: %w", err)
	}
	sigSignedAt, err := time.Parse(rfc3339Nano, sigSignedAtStr)
	if err != nil {
		return reporting.FrameworkReport{}, fmt.Errorf("parse sig_signed_at: %w", err)
	}
	return reporting.FrameworkReport{
		ID:           id,
		TenantID:     storage.TenantID(tenantID),
		ProfileID:    profileID,
		SnapshotID:   snapshotID,
		PDFSHA256:    pdfSHA,
		PDFSizeBytes: pdfSize,
		GeneratedAt:  generatedAt,
		GeneratedBy:  generatedBy,
		Signature: reporting.ReportSignature{
			Algorithm:     sigAlgo,
			SignerKeyID:   sigKeyID,
			Signature:     sigBytes,
			SignedAt:      sigSignedAt,
			ChainHeadSeq:  sigChainSeq,
			ChainHeadHash: sigChainHash,
		},
	}, nil
}
