// Package sqliterepo는 reporting.Service의 SQLite 어댑터입니다 (E8 Stage A).
//
// 책임:
//
//	Generate    → scan.Session/Result + evidence sha 조립 → ContentBuilder.Build → reports INSERT
//	Sign        → sig_* 컬럼 UPDATE (외부 sigBytes 주입; 키 관리·서명 자체는 호출자)
//	GetReport   → reports SELECT (메타 + pdf_blob)
//	ListReports → reports SELECT (PDF nil; generated_at DESC)
//
// 외부 도메인 결합(P5):
//
//	scan/evidence/tenant 도메인은 본 패키지가 직접 import하지 않고, Deps의 minimal
//	interface(ScanReader·EvidenceReader·TenantReader)로 주입받습니다. cmd/* bootstrap이
//	각 도메인의 Service를 어댑팅해 결선 — 테스트는 fake로 단순화.
//
// 서명 부재 placeholder:
//
//	reports.sig_bytes는 BLOB NOT NULL인데 Generate 시점엔 서명이 없습니다 — INSERT 시
//	zero 64B(`make([]byte, 64)`)를 placeholder로 채우고, Sign 단계에서 UPDATE.
//	ReportSignature.IsZero()로 미서명 판정 가능.
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
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const rfc3339Nano = time.RFC3339Nano
const defaultListLimit = 50

// ScanReader는 scan 도메인 read-only 표면 (Generate가 필요로 하는 최소 set).
//
// scan.Service의 GetSession·ListResults에 1:1 매핑되며, bootstrap이 어댑터 결선.
type ScanReader interface {
	GetSession(ctx context.Context, tx storage.Tx, id string) (ScanSessionView, error)
	ListResults(ctx context.Context, tx storage.Tx, sessionID string) ([]ScanResultView, error)
}

// ScanSessionView는 reporting이 필요한 ScanSession 필드만 추린 view (P5 격리용 DTO).
type ScanSessionView struct {
	ID          string
	TenantID    storage.TenantID
	FleetID     string
	PackID      string
	Status      string // "pending"|"running"|"completed"|"failed"|"cancelled"
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// ScanResultView는 reporting이 필요한 ScanResult 필드만 추린 view.
type ScanResultView struct {
	ID         string
	RobotID    string
	CheckID    string
	Outcome    string // "pass"|"fail"|"error"|"indeterminate"|"skipped"
	EvalReason string
}

// EvidenceReader는 evidence 도메인 read-only 표면.
//
// ListForResult를 통해 한 ScanResult에 부착된 evidence sha 슬라이스를 회수.
type EvidenceReader interface {
	ListForResult(ctx context.Context, tx storage.Tx, scanResultID string) ([]EvidenceView, error)
}

// EvidenceView는 reporting이 필요한 Evidence 필드만 추린 view.
type EvidenceView struct {
	SHA256 string
}

// TenantReader는 tenant 도메인 read-only 표면 (TenantName 표시용).
type TenantReader interface {
	GetTenant(ctx context.Context, tx storage.Tx, id storage.TenantID) (TenantView, error)
}

// TenantView는 reporting이 필요한 Tenant 필드만 추린 view.
type TenantView struct {
	ID   storage.TenantID
	Name string
}

// PackReader는 pack/check 메타데이터 read-only 표면 (PDF row 보강용).
//
// nil이어도 동작 — 그 경우 PackName/PackVersion/CheckRow의 Title·Severity·Rationale·FixGuidance
// 가 공란으로 남습니다. Phase 1 minimal: 미주입을 허용해 도메인 결합을 줄임.
type PackReader interface {
	GetPack(ctx context.Context, tx storage.Tx, packID string) (PackView, error)
	GetCheck(ctx context.Context, tx storage.Tx, packCheckID string) (CheckView, error)
}

// PackView는 reporting이 필요한 Pack 필드만 추린 view.
type PackView struct {
	ID      string
	Name    string
	Version string
}

// CheckView는 reporting이 필요한 PackCheck 필드만 추린 view.
type CheckView struct {
	ID          string
	Title       string
	Severity    string
	Rationale   string
	FixGuidance string
}

// RobotReader는 robot 메타데이터 read-only 표면 (PDF row의 RobotName 보강용).
//
// nil이어도 동작 — RobotName이 빈 문자열로 남습니다.
type RobotReader interface {
	GetRobot(ctx context.Context, tx storage.Tx, robotID string) (RobotView, error)
}

// RobotView는 reporting이 필요한 Robot 필드만 추린 view.
type RobotView struct {
	ID   string
	Name string
}

// Deps는 어댑터 의존성입니다.
//
// Builder는 필수 — nil이면 Generate가 ErrBuilderNil을 반환.
// PackReader/RobotReader/Tenant는 미주입(nil) 허용 — 표시 정보만 비어남, 동작은 정상.
type Deps struct {
	Clock    clock.Clock
	IDGen    idgen.IDGen
	Audit    reporting.AuditEmitter
	Builder  reporting.ContentBuilder
	Scan     ScanReader
	Evidence EvidenceReader
	Tenant   TenantReader
	Pack     PackReader  // 옵션
	Robot    RobotReader // 옵션
}

// Repo는 reporting.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// Generate는 SessionID로부터 PDFInput을 조립 → ContentBuilder.Build → reports INSERT.
//
// 흐름:
//  1. session 조회 → status=completed 강제(R10-8 — 미완료 세션은 리포트 부적격)
//  2. results 목록 + 각 result의 evidence sha 수집
//  3. PDFInput 조립 (rows는 RobotID·CheckCode 안정 정렬)
//  4. Builder.Build → PDF bytes
//  5. sha256(PDF) + len 산출 → reports INSERT (sig_* 컬럼은 placeholder 64B zero)
//  6. AuditEmitter.EmitReportGenerated
func (r *Repo) Generate(ctx context.Context, tx storage.Tx, req reporting.GenerateRequest) (reporting.Report, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return reporting.Report{}, storage.ErrTenantMissing
	}
	if req.TenantID != "" && req.TenantID != tenantID {
		return reporting.Report{}, fmt.Errorf("reporting: input tenant=%q != tx tenant=%q", req.TenantID, tenantID)
	}
	if r.deps.Builder == nil {
		return reporting.Report{}, reporting.ErrBuilderNil
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.SessionID == "" {
		return reporting.Report{}, reporting.ErrSessionMissing
	}
	templateID := req.TemplateID
	if strings.TrimSpace(templateID) == "" {
		templateID = "default"
	}
	generatedBy := req.GeneratedBy
	if strings.TrimSpace(generatedBy) == "" {
		generatedBy = "system"
	}

	// 1) session
	session, err := r.deps.Scan.GetSession(ctx, tx, req.SessionID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return reporting.Report{}, reporting.ErrSessionNotFound
		}
		return reporting.Report{}, fmt.Errorf("reporting: get session: %w", err)
	}
	if session.Status != "completed" {
		return reporting.Report{}, reporting.ErrSessionNotCompleted
	}

	// 2) results + evidence
	results, err := r.deps.Scan.ListResults(ctx, tx, req.SessionID)
	if err != nil {
		return reporting.Report{}, fmt.Errorf("reporting: list results: %w", err)
	}
	rows, stats, err := r.assembleRows(ctx, tx, results)
	if err != nil {
		return reporting.Report{}, err
	}

	// 3) tenant/pack 메타 (옵션 — 실패 무시)
	var tenantName string
	if r.deps.Tenant != nil {
		if tv, terr := r.deps.Tenant.GetTenant(ctx, tx, tenantID); terr == nil {
			tenantName = tv.Name
		}
	}
	var packName, packVersion string
	if r.deps.Pack != nil {
		if pv, perr := r.deps.Pack.GetPack(ctx, tx, session.PackID); perr == nil {
			packName = pv.Name
			packVersion = pv.Version
		}
	}

	// 4) GeneratedAt 결정 — req 명시값 우선, zero면 Clock.
	generatedAt := req.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = r.deps.Clock.Now().UTC()
	}
	generatedAt = generatedAt.UTC()

	input := reporting.PDFInput{
		TenantID:    string(tenantID),
		TenantName:  tenantName,
		SessionID:   session.ID,
		PackName:    packName,
		PackVersion: packVersion,
		GeneratedAt: generatedAt,
		GeneratedBy: generatedBy,
		Stats:       stats,
		Rows:        rows,
		// AuditAnchor는 Sign 시점에 주입 — Generate는 zero anchor.
	}
	if session.StartedAt != nil {
		input.SessionStartedAt = session.StartedAt.UTC()
	}
	if session.CompletedAt != nil {
		input.SessionEndedAt = session.CompletedAt.UTC()
	}

	// 5) Builder
	pdfBytes, err := r.deps.Builder.Build(input)
	if err != nil {
		return reporting.Report{}, fmt.Errorf("reporting: build pdf: %w", err)
	}
	sum := sha256.Sum256(pdfBytes)
	pdfSHA := hex.EncodeToString(sum[:])

	// 6) reports INSERT
	report := reporting.Report{
		ID:           r.deps.IDGen.New("rep"),
		TenantID:     tenantID,
		TemplateID:   templateID,
		ScopeType:    reporting.ScopeSession,
		SessionID:    session.ID,
		Format:       reporting.FormatPDF,
		PDFSHA256:    pdfSHA,
		PDFSizeBytes: int64(len(pdfBytes)),
		PDF:          pdfBytes,
		GeneratedAt:  generatedAt,
		GeneratedBy:  generatedBy,
		Signature: reporting.ReportSignature{
			Algorithm: reporting.SignatureAlgorithmEd25519,
			Signature: make([]byte, reporting.Ed25519SignatureSize),
			// SignerKeyID="" / SignedAt=zero / ChainHead* = 0 → IsZero=true
		},
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO reports (
    id, tenant_id, template_id, scope_type, scope_session_id, format,
    pdf_sha256, pdf_size_bytes, pdf_blob, generated_at, generated_by,
    sig_algorithm, sig_key_id, sig_bytes, sig_signed_at,
    sig_chain_head_seq, sig_chain_head_hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, '', 0, '')`,
		report.ID, string(report.TenantID), report.TemplateID, string(report.ScopeType),
		report.SessionID, report.Format,
		report.PDFSHA256, report.PDFSizeBytes, report.PDF,
		report.GeneratedAt.Format(rfc3339Nano), report.GeneratedBy,
		report.Signature.Algorithm,
		report.Signature.Signature,
	); err != nil {
		return reporting.Report{}, fmt.Errorf("reporting: insert report: %w", err)
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitReportGenerated(ctx, tx, report); err != nil {
			return reporting.Report{}, fmt.Errorf("reporting: audit emit: %w", err)
		}
	}
	return report, nil
}

// Sign은 이미 Generate된 Report에 Ed25519 서명을 부착합니다.
func (r *Repo) Sign(ctx context.Context, tx storage.Tx, reportID string, signerKeyID string,
	sigBytes []byte, chainHeadSeq int64, chainHeadHash string, signedAt time.Time) (reporting.Report, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return reporting.Report{}, storage.ErrTenantMissing
	}
	if len(sigBytes) != reporting.Ed25519SignatureSize {
		return reporting.Report{}, reporting.ErrInvalidSignature
	}

	report, err := r.GetReport(ctx, tx, reportID)
	if err != nil {
		return reporting.Report{}, err
	}
	if !report.Signature.IsZero() {
		return reporting.Report{}, reporting.ErrAlreadySigned
	}

	signedAt = signedAt.UTC()
	if _, err := tx.Exec(ctx, `
UPDATE reports
   SET sig_algorithm       = ?,
       sig_key_id          = ?,
       sig_bytes           = ?,
       sig_signed_at       = ?,
       sig_chain_head_seq  = ?,
       sig_chain_head_hash = ?
 WHERE id = ? AND tenant_id = ?`,
		reporting.SignatureAlgorithmEd25519,
		signerKeyID, sigBytes,
		signedAt.Format(rfc3339Nano),
		chainHeadSeq, chainHeadHash,
		reportID, string(tenantID),
	); err != nil {
		return reporting.Report{}, fmt.Errorf("reporting: update signature: %w", err)
	}

	report.Signature = reporting.ReportSignature{
		Algorithm:     reporting.SignatureAlgorithmEd25519,
		SignerKeyID:   signerKeyID,
		Signature:     append([]byte(nil), sigBytes...),
		SignedAt:      signedAt,
		ChainHeadSeq:  chainHeadSeq,
		ChainHeadHash: chainHeadHash,
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitReportSigned(ctx, tx, report); err != nil {
			return reporting.Report{}, fmt.Errorf("reporting: audit emit: %w", err)
		}
	}
	return report, nil
}

// GetReport는 ID로 메타 + PDF body를 반환합니다.
func (r *Repo) GetReport(ctx context.Context, tx storage.Tx, reportID string) (reporting.Report, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return reporting.Report{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, reportSelectColumns+`, pdf_blob
  FROM reports
 WHERE id = ? AND tenant_id = ?`, reportID, string(tenantID))
	rep, err := scanReportRow(row.Scan, true)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return reporting.Report{}, reporting.ErrReportNotFound
		}
		return reporting.Report{}, err
	}
	return rep, nil
}

// ListReports는 tenant 내 리포트 메타(PDF nil)를 generated_at DESC로 반환합니다.
func (r *Repo) ListReports(ctx context.Context, tx storage.Tx, filter reporting.ListFilter) ([]reporting.Report, error) {
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
	query.WriteString(reportSelectColumns)
	query.WriteString(`
  FROM reports
 WHERE tenant_id = ?`)
	args = append(args, string(tenantID))
	if filter.SessionID != "" {
		query.WriteString(` AND scope_session_id = ?`)
		args = append(args, filter.SessionID)
	}
	query.WriteString(` ORDER BY generated_at DESC LIMIT ?`)
	args = append(args, limit)

	rows, err := tx.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("reporting: list reports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []reporting.Report
	for rows.Next() {
		rep, err := scanReportRow(rows.Scan, false)
		if err != nil {
			return nil, err
		}
		out = append(out, rep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reporting: list iterate: %w", err)
	}
	return out, nil
}

// --- helpers ---

const reportSelectColumns = `
SELECT id, tenant_id, template_id, scope_type, scope_session_id, format,
       pdf_sha256, pdf_size_bytes, generated_at, generated_by,
       sig_algorithm, sig_key_id, sig_bytes, sig_signed_at,
       sig_chain_head_seq, sig_chain_head_hash`

// assembleRows는 results를 PDFCheckRow 슬라이스 + Stats로 변환합니다.
//
// 정렬 안정: (RobotID, CheckCode) ASC. evidence sha는 EvidenceReader로 회수 (없으면 빈 슬라이스).
// Pack/Robot 메타는 Reader 주입 시에만 채움.
func (r *Repo) assembleRows(ctx context.Context, tx storage.Tx, results []ScanResultView) ([]reporting.PDFCheckRow, reporting.PDFStats, error) {
	rows := make([]reporting.PDFCheckRow, 0, len(results))
	stats := reporting.PDFStats{TotalChecks: len(results)}

	// pack/robot 캐시 — 같은 ID 반복 조회 회피.
	robotCache := map[string]RobotView{}
	checkCache := map[string]CheckView{}

	for _, res := range results {
		var evidenceSHAs []string
		if r.deps.Evidence != nil {
			evList, err := r.deps.Evidence.ListForResult(ctx, tx, res.ID)
			if err != nil {
				return nil, reporting.PDFStats{}, fmt.Errorf("reporting: list evidence for %q: %w", res.ID, err)
			}
			evidenceSHAs = make([]string, 0, len(evList))
			for _, e := range evList {
				evidenceSHAs = append(evidenceSHAs, e.SHA256)
			}
		}

		var robotName string
		if r.deps.Robot != nil {
			rv, ok := robotCache[res.RobotID]
			if !ok {
				if got, err := r.deps.Robot.GetRobot(ctx, tx, res.RobotID); err == nil {
					rv = got
					robotCache[res.RobotID] = got
				}
			}
			robotName = rv.Name
		}

		var (
			title, severity, rationale, fix string
		)
		if r.deps.Pack != nil {
			// CheckID는 도메인의 "CIS-1.1.1.1" 식별자 — Pack의 CheckView는 packCheckID(ck_*)로 조회.
			// 본 view에는 packCheckID가 없으므로 (P5 격리) 캐시 키로 res.CheckID 사용.
			cv, ok := checkCache[res.CheckID]
			if !ok {
				if got, err := r.deps.Pack.GetCheck(ctx, tx, res.CheckID); err == nil {
					cv = got
					checkCache[res.CheckID] = got
				}
			}
			title = cv.Title
			severity = cv.Severity
			rationale = cv.Rationale
			fix = cv.FixGuidance
		}

		row := reporting.PDFCheckRow{
			Outcome:      res.Outcome,
			Severity:     severity,
			CheckCode:    res.CheckID,
			Title:        title,
			RobotID:      res.RobotID,
			RobotName:    robotName,
			Reason:       res.EvalReason,
			Rationale:    rationale,
			FixGuidance:  fix,
			EvidenceSHAs: evidenceSHAs,
		}
		rows = append(rows, row)
		bumpStats(&stats, res.Outcome)
	}

	// 안정 정렬 — 같은 입력은 같은 순서를 보장 (결정성).
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].RobotID != rows[j].RobotID {
			return rows[i].RobotID < rows[j].RobotID
		}
		return rows[i].CheckCode < rows[j].CheckCode
	})

	return rows, stats, nil
}

func bumpStats(s *reporting.PDFStats, outcome string) {
	switch outcome {
	case "pass":
		s.Pass++
	case "fail":
		s.Fail++
	case "error":
		s.Error++
	case "indeterminate":
		s.Indeterminate++
	case "skipped":
		s.Skipped++
	}
}

func scanReportRow(scanFn func(...any) error, withBlob bool) (reporting.Report, error) {
	var (
		id, tenantID, templateID, scopeType, format           string
		pdfSHA, generatedAt, generatedBy                      string
		sigAlgorithm, sigKeyID, sigSignedAt, sigChainHeadHash string
		scopeSessionID                                        sql.NullString
		pdfSizeBytes, sigChainHeadSeq                         int64
		sigBytes                                              []byte
		pdfBlob                                               []byte
	)

	var err error
	if withBlob {
		err = scanFn(&id, &tenantID, &templateID, &scopeType, &scopeSessionID, &format,
			&pdfSHA, &pdfSizeBytes, &generatedAt, &generatedBy,
			&sigAlgorithm, &sigKeyID, &sigBytes, &sigSignedAt,
			&sigChainHeadSeq, &sigChainHeadHash, &pdfBlob)
	} else {
		err = scanFn(&id, &tenantID, &templateID, &scopeType, &scopeSessionID, &format,
			&pdfSHA, &pdfSizeBytes, &generatedAt, &generatedBy,
			&sigAlgorithm, &sigKeyID, &sigBytes, &sigSignedAt,
			&sigChainHeadSeq, &sigChainHeadHash)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return reporting.Report{}, storage.ErrNotFound
		}
		return reporting.Report{}, fmt.Errorf("reporting: scan row: %w", err)
	}

	gen, err := time.Parse(rfc3339Nano, generatedAt)
	if err != nil {
		return reporting.Report{}, fmt.Errorf("reporting: parse generated_at: %w", err)
	}
	rep := reporting.Report{
		ID:           id,
		TenantID:     storage.TenantID(tenantID),
		TemplateID:   templateID,
		ScopeType:    reporting.ScopeType(scopeType),
		Format:       format,
		PDFSHA256:    pdfSHA,
		PDFSizeBytes: pdfSizeBytes,
		GeneratedAt:  gen,
		GeneratedBy:  generatedBy,
		Signature: reporting.ReportSignature{
			Algorithm:     sigAlgorithm,
			SignerKeyID:   sigKeyID,
			Signature:     sigBytes,
			ChainHeadSeq:  sigChainHeadSeq,
			ChainHeadHash: sigChainHeadHash,
		},
	}
	if scopeSessionID.Valid {
		rep.SessionID = scopeSessionID.String
	}
	if sigSignedAt != "" {
		if ts, perr := time.Parse(rfc3339Nano, sigSignedAt); perr == nil {
			rep.Signature.SignedAt = ts
		}
	}
	if withBlob {
		rep.PDF = pdfBlob
	}
	return rep, nil
}
