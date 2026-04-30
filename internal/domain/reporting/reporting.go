// Package reporting는 ScanSession 결과를 서명된 PDF로 묶는 도메인입니다 (E8).
//
// 책임 분담(R10):
//
//   - Stage A (본 패키지): 도메인 모델 + Service interface + sqliterepo
//     PDF 생성 자체는 ContentBuilder interface 주입 — Stage B(internal/domain/reporting/pdf)가 구현
//     서명은 Service.Sign 단계에서 외부 sigBytes 주입 — 키 관리·서명 자체는 호출자 책임 (Stage C)
//
//   - R10-2: detached `.sig` (minisign 호환) 보조 출력 — Stage C에서 결선
//
//   - R10-3: audit anchor = {tenantId, headSeq, headHash, signedAt, signerKeyId} JSON
//
//   - R10-7: report 키 ↔ audit checkpoint 키 분리 (호출자가 적절한 키를 선택)
//
//   - R10-8: 콘텐츠 = 메타 → 통계 → check 상세 → audit anchor footer
//
// 도메인 결합 규칙(P5):
//
//	본 도메인은 application service 성격이 강해 외부 도메인(scan·evidence·tenant)을
//	직접 import하되, 호출은 각 도메인의 Service interface를 통해서만 — repo 내부 직접
//	접근은 금지. AuditEmitter는 다른 도메인 패턴과 동일하게 interface 주입 (cmd/* bootstrap에서
//	audit.Service 어댑터 결선).
package reporting

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// ScopeType은 리포트 범위입니다.
//
// Phase 1은 ScopeSession만 — 한 ScanSession 결과를 묶음. fleet/tenant 스코프는 Phase 2.
type ScopeType string

const (
	ScopeSession ScopeType = "session"
	ScopeFleet   ScopeType = "fleet"  // Phase 2
	ScopeTenant  ScopeType = "tenant" // Phase 2
)

// FormatPDF는 Phase 1에서 유일하게 지원되는 포맷입니다.
const FormatPDF = "pdf"

// SignatureAlgorithmEd25519는 Phase 1 표준 서명 알고리즘입니다.
const SignatureAlgorithmEd25519 = "ed25519"

// ed25519SignatureSize는 Ed25519 서명 byte 크기입니다.
const ed25519SignatureSize = 64

// Report는 생성·서명된 PDF의 메타입니다 (§04.2 Report).
//
// PDF 본문 자체는 Phase 1에서 reports.pdf_blob 컬럼에 inline 저장(단순화).
// Phase 2에서 blobstore로 외부화 검토.
type Report struct {
	ID           string
	TenantID     storage.TenantID
	TemplateID   string
	ScopeType    ScopeType
	SessionID    string // ScopeType=session일 때만
	Format       string // "pdf"
	PDFSHA256    string // 64자 lowercase hex
	PDFSizeBytes int64
	PDF          []byte // 본문 (Read 시에만 채움; List는 nil)
	GeneratedAt  time.Time
	GeneratedBy  string
	Signature    ReportSignature
}

// ReportSignature는 §04.2 ReportSignature 인라인입니다.
//
// Generate 직후에는 zero (Algorithm/SignerKeyID/Signature 모두 빈/zero); Sign 호출 후 채워짐.
// chain head 스냅샷은 서명 시점의 audit chain 상태로 검증자가 cross-check 가능.
type ReportSignature struct {
	Algorithm     string // "ed25519"
	SignerKeyID   string // "key_<hex>"
	Signature     []byte // 64B Ed25519 sig
	SignedAt      time.Time
	ChainHeadSeq  int64
	ChainHeadHash string // hex
}

// IsZero는 서명이 부착되지 않은 상태(Generate 직후)를 판정합니다.
//
// 모든 byte가 0인 placeholder Signature(또는 빈 슬라이스)는 미서명으로 간주합니다.
// Sign 단계가 64B sig를 UPDATE하면 IsZero=false로 전이.
func (s ReportSignature) IsZero() bool {
	if len(s.Signature) == 0 {
		return true
	}
	for _, b := range s.Signature {
		if b != 0 {
			return false
		}
	}
	return true
}

// PDFInput은 ContentBuilder에 전달되는 결정적 입력입니다 (Stage B builder가 그대로 수용).
//
// **시그니처 변경 금지** — Stage B 에이전트가 같은 spec으로 builder를 구현 중.
type PDFInput struct {
	TenantID         string
	TenantName       string
	SessionID        string
	SessionStartedAt time.Time
	SessionEndedAt   time.Time
	PackName         string
	PackVersion      string
	GeneratedAt      time.Time // 결정성: caller가 명시 입력
	GeneratedBy      string

	Stats PDFStats
	Rows  []PDFCheckRow

	AuditAnchor PDFAuditAnchor
}

// PDFStats는 outcome 별 집계입니다.
type PDFStats struct {
	TotalChecks   int
	Pass          int
	Fail          int
	Error         int
	Indeterminate int
	Skipped       int
}

// PDFCheckRow는 PDF 본문에 출력되는 한 row입니다.
//
// EvidenceSHAs는 evidence sha256 hex 참조만 — blob 본문은 임베드하지 않음(R10-8 본문 분리).
type PDFCheckRow struct {
	Outcome      string // "pass"|"fail"|"error"|"indeterminate"|"skipped"
	Severity     string // "low"|"medium"|"high"|"critical"
	CheckCode    string // "CIS-1.1.1.1"
	Title        string
	RobotID      string
	RobotName    string
	Reason       string
	Rationale    string
	FixGuidance  string
	EvidenceSHAs []string // sha256 hex 참조 (blob 임베드 X)
}

// PDFAuditAnchor는 R10-3 anchor 페이로드 (PDF footer 가시화 + JSON anchor).
type PDFAuditAnchor struct {
	HeadSeq     int64
	HeadHash    string // hex
	SignedAt    time.Time
	SignerKeyID string
}

// ContentBuilder는 PDFInput → PDF bytes의 결정적 변환자입니다.
//
// Stage B에서 signintech/gopdf 기반 구현체 제공 — 같은 입력 → byte-identical PDF.
// 본 Stage A 테스트는 fakeBuilder로 입력 검증만 진행.
type ContentBuilder interface {
	Build(input PDFInput) ([]byte, error)
}

// AuditEmitter는 reporting 도메인 변경을 감사 체인에 기록 (P5 격리).
//
// 본 인터페이스는 cmd/* bootstrap이 audit.Service 어댑터로 주입 — reporting 패키지
// 자체는 audit 패키지를 import하지 않습니다.
type AuditEmitter interface {
	EmitReportGenerated(ctx context.Context, tx storage.Tx, r Report) error
	EmitReportSigned(ctx context.Context, tx storage.Tx, r Report) error
	// E18 Phase 2 — Framework 리포트 생성·서명 audit emit.
	EmitFrameworkReportGenerated(ctx context.Context, tx storage.Tx, r FrameworkReport) error
	EmitFrameworkReportSigned(ctx context.Context, tx storage.Tx, r FrameworkReport) error
}

// FrameworkReport는 ComplianceProfile/FrameworkSnapshot 기반 PDF의 메타입니다 (E18 Phase 2).
//
// 별도 타입(Report와 분리)인 이유:
//   - profile/snapshot ID는 framework 전용 — Report.SessionID 자리에 넣으면 의미 혼동
//   - 향후 프레임워크별 메타(framework·version) 첨가 가능 (현재 ProfileID/SnapshotID로 lookup)
//   - 별도 영속 테이블(framework_reports, 마이그레이션 0016)
type FrameworkReport struct {
	ID           string
	TenantID     storage.TenantID
	ProfileID    string
	SnapshotID   string
	PDFSHA256    string
	PDFSizeBytes int64
	PDF          []byte // 본문 (Read 시에만 채움; List는 nil)
	GeneratedAt  time.Time
	GeneratedBy  string
	Signature    ReportSignature
}

// FrameworkComplianceView는 ComplianceReader가 반환하는 minimal DTO 묶음입니다.
//
// reporting 도메인이 compliance 패키지를 직접 import하지 않도록 (P5),
// 필요한 필드만 격리 사본으로 정의.
type FrameworkComplianceView struct {
	Profile  FrameworkProfileView
	Snapshot FrameworkSnapshotView
}

// FrameworkProfileView는 PDF 표시에 필요한 profile 메타입니다.
type FrameworkProfileView struct {
	ID               string
	Framework        string // "isms-p" 등
	FrameworkVersion string
}

// FrameworkSnapshotView는 PDF에 표시할 snapshot 데이터입니다.
type FrameworkSnapshotView struct {
	ID                 string
	OverallScore       float64
	PassCount          int
	FailCount          int
	PartialCount       int
	NotApplicableCount int
	UnmappedCount      int
	ChainHeadSeq       int64
	ChainHeadHash      string
	CreatedAt          time.Time
	Statuses           []FrameworkControlStatusView
}

// FrameworkControlStatusView는 한 통제의 상태 + 표시용 메타입니다.
type FrameworkControlStatusView struct {
	ControlID string
	Title     string // ControlDefinition.Title (없으면 빈 문자열)
	Status    string // "pass"|"fail"|"partial"|"not_applicable"|"unmapped"
	PassCount int
	FailCount int
	Notes     string
}

// ComplianceReader는 reporting이 필요한 compliance 도메인 read-only 표면입니다 (P5).
//
// bootstrap이 compliance.Service + framework YAML 로더를 어댑팅해 주입.
type ComplianceReader interface {
	// LoadProfileSnapshot은 profileID·snapshotID로 PDF 입력을 조립합니다.
	// 둘 다 호출 tenant 소유여야 하며, 아니면 ErrFrameworkSnapshotNotFound.
	LoadProfileSnapshot(ctx context.Context, tx storage.Tx, profileID, snapshotID string) (FrameworkComplianceView, error)
}

// FrameworkPDFInput은 FrameworkContentBuilder에 전달되는 결정적 입력입니다.
//
// 모든 시간은 UTC, 모든 list는 안정 정렬(ControlID 알파벳순).
type FrameworkPDFInput struct {
	TenantID         string
	TenantName       string
	ProfileID        string
	Framework        string // "isms-p"
	FrameworkVersion string
	SnapshotID       string
	OverallScore     float64
	Stats            FrameworkPDFStats
	Controls         []FrameworkPDFControlRow
	GeneratedAt      time.Time
	GeneratedBy      string
	AuditAnchor      PDFAuditAnchor
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

// FrameworkContentBuilder는 FrameworkPDFInput → 결정적 PDF bytes 변환자입니다.
//
// pdf 패키지가 구현. 같은 입력 → byte-identical PDF (R10-5 결정성 정책 동일).
type FrameworkContentBuilder interface {
	BuildFramework(input FrameworkPDFInput) ([]byte, error)
}

// GenerateRequest는 Service.Generate 입력입니다.
//
// Phase 1은 ScopeSession 강제 — SessionID 필수.
// GeneratedAt이 zero이면 Service 내부 Clock.Now()로 채움. 결정성 보장 시(테스트·재생성)
// 호출자가 명시 시각을 주입.
type GenerateRequest struct {
	TenantID    storage.TenantID
	SessionID   string // ScopeSession 강제 (Phase 1)
	TemplateID  string // 빈 값이면 "default"
	GeneratedBy string // userID 또는 "system"
	GeneratedAt time.Time
}

// Service는 reporting 도메인 진입점입니다 (E8 Stage A).
//
// Generate는 PDF 본문까지만 만들고 서명은 별도 Sign 단계 — R10-7 키 분리 운영을 자연스럽게.
type Service interface {
	// Generate는 SessionID로부터 PDFInput을 조립 → ContentBuilder.Build → reports INSERT.
	// 본 Service는 **서명을 하지 않음** — Sign이 별도 단계(Stage C가 결선).
	// 반환 Report는 PDF=Build 결과, Signature는 zero값.
	Generate(ctx context.Context, tx storage.Tx, req GenerateRequest) (Report, error)

	// Sign은 이미 Generate된 Report에 Ed25519 서명을 부착합니다.
	// Signer는 호출자가 주입(서명 키는 audit checkpoint와 분리 — R10-7).
	// chainHead는 서명 시점 audit chain의 (seq, hash) snapshot — 호출자가 audit.Service로 조회.
	Sign(ctx context.Context, tx storage.Tx, reportID string, signerKeyID string, sigBytes []byte,
		chainHeadSeq int64, chainHeadHash string, signedAt time.Time) (Report, error)

	// GetReport는 ID로 메타 + PDF body 반환.
	GetReport(ctx context.Context, tx storage.Tx, reportID string) (Report, error)

	// ListReports는 tenant 내 리포트 메타(PDF nil)를 generated_at DESC로 반환.
	ListReports(ctx context.Context, tx storage.Tx, filter ListFilter) ([]Report, error)

	// E18 Phase 2 — Framework 리포트 4 메서드.

	// GenerateFramework는 (profileID, snapshotID)로 FrameworkPDFInput을 조립 →
	// FrameworkContentBuilder.BuildFramework → framework_reports INSERT + audit emit.
	// 서명은 별도 SignFramework 단계.
	GenerateFramework(ctx context.Context, tx storage.Tx, req GenerateFrameworkRequest) (FrameworkReport, error)

	// SignFramework는 GenerateFramework된 보고서에 Ed25519 서명을 부착합니다.
	SignFramework(ctx context.Context, tx storage.Tx, reportID, signerKeyID string, sigBytes []byte,
		chainHeadSeq int64, chainHeadHash string, signedAt time.Time) (FrameworkReport, error)

	// GetFrameworkReport는 ID로 메타 + PDF body 반환.
	GetFrameworkReport(ctx context.Context, tx storage.Tx, reportID string) (FrameworkReport, error)

	// ListFrameworkReports는 tenant 또는 profile 내 보고서 메타(PDF nil)를 generated_at DESC로 반환.
	ListFrameworkReports(ctx context.Context, tx storage.Tx, filter FrameworkListFilter) ([]FrameworkReport, error)
}

// GenerateFrameworkRequest는 Service.GenerateFramework 입력입니다.
type GenerateFrameworkRequest struct {
	TenantID    storage.TenantID
	ProfileID   string
	SnapshotID  string
	GeneratedBy string
	GeneratedAt time.Time // zero이면 Service 내부 Clock.Now() 사용
}

// FrameworkListFilter는 ListFrameworkReports 필터입니다.
type FrameworkListFilter struct {
	ProfileID string // 옵션 — 빈 값이면 모든 profile
	Limit     int    // 0이면 50
}

// ListFilter는 ListReports 필터입니다.
type ListFilter struct {
	SessionID string // 옵션 — 빈 값이면 모든 session
	Limit     int    // 0이면 50
}

// Ed25519SignatureSize는 외부에서 검증 시 사용할 수 있도록 export합니다.
const Ed25519SignatureSize = ed25519SignatureSize

// 공통 에러 sentinel.
var (
	ErrSessionMissing      = errors.New("reporting: scope=session requires SessionID")
	ErrSessionNotFound     = errors.New("reporting: session not found")
	ErrSessionNotCompleted = errors.New("reporting: session must be completed before report")
	ErrReportNotFound      = errors.New("reporting: report not found")
	ErrAlreadySigned       = errors.New("reporting: report already signed")
	ErrInvalidSignature    = errors.New("reporting: invalid signature size (must be 64 bytes Ed25519)")
	ErrBuilderNil          = errors.New("reporting: ContentBuilder not configured")

	// E18 Phase 2 — Framework 리포트 sentinel.
	ErrFrameworkProfileMissing      = errors.New("reporting: ProfileID is required")
	ErrFrameworkSnapshotMissing     = errors.New("reporting: SnapshotID is required")
	ErrFrameworkSnapshotNotFound    = errors.New("reporting: framework snapshot not found")
	ErrFrameworkReportNotFound      = errors.New("reporting: framework report not found")
	ErrFrameworkBuilderNil          = errors.New("reporting: FrameworkContentBuilder not configured")
	ErrFrameworkComplianceReaderNil = errors.New("reporting: ComplianceReader not configured")
)
