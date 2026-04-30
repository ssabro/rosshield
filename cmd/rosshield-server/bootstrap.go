package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ssabro/rosshield/internal/app/insightautorun"
	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	benchmarkrepo "github.com/ssabro/rosshield/internal/domain/benchmark/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/compliance"
	compliancerepo "github.com/ssabro/rosshield/internal/domain/compliance/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/evidence"
	evidencerepo "github.com/ssabro/rosshield/internal/domain/evidence/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/insight"
	insightrepo "github.com/ssabro/rosshield/internal/domain/insight/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/reporting/pdf"
	reportingrepo "github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	robotrepo "github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/scan"
	scanrepo "github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/blobstore"
	blobfs "github.com/ssabro/rosshield/internal/platform/blobstore/fs"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/llm"
	llmanthropic "github.com/ssabro/rosshield/internal/platform/llm/anthropic"
	llmnoop "github.com/ssabro/rosshield/internal/platform/llm/noop"
	llmollama "github.com/ssabro/rosshield/internal/platform/llm/ollama"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
	xssh "golang.org/x/crypto/ssh"
)

// Config는 부트스트랩 입력입니다.
type Config struct {
	DataDir string       // SQLite 파일·키·로그 저장 디렉토리 (예: ~/.rosshield).
	Logger  *slog.Logger // nil이면 stdout JSON 핸들러로 자동 생성.

	// SystemTenantID는 부팅 시 자동 등록되는 audit checkpoint 잡의 테넌트 식별자.
	// 빈 값이면 "system" 사용. 도메인 진입(E3 Tenant) 후에도 시스템 자체 액션은 이 테넌트.
	SystemTenantID storage.TenantID

	// CheckpointSpec은 audit checkpoint 잡의 cron spec.
	// 빈 값이면 "@every 1h" (§10.5 매시간 기본). 테스트에서 `@every 1s` 등으로 단축.
	CheckpointSpec string

	// LLM 옵션 — R14-1 옵트인 (기본값 noop).
	// LLMProvider: "" → noop, "ollama" → Ollama, "anthropic" → Anthropic. 그 외는 부트스트랩 에러.
	// LLMModel·LLMBaseURL·LLMAPIKey·LLMTimeout은 provider별 의미가 다름 (provider 주석 참조).
	LLMProvider string
	LLMModel    string
	LLMBaseURL  string        // ollama daemon URL 또는 anthropic API base
	LLMAPIKey   string        // anthropic 전용
	LLMTimeout  time.Duration // 0이면 어댑터 기본값
}

// Platform은 초기화된 모든 platform 서비스의 묶음입니다.
// 도메인 서비스는 이 구조체에서 필요한 의존성만 주입받습니다 (§03.4 시작 시퀀스).
type Platform struct {
	Logger       *slog.Logger
	Clock        clock.Clock
	IDGen        idgen.IDGen
	Storage      storage.Storage
	EventBus     eventbus.Bus
	Signer       signer.Signer
	Scheduler    scheduler.Scheduler
	Audit        audit.Service
	Tenant       tenant.Service
	Benchmark    benchmark.Service
	Robot        robot.Service
	Scan         scan.Service
	ScanRun      *scanrun.Orchestrator
	Evidence     evidence.Service
	BlobStore    blobstore.Store
	Reporting    reporting.Service
	ReportSigner signer.Signer // R10-7: report 키 ↔ audit checkpoint 키 분리
	Insight      insight.Service
	Compliance   compliance.Service
	LLM          llm.Adapter

	systemTenant storage.TenantID

	insightAutorunSub eventbus.Subscription // E19 — scan.completed 구독

	shutdownOnce sync.Once
	shutdownErr  error
	shutdown     bool
}

// auditEmitterAdapter는 audit.Service를 tenant.AuditEmitter로 감쌉니다.
//
// tenant 도메인이 audit 패키지를 직접 import하지 않도록 하기 위한 결선 글루(P5).
// 새 도메인이 audit를 emit해야 하면 같은 패턴으로 어댑터 추가.
type auditEmitterAdapter struct {
	svc audit.Service
}

func (a *auditEmitterAdapter) EmitTenantCreated(ctx context.Context, tx storage.Tx, t tenant.Tenant, admin tenant.User) error {
	payload := fmt.Sprintf(`{"tenantId":%q,"name":%q,"plan":%q,"adminEmail":%q}`,
		string(t.ID), t.Name, string(t.Plan), admin.Email)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: t.ID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "tenant.created",
		Target:   audit.Target{Type: "tenant", ID: string(t.ID)},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitPackInstalled는 benchmark.AuditEmitter 구현 (P5 격리 — benchmark가 audit 직접 import 안 함).
func (a *auditEmitterAdapter) EmitPackInstalled(ctx context.Context, tx storage.Tx, p benchmark.Pack, actorID string) error {
	payload := fmt.Sprintf(`{"packId":%q,"packKey":%q,"vendor":%q,"version":%q}`,
		p.ID, p.PackKey, p.Vendor, p.Version)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: p.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: actorID},
		Action:   "pack.installed",
		Target:   audit.Target{Type: "pack", ID: p.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitPackLifecycleChanged는 pack.lifecycle.<state> 이벤트를 audit에 emit합니다.
func (a *auditEmitterAdapter) EmitPackLifecycleChanged(ctx context.Context, tx storage.Tx, packID string, from, to benchmark.State, actorID, reason string) error {
	payload := fmt.Sprintf(`{"packId":%q,"from":%q,"to":%q,"reason":%q}`,
		packID, string(from), string(to), reason)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: tx.TenantID(),
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: actorID},
		Action:   "pack.lifecycle." + string(to),
		Target:   audit.Target{Type: "pack", ID: packID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitFleetCreated는 robot.AuditEmitter 구현 (P5 격리 — robot이 audit 직접 import 안 함).
func (a *auditEmitterAdapter) EmitFleetCreated(ctx context.Context, tx storage.Tx, f robot.Fleet) error {
	payload := fmt.Sprintf(`{"fleetId":%q,"name":%q}`, f.ID, f.Name)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: f.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "fleet.created",
		Target:   audit.Target{Type: "fleet", ID: f.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitRobotCreated는 robot.created 이벤트를 audit에 emit합니다 (Stage C).
func (a *auditEmitterAdapter) EmitRobotCreated(ctx context.Context, tx storage.Tx, r robot.Robot, credentialID string) error {
	payload := fmt.Sprintf(`{"robotId":%q,"name":%q,"fleetId":%q,"host":%q,"credentialId":%q}`,
		r.ID, r.Name, r.FleetID, r.Host, credentialID)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: r.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "robot.created",
		Target:   audit.Target{Type: "robot", ID: r.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitRobotDeleted는 robot.deleted 이벤트를 audit에 emit합니다 (Stage C, soft delete).
func (a *auditEmitterAdapter) EmitRobotDeleted(ctx context.Context, tx storage.Tx, robotID string, tenantID storage.TenantID) error {
	payload := fmt.Sprintf(`{"robotId":%q}`, robotID)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: tenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "robot.deleted",
		Target:   audit.Target{Type: "robot", ID: robotID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitCredentialRotated는 credential.rotated 이벤트를 audit에 emit합니다 (Stage C, R3-3).
func (a *auditEmitterAdapter) EmitCredentialRotated(ctx context.Context, tx storage.Tx, robotID, oldCredID, newCredID string, tenantID storage.TenantID) error {
	payload := fmt.Sprintf(`{"robotId":%q,"oldCredentialId":%q,"newCredentialId":%q}`,
		robotID, oldCredID, newCredID)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: tenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "credential.rotated",
		Target:   audit.Target{Type: "robot", ID: robotID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitScanStarted는 scan.AuditEmitter 구현 (E6 Stage C — pending → running 전이 시점).
func (a *auditEmitterAdapter) EmitScanStarted(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	payload := fmt.Sprintf(`{"sessionId":%q,"fleetId":%q,"packId":%q,"trigger":%q,"total":%d}`,
		s.ID, s.FleetID, s.PackID, string(s.Trigger), s.Progress.Total)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "scan.started",
		Target:   audit.Target{Type: "scan_session", ID: s.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitScanCompleted는 running → completed 전이 시점 audit 엔트리입니다.
func (a *auditEmitterAdapter) EmitScanCompleted(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	payload := fmt.Sprintf(`{"sessionId":%q,"completed":%d,"failed":%d}`,
		s.ID, s.Progress.Completed, s.Progress.Failed)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "scan.completed",
		Target:   audit.Target{Type: "scan_session", ID: s.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitScanFailed는 (pending|running) → failed 전이 시점 audit 엔트리입니다.
func (a *auditEmitterAdapter) EmitScanFailed(ctx context.Context, tx storage.Tx, s scan.ScanSession, reason string) error {
	payload := fmt.Sprintf(`{"sessionId":%q,"reason":%q}`, s.ID, reason)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "scan.failed",
		Target:   audit.Target{Type: "scan_session", ID: s.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeFailure,
	})
	return err
}

// EmitScanCancelled는 (pending|running) → cancelled 전이 시점 audit 엔트리입니다 (R5-5).
func (a *auditEmitterAdapter) EmitScanCancelled(ctx context.Context, tx storage.Tx, s scan.ScanSession, reason string) error {
	payload := fmt.Sprintf(`{"sessionId":%q,"reason":%q}`, s.ID, reason)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "scan.cancelled",
		Target:   audit.Target{Type: "scan_session", ID: s.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitEvidenceStored는 evidence.AuditEmitter 구현 (E7 Stage C — 신규 evidence INSERT 시점).
// dedup 히트는 emit하지 않음(이미 chain에 있음).
func (a *auditEmitterAdapter) EmitEvidenceStored(ctx context.Context, tx storage.Tx, rec evidence.Record) error {
	payload := fmt.Sprintf(`{"sha256":%q,"contentType":%q,"sizeBytes":%d}`,
		rec.SHA256, string(rec.ContentType), rec.SizeBytes)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: rec.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "evidence.stored",
		Target:   audit.Target{Type: "evidence", ID: rec.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitReportGenerated는 reporting.AuditEmitter 구현 (E8 Stage A — Generate 후).
// 서명 전 시점 — Sign 이전 통계와 PDF 본문 sha256만 기록.
func (a *auditEmitterAdapter) EmitReportGenerated(ctx context.Context, tx storage.Tx, r reporting.Report) error {
	payload := fmt.Sprintf(`{"sessionId":%q,"pdfSha256":%q,"sizeBytes":%d,"generatedBy":%q}`,
		r.SessionID, r.PDFSHA256, r.PDFSizeBytes, r.GeneratedBy)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: r.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: r.GeneratedBy},
		Action:   "reporting.generate",
		Target:   audit.Target{Type: "report", ID: r.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitFrameworkReportGenerated는 reporting.AuditEmitter 구현 (E18 — Framework Generate 후).
func (a *auditEmitterAdapter) EmitFrameworkReportGenerated(ctx context.Context, tx storage.Tx, r reporting.FrameworkReport) error {
	payload := fmt.Sprintf(`{"profileId":%q,"snapshotId":%q,"pdfSha256":%q,"sizeBytes":%d,"generatedBy":%q}`,
		r.ProfileID, r.SnapshotID, r.PDFSHA256, r.PDFSizeBytes, r.GeneratedBy)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: r.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: r.GeneratedBy},
		Action:   "reporting.framework.generate",
		Target:   audit.Target{Type: "framework_report", ID: r.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitFrameworkReportSigned는 reporting.AuditEmitter 구현 (E18 — Framework Sign 후).
func (a *auditEmitterAdapter) EmitFrameworkReportSigned(ctx context.Context, tx storage.Tx, r reporting.FrameworkReport) error {
	payload := fmt.Sprintf(`{"signerKeyId":%q,"chainHeadSeq":%d,"chainHeadHash":%q}`,
		r.Signature.SignerKeyID, r.Signature.ChainHeadSeq, r.Signature.ChainHeadHash)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: r.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "reporting.framework.sign",
		Target:   audit.Target{Type: "framework_report", ID: r.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitReportSigned는 reporting.AuditEmitter 구현 (E8 Stage A — Sign 후).
// signer keyId + chain head anchor를 audit에 박아 향후 cross-check.
func (a *auditEmitterAdapter) EmitReportSigned(ctx context.Context, tx storage.Tx, r reporting.Report) error {
	payload := fmt.Sprintf(`{"signerKeyId":%q,"chainHeadSeq":%d,"chainHeadHash":%q}`,
		r.Signature.SignerKeyID, r.Signature.ChainHeadSeq, r.Signature.ChainHeadHash)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: r.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "reporting.sign",
		Target:   audit.Target{Type: "report", ID: r.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitInsightCreated는 insight.AuditEmitter 구현 (E14·E16 — RunForFleet INSERT마다).
func (a *auditEmitterAdapter) EmitInsightCreated(ctx context.Context, tx storage.Tx, in insight.Insight) error {
	payload := fmt.Sprintf(`{"insightId":%q,"kind":%q,"severity":%q,"summary":%q,"producedBy":%q}`,
		in.ID, string(in.Kind), string(in.Severity), in.Summary, string(in.ProducedBy))
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: in.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "insight.created",
		Target:   audit.Target{Type: "insight", ID: in.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitInsightDismissed는 insight.AuditEmitter 구현 (Dismiss 시점, reason 포함).
func (a *auditEmitterAdapter) EmitInsightDismissed(ctx context.Context, tx storage.Tx, in insight.Insight, reason string) error {
	payload := fmt.Sprintf(`{"insightId":%q,"kind":%q,"dismissedBy":%q,"reason":%q}`,
		in.ID, string(in.Kind), in.DismissedBy, reason)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: in.TenantID,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: in.DismissedBy},
		Action:   "insight.dismissed",
		Target:   audit.Target{Type: "insight", ID: in.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitProfileCreated는 compliance.AuditEmitter 구현 (E15·E16 — CreateProfile 시점).
func (a *auditEmitterAdapter) EmitProfileCreated(ctx context.Context, tx storage.Tx, p compliance.ComplianceProfile) error {
	payload := fmt.Sprintf(`{"profileId":%q,"framework":%q,"frameworkVersion":%q,"enabled":%t}`,
		p.ID, string(p.Framework), p.FrameworkVersion, p.Enabled)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: p.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "compliance.profile.created",
		Target:   audit.Target{Type: "compliance_profile", ID: p.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitSnapshotGenerated는 compliance.AuditEmitter 구현 (GenerateSnapshot 시점).
// chain anchor (head_seq, head_hash)는 snapshot 자체에 포함되어 있어 payload에 그대로 직렬화.
func (a *auditEmitterAdapter) EmitSnapshotGenerated(ctx context.Context, tx storage.Tx, s compliance.FrameworkSnapshot) error {
	payload := fmt.Sprintf(`{"snapshotId":%q,"profileId":%q,"sessionId":%q,"score":%g,"chainHeadSeq":%d,"chainHeadHash":%q}`,
		s.ID, s.ProfileID, s.SessionID, s.OverallScore, s.ChainHeadSeq, s.ChainHeadHash)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "compliance.snapshot.generated",
		Target:   audit.Target{Type: "compliance_snapshot", ID: s.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// insightScanAdapter는 scan.Service를 insight.ScanReader로 어댑팅합니다 (P5 — insight가 scan import 안 함).
//
// ListRecentSessions: scan.ListSessions(filter{FleetID, Status=completed}) → completed_at DESC 정렬,
// limit 적용. scan은 created_at DESC를 반환하지만 completed 세션은 created_at과 completed_at의
// 상대 순서가 거의 일치하므로(StartScan→Transition 갭 작음) 추가 정렬 없이 그대로 사용.
type insightScanAdapter struct {
	svc scan.Service
}

func (a *insightScanAdapter) ListRecentSessions(ctx context.Context, tx storage.Tx, fleetID string, limit int) ([]insight.ScanSessionView, error) {
	sessions, err := a.svc.ListSessions(ctx, tx, scan.ListSessionsFilter{
		FleetID: fleetID,
		Status:  scan.StatusCompleted,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]insight.ScanSessionView, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, insight.ScanSessionView{
			ID:          s.ID,
			TenantID:    s.TenantID,
			FleetID:     s.FleetID,
			Status:      string(s.Status),
			CompletedAt: s.CompletedAt,
		})
	}
	return out, nil
}

func (a *insightScanAdapter) ListResultsForSession(ctx context.Context, tx storage.Tx, sessionID string) ([]insight.ScanResultView, error) {
	results, err := a.svc.ListResults(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]insight.ScanResultView, 0, len(results))
	for _, r := range results {
		out = append(out, insight.ScanResultView{
			ID:         r.ID,
			SessionID:  r.SessionID,
			RobotID:    r.RobotID,
			CheckID:    r.CheckID,
			Outcome:    string(r.Outcome),
			DurationMs: r.DurationMs,
		})
	}
	return out, nil
}

// complianceScanAdapter는 scan.Service를 compliance.ScanReader로 어댑팅합니다 (P5).
type complianceScanAdapter struct {
	svc scan.Service
}

func (a *complianceScanAdapter) ListResultsForSession(ctx context.Context, tx storage.Tx, sessionID string) ([]compliance.ScanResultView, error) {
	results, err := a.svc.ListResults(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]compliance.ScanResultView, 0, len(results))
	for _, r := range results {
		out = append(out, compliance.ScanResultView{
			CheckID: r.CheckID,
			Outcome: string(r.Outcome),
		})
	}
	return out, nil
}

// complianceAuditReaderAdapter는 audit.Service를 compliance.AuditReader로 어댑팅합니다 (P5).
// audit.ChainHead.Hash는 [32]byte → lowercase hex (compliance 격리 사본).
type complianceAuditReaderAdapter struct {
	svc audit.Service
}

func (a *complianceAuditReaderAdapter) Head(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (compliance.HeadView, error) {
	head, err := a.svc.Head(ctx, tx, tenantID)
	if err != nil {
		return compliance.HeadView{}, err
	}
	return compliance.HeadView{
		Seq:  head.Seq,
		Hash: hex.EncodeToString(head.Hash[:]),
	}, nil
}

// systemTenantID는 부팅 시 결정된 시스템 테넌트를 반환합니다 (healthz·system audit job용).
func (p *Platform) systemTenantID() storage.TenantID {
	return p.systemTenant
}

// Bootstrap은 §03.4 시작 시퀀스에 따라 모든 platform 서비스를 초기화합니다.
// 실패 시 이미 초기화된 자원을 역순으로 정리한 뒤 에러를 반환합니다 (fail-fast).
func Bootstrap(ctx context.Context, cfg Config) (*Platform, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("bootstrap: DataDir is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("bootstrap: mkdir %q: %w", cfg.DataDir, err)
	}

	clk := clock.System()
	ids := idgen.NewULID()

	dbPath := filepath.Join(cfg.DataDir, "data.db")
	store, err := sqlite.Open(storage.Config{
		Driver: "sqlite",
		DSN:    dbPath,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap: open storage: %w", err)
	}

	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: migrate: %w", err)
	}

	bus := inproc.New(inproc.Deps{Logger: logger, Clock: clk, IDGen: ids})

	keyPath := filepath.Join(cfg.DataDir, "keys", "platform.ed25519")
	sgn, err := soft.LoadOrCreate(keyPath)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: signer: %w", err)
	}

	// JWT 별도 키 — audit checkpoint 키와 분리(B4 결정).
	// 키 회전 주기·키 손실 영향이 다르므로 결선 단계에서 두 개 별도 키.
	// jwt 라이브러리(`golang-jwt/jwt/v5`)는 raw ed25519.PrivateKey/PublicKey를 요구하므로 LoadOrCreatePrivateKey 사용.
	jwtKeyPath := filepath.Join(cfg.DataDir, "keys", "jwt.ed25519")
	jwtPrivateKey, err := soft.LoadOrCreatePrivateKey(jwtKeyPath)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: jwt key: %w", err)
	}
	jwtPublicKey := jwtPrivateKey.Public().(ed25519.PublicKey)

	sch := cronsched.New(cronsched.Deps{Logger: logger})

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})

	emitter := &auditEmitterAdapter{svc: auditSvc}

	tenantSvc := tenantrepo.New(tenantrepo.Deps{
		Clock:         clk,
		IDGen:         ids,
		Audit:         emitter,
		JWTPrivateKey: jwtPrivateKey,
		JWTPublicKey:  jwtPublicKey,
		// AccessTTL/RefreshTTL는 0 → tenant.DefaultAccessTTL/DefaultRefreshTTL.
	})

	benchmarkSvc := benchmarkrepo.New(benchmarkrepo.Deps{
		Clock:              clk,
		IDGen:              ids,
		Audit:              emitter,
		DefaultSignerKeyID: sgn.KeyID(), // audit checkpoint와 같은 키로 pack 서명한다고 가정
	})

	kekPath := filepath.Join(cfg.DataDir, "keys", "credential.kek")
	kek, err := robot.LoadOrCreateKEK(kekPath)
	if err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: KEK: %w", err)
	}

	robotSvc := robotrepo.New(robotrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: emitter,
		KEK:   kek,
		// SSHTester는 E6 sshpool 결선 시 주입 — Phase 1 E5는 nil (TestConnection 호출 시 ErrSSHTesterNotConfigured).
		SSHTester: nil,
	})

	scanSvc := scanrepo.New(scanrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: emitter,
	})

	// E7 Stage C — Evidence 도메인 결선 (R9-1 fs blobstore, R9-8 tenant scope).
	blobRoot := filepath.Join(cfg.DataDir, "evidence")
	bs, err := blobfs.New(blobRoot)
	if err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: blobstore: %w", err)
	}
	evidenceSvc := evidencerepo.New(evidencerepo.Deps{
		Clock:     clk,
		IDGen:     ids,
		Audit:     emitter,
		BlobStore: bs,
	})

	// E15 Compliance 도메인 결선 — reporting 결선 전에 만들어 framework 어댑터를 reporting Deps에 주입 (E18).
	complianceSvc := compliancerepo.New(compliancerepo.Deps{
		Clock:       clk,
		IDGen:       ids,
		Audit:       emitter,
		ScanReader:  &complianceScanAdapter{svc: scanSvc},
		AuditReader: &complianceAuditReaderAdapter{svc: auditSvc},
	})

	// E8 Stage D — Reporting 도메인 결선 (R10-1 signintech/gopdf, R10-7 키 분리).
	// Report signer는 audit checkpoint signer와 별도 키 파일(역할 격리·키 회전 분리).
	reportKeyPath := filepath.Join(cfg.DataDir, "keys", "report.ed25519")
	reportSigner, err := soft.LoadOrCreate(reportKeyPath)
	if err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: report signer: %w", err)
	}
	reportPDFBuilder := pdf.New()
	reportingSvc := reportingrepo.New(reportingrepo.Deps{
		Clock:            clk,
		IDGen:            ids,
		Audit:            emitter,
		Builder:          &pdfBuilderAdapter{inner: reportPDFBuilder},
		Scan:             &reportingScanAdapter{svc: scanSvc},
		Evidence:         &reportingEvidenceAdapter{svc: evidenceSvc},
		Tenant:           &reportingTenantAdapter{svc: tenantSvc},
		FrameworkBuilder: &frameworkPdfBuilderAdapter{inner: reportPDFBuilder}, // E18
		Compliance:       &complianceReaderAdapter{svc: complianceSvc},         // E18
		// PackReader/RobotReader는 Phase 1 미주입 — 표시 메타는 빈 string으로 노출.
	})

	// E6 Stage D.2 — scan Orchestrator 결선 (R6-1~R6-8) + E7 evidence 결선.
	// host key callback은 임시 InsecureIgnoreHostKey + warning 로그.
	// R4-2 first-touch trust + DB 기록은 후속 stage(D.3 또는 별도)에서.
	logger.Warn("ScanRun host-key check disabled (Phase 1 placeholder) — R4-2 first-touch trust pending",
		"todo", "implement known_hosts file + first-touch DB record")
	sshExec := sshpool.New(sshpool.Deps{Logger: logger})
	scanRun := scanrun.New(scanrun.Deps{
		Scan:    scanSvc,
		Storage: store,
		Executor: &sshExecutorAdapter{
			pool:      sshExec,
			robot:     robotSvc,
			storage:   store,
			hostKeyCB: xssh.InsecureIgnoreHostKey(), //nolint:gosec // Phase 1 placeholder; R4-2 후속 결선
			logger:    logger,
		},
		Evaluator: &benchmarkEvaluatorAdapter{},
		Bus:       bus,
		Clock:     clk,
		Evidence:  evidenceSvc,
		// WorkerLimit은 default(R4-4 — 10).
	})

	// E16 — LLM 어댑터 결선 (R14-1 옵트인, 기본값 noop).
	llmAdapter, err := buildLLMAdapter(cfg)
	if err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: llm: %w", err)
	}

	// E16 — Insight 도메인 결선 (E14 + scan/audit 어댑터 주입).
	insightSvc := insightrepo.New(insightrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: emitter,
		Scan:  &insightScanAdapter{svc: scanSvc},
	})

	// (Compliance 도메인은 E18 결선 순서 변경으로 reporting 위에서 만듦)

	// E19 — scan.completed 이벤트 구독 → Insight.RunForFleet 자동 호출.
	insightAutorun := insightautorun.New(insightautorun.Deps{
		Logger:  logger,
		Storage: store,
		Scan:    scanSvc,
		Insight: insightSvc,
	})
	insightAutorunSub := insightAutorun.Start(ctx, bus)

	systemTenant := cfg.SystemTenantID
	if systemTenant == "" {
		systemTenant = "system"
	}
	checkpointSpec := cfg.CheckpointSpec
	if checkpointSpec == "" {
		checkpointSpec = "@every 1h"
	}
	if err := audit.RegisterCheckpointJob(sch, store, auditSvc, logger,
		"audit-checkpoint-system", checkpointSpec, systemTenant, sgn); err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: register checkpoint job: %w", err)
	}

	logger.Info("platform bootstrap complete",
		"dataDir", cfg.DataDir,
		"dbPath", dbPath,
		"keyPath", keyPath,
		"signerKeyId", sgn.KeyID(),
		"kekKeyId", kek.KeyID(),
		"blobRoot", blobRoot,
		"reportSignerKeyId", reportSigner.KeyID(),
		"systemTenant", string(systemTenant),
		"checkpointSpec", checkpointSpec,
		"llmProvider", llmAdapter.Provider())

	return &Platform{
		Logger:            logger,
		Clock:             clk,
		IDGen:             ids,
		Storage:           store,
		EventBus:          bus,
		Signer:            sgn,
		Scheduler:         sch,
		Audit:             auditSvc,
		Tenant:            tenantSvc,
		Benchmark:         benchmarkSvc,
		Robot:             robotSvc,
		Scan:              scanSvc,
		ScanRun:           scanRun,
		Evidence:          evidenceSvc,
		BlobStore:         bs,
		Reporting:         reportingSvc,
		ReportSigner:      reportSigner,
		Insight:           insightSvc,
		Compliance:        complianceSvc,
		LLM:               llmAdapter,
		systemTenant:      systemTenant,
		insightAutorunSub: insightAutorunSub,
	}, nil
}

// buildLLMAdapter는 cfg.LLMProvider 기반으로 어댑터 1개를 생성합니다 (R14-1 옵트인).
//
//	"" / "noop"   → noop.New()  — 기본값, ErrLLMDisabled 즉시 반환.
//	"ollama"      → ollama.New(BaseURL, DefaultModel, Timeout)
//	"anthropic"   → anthropic.New(BaseURL, APIKey, DefaultModel, Timeout). APIKey 누락은 에러.
//	그 외          → 에러 (오타 방지).
func buildLLMAdapter(cfg Config) (llm.Adapter, error) {
	switch cfg.LLMProvider {
	case "", "noop":
		return llmnoop.New(), nil
	case "ollama":
		return llmollama.New(llmollama.Options{
			Endpoint:     cfg.LLMBaseURL,
			DefaultModel: cfg.LLMModel,
			HTTPTimeout:  cfg.LLMTimeout,
		}), nil
	case "anthropic":
		if cfg.LLMAPIKey == "" {
			return nil, errors.New("anthropic: LLMAPIKey is required")
		}
		return llmanthropic.New(llmanthropic.Options{
			APIKey:       cfg.LLMAPIKey,
			BaseURL:      cfg.LLMBaseURL,
			DefaultModel: cfg.LLMModel,
			HTTPTimeout:  cfg.LLMTimeout,
		}), nil
	default:
		return nil, fmt.Errorf("unknown LLMProvider %q (allowed: noop|ollama|anthropic)", cfg.LLMProvider)
	}
}

// Shutdown은 platform 서비스를 역순으로 정상 종료합니다 (idempotent).
// InsightAutorun Sub → Scheduler → EventBus → Storage 순. ctx 만료 시 ctx.Err() 반환.
func (p *Platform) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		var errs []error

		// E19 — subscription 먼저 cancel하면 EventBus.Close 시 worker가 깨끗이 종료됨.
		if p.insightAutorunSub != nil {
			p.insightAutorunSub.Cancel()
		}

		if err := p.Scheduler.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("scheduler close: %w", err))
		}
		if err := p.EventBus.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("eventbus close: %w", err))
		}
		if err := p.Storage.Close(); err != nil {
			errs = append(errs, fmt.Errorf("storage close: %w", err))
		}

		p.shutdown = true
		p.shutdownErr = errors.Join(errs...)
		if p.shutdownErr != nil {
			p.Logger.Error("platform shutdown errors", "err", p.shutdownErr.Error())
		} else {
			p.Logger.Info("platform shutdown complete")
		}
	})
	return p.shutdownErr
}

// IsShutdown은 Shutdown이 호출되었는지 반환합니다 (healthz에서 사용).
func (p *Platform) IsShutdown() bool {
	return p.shutdown
}
