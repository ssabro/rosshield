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
	"strings"
	"sync"
	"time"

	"github.com/ssabro/rosshield/internal/app/advisorrun"
	"github.com/ssabro/rosshield/internal/app/insightautorun"
	"github.com/ssabro/rosshield/internal/app/llmmapper"
	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/app/webhookrun"
	"github.com/ssabro/rosshield/internal/domain/advisor"
	advisorrepo "github.com/ssabro/rosshield/internal/domain/advisor/sqliterepo"
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
	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	webhookrepo "github.com/ssabro/rosshield/internal/domain/integration/webhook/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/reporting/pdf"
	reportingrepo "github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	robotrepo "github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/scan"
	scanrepo "github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	ssorepo "github.com/ssabro/rosshield/internal/domain/tenant/sso/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/blobstore"
	blobfs "github.com/ssabro/rosshield/internal/platform/blobstore/fs"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/email"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/ha"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/keystore"
	keystorefile "github.com/ssabro/rosshield/internal/platform/keystore/file"
	keystoretpm "github.com/ssabro/rosshield/internal/platform/keystore/tpm"
	"github.com/ssabro/rosshield/internal/platform/license"
	"github.com/ssabro/rosshield/internal/platform/llm"
	llmanthropic "github.com/ssabro/rosshield/internal/platform/llm/anthropic"
	llmnoop "github.com/ssabro/rosshield/internal/platform/llm/noop"
	llmollama "github.com/ssabro/rosshield/internal/platform/llm/ollama"
	"github.com/ssabro/rosshield/internal/platform/metrics"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
	xssh "golang.org/x/crypto/ssh"
)

// openStorage는 cfg.StorageDriver 기반으로 storage 어댑터를 엽니다 (E22-D).
//
// "" / "sqlite": SQLite (DataDir/data.db).
// "postgres" / "pg": PostgreSQL (StorageDSN 필수).
//
// 두 번째 반환값은 운영자 식별용 path 문자열 (로그용). PG는 host/db.
func openStorage(cfg Config) (storage.Storage, string, error) {
	switch cfg.StorageDriver {
	case "", "sqlite":
		dbPath := filepath.Join(cfg.DataDir, "data.db")
		s, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
		if err != nil {
			return nil, "", err
		}
		return s, dbPath, nil
	case "postgres", "pg":
		if cfg.StorageDSN == "" {
			return nil, "", errors.New("postgres: StorageDSN is required (set --storage-dsn or ROSSHIELD_DATABASE_URL)")
		}
		s, err := postgres.Open(storage.Config{Driver: "postgres", DSN: cfg.StorageDSN})
		if err != nil {
			return nil, "", err
		}
		// DSN 자체는 비밀(패스워드 포함) — 로그에는 driver 라벨만.
		return s, "postgres", nil
	default:
		return nil, "", fmt.Errorf("unknown storage driver %q (allowed: sqlite|postgres)", cfg.StorageDriver)
	}
}

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

	// E24 — License 옵션 (옵트인).
	// LicenseToken: 빈 값이면 community SKU (enterprise feature 모두 비활성).
	// LicensePublicKeyHex: 토큰 검증용 Ed25519 public key (32B hex). 빈 값이면 license 검증 skip.
	// 두 값이 모두 있으면 Verify → Enforcer 결선. 검증 실패 시 부트스트랩 에러.
	LicenseToken        string
	LicensePublicKeyHex string

	// E23-B — Webhook dispatcher tick 주기. 0이면 webhookrun.DefaultTickInterval (30s).
	// 테스트에서 짧게 설정 가능.
	WebhookTickInterval time.Duration

	// E22-D — Storage 드라이버 선택.
	//
	// "" 또는 "sqlite" → SQLite(데스크톱·온프렘 단일 인스턴스).
	// "postgres" 또는 "pg" → PostgreSQL (StorageDSN 필수, SaaS·HA 배포).
	StorageDriver string

	// StorageDSN은 storage 어댑터 DSN.
	//
	// SQLite: 빈 값이면 DataDir/data.db (현 동작 유지).
	// Postgres: postgres://user:pass@host:port/db?sslmode=... 형식. 빈 값이면 부트스트랩 에러.
	StorageDSN string

	// O6 — Email + invite notifier 옵션 (옵트인).
	//
	// EmailProvider: "" 또는 "noop" → NoopSender (stdout JSON, 실 SMTP 호출 X — 기본).
	//                "smtp" → SMTPSender (Host/Port + optional auth).
	// SMTPHost/SMTPPort/SMTPUsername/SMTPPassword/SMTPFrom는 EmailProvider="smtp"일 때만 사용.
	// PublicBaseURL은 invite accept URL 빌드 — 빈 값이면 acceptURL이 빈 문자열로 Notifier에 전달.
	EmailProvider string
	SMTPHost      string
	SMTPPort      int
	SMTPUsername  string
	SMTPPassword  string
	SMTPFrom      string // "rosshield <noreply@example.com>" 또는 단순 주소.
	PublicBaseURL string // 예: "https://app.example.com" (trailing slash 없이).

	// E25 — HA(High Availability) 옵션 (Phase 5, R30-2 = PG advisory lock + leader/follower).
	//
	// HAEnabled = true일 때 PG advisory lock 기반 leader-election 활성. sqlite와 조합 시
	// 부팅 거부(R30-2 부속2). 두 인스턴스 이상이 같은 HALockID로 동시 실행되면 단일 leader 유지.
	//
	// HAEnabled = false (기본)일 때 단일 인스턴스 가정 — leader-election 없이 모든 write 활성.
	HAEnabled           bool
	HALockID            int64         // PG advisory lock ID. 0이면 기본값 12345.
	HAHeartbeatInterval time.Duration // leader heartbeat 주기. 0이면 5초.
	HALeaderID          string        // 본 인스턴스 식별자 ("hostname:pid"). 빈 값이면 자동 생성.
	HAAdvertisedAddr    string        // 다른 인스턴스가 redirect 시 사용할 URL (옵션, Stage 3 사용).

	// E34 — KeyStore 어댑터 선택 (Phase 5 어플라이언스 트랙).
	//
	// "" 또는 "file" → file 어댑터(현재 동작, soft.LoadOrCreatePrivateKey 위임).
	// "tpm" → TPM 2.0 PCR-sealed (Stage 1 placeholder = 즉시 부팅 실패, Stage 2+ 본격 구현).
	//
	// R40-2 결정(2026-05-11): TPM 시뮬레이터 = swtpm. R41 결정 후 본격 구현.
	KeystoreType string
}

// Platform은 초기화된 모든 platform 서비스의 묶음입니다.
// 도메인 서비스는 이 구조체에서 필요한 의존성만 주입받습니다 (§03.4 시작 시퀀스).
type Platform struct {
	Logger            *slog.Logger
	Clock             clock.Clock
	IDGen             idgen.IDGen
	Storage           storage.Storage
	EventBus          eventbus.Bus
	Signer            signer.Signer
	Scheduler         scheduler.Scheduler
	Audit             audit.Service
	Tenant            tenant.Service
	Benchmark         benchmark.Service
	Robot             robot.Service
	Scan              scan.Service
	ScanRun           *scanrun.Orchestrator
	Evidence          evidence.Service
	BlobStore         blobstore.Store
	Reporting         reporting.Service
	ReportSigner      signer.Signer // R10-7: report 키 ↔ audit checkpoint 키 분리
	Insight           insight.Service
	Compliance        compliance.Service
	LLM               llm.Adapter
	Advisor           advisor.Service          // E16
	License           *license.Enforcer        // E24 — Open-core enterprise feature 게이트 + 쿼터
	Webhook           webhook.Service          // E23 — webhook + SIEM 통합 도메인
	WebhookDispatcher *webhookrun.Dispatcher   // E23-B — Process worker
	WebhookBridge     *webhookrun.EventBridge  // E23-D — EventBus → webhook.Enqueue bridge
	SSO               sso.Service              // E20-D — SSO Provider CRUD + IdP 호출
	Invitation        tenant.InvitationService // E21 — 초대·역할 관리
	Metrics           *metrics.Registry        // E27 — Prometheus exposition (옵트인)
	MetricsBridge     *metrics.MetricsBridge   // E27 — EventBus → counter 결선
	HA                *ha.Manager              // E25 — leader-election (HAEnabled 시 non-nil, 아니면 nil)
	Keystore          keystore.KeyStore        // E34 — KeyStore 어댑터 (file 기본, tpm은 Stage 2+)

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

// EmitConversationStarted는 advisor.AuditEmitter 구현 (E16 — StartConversation 시점).
func (a *auditEmitterAdapter) EmitConversationStarted(ctx context.Context, tx storage.Tx, c advisor.Conversation) error {
	payload := fmt.Sprintf(`{"conversationId":%q,"userId":%q,"title":%q}`, c.ID, c.UserID, c.Title)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: c.TenantID,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: c.UserID},
		Action:   "advisor.conversation.started",
		Target:   audit.Target{Type: "advisor_conversation", ID: c.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitToolCalled는 advisor.AuditEmitter 구현 (E16 — 각 tool dispatch 시점).
func (a *auditEmitterAdapter) EmitToolCalled(ctx context.Context, tx storage.Tx, c advisor.ToolCall) error {
	outcome := audit.OutcomeSuccess
	if c.Error != "" {
		outcome = audit.OutcomeFailure
	}
	payload := fmt.Sprintf(`{"toolCallId":%q,"turnId":%q,"toolName":%q,"durationMs":%d,"error":%q}`,
		c.ID, c.TurnID, c.ToolName, c.DurationMs, c.Error)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: c.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "advisor"},
		Action:   "advisor.tool_called",
		Target:   audit.Target{Type: "advisor_tool_call", ID: c.ID},
		Payload:  []byte(payload),
		Outcome:  outcome,
	})
	return err
}

// EmitAdvisorResponded는 advisor.AuditEmitter 구현 (E16 — 최종 assistant 답변 시점).
func (a *auditEmitterAdapter) EmitAdvisorResponded(ctx context.Context, tx storage.Tx, t advisor.Turn) error {
	payload := fmt.Sprintf(`{"turnId":%q,"conversationId":%q,"llmProvider":%q,"llmModel":%q,"inputTokens":%d,"outputTokens":%d,"costUsd":%g}`,
		t.ID, t.ConversationID, t.LLMProvider, t.LLMModel, t.InputTokens, t.OutputTokens, t.CostUSD)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: t.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "advisor"},
		Action:   "advisor.responded",
		Target:   audit.Target{Type: "advisor_turn", ID: t.ID},
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

// EmitSuggestionCreated는 compliance.AuditEmitter 구현 (E17 — SuggestMappings INSERT마다).
func (a *auditEmitterAdapter) EmitSuggestionCreated(ctx context.Context, tx storage.Tx, s compliance.MappingSuggestion) error {
	payload := fmt.Sprintf(`{"suggestionId":%q,"checkCode":%q,"framework":%q,"controlId":%q,"confidence":%g,"producedBy":%q,"llmProvider":%q}`,
		s.ID, s.CheckCode, string(s.Framework), s.ControlID, s.Confidence, string(s.ProducedBy), s.LLMProvider)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "compliance.suggestion.created",
		Target:   audit.Target{Type: "mapping_suggestion", ID: s.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitSuggestionDecided는 compliance.AuditEmitter 구현 (E17 — Confirm/Reject 시점).
func (a *auditEmitterAdapter) EmitSuggestionDecided(ctx context.Context, tx storage.Tx, s compliance.MappingSuggestion) error {
	actorID := s.DecidedBy
	if actorID == "" {
		actorID = "system"
	}
	payload := fmt.Sprintf(`{"suggestionId":%q,"checkCode":%q,"controlId":%q,"status":%q,"decidedBy":%q}`,
		s.ID, s.CheckCode, s.ControlID, string(s.Status), s.DecidedBy)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: actorID},
		Action:   "compliance.suggestion." + string(s.Status),
		Target:   audit.Target{Type: "mapping_suggestion", ID: s.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitInvitationSent는 tenant.InvitationAuditEmitter 구현 (E21 — CreateInvitation 시점).
func (a *auditEmitterAdapter) EmitInvitationSent(ctx context.Context, tx storage.Tx, inv tenant.Invitation) error {
	payload := fmt.Sprintf(`{"invitationId":%q,"email":%q,"roleName":%q,"invitedBy":%q,"expiresAt":%q}`,
		inv.ID, inv.Email, inv.RoleName, inv.InvitedBy, inv.ExpiresAt.Format(time.RFC3339))
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: inv.TenantID,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: inv.InvitedBy},
		Action:   "invitation.sent",
		Target:   audit.Target{Type: "invitation", ID: inv.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitInvitationAccepted는 tenant.InvitationAuditEmitter 구현 (E21 — AcceptInvitation 시점).
func (a *auditEmitterAdapter) EmitInvitationAccepted(ctx context.Context, tx storage.Tx, inv tenant.Invitation, user tenant.User) error {
	payload := fmt.Sprintf(`{"invitationId":%q,"userId":%q,"email":%q,"roleName":%q}`,
		inv.ID, user.ID, user.Email, inv.RoleName)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: inv.TenantID,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: user.ID},
		Action:   "invitation.accepted",
		Target:   audit.Target{Type: "invitation", ID: inv.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// ssoIdentityResolverAdapter는 ssorepo.IdentityResolver 구현입니다 (O5 Phase 4).
//
// 첫 SSO 로그인 시 tenant.Service.ProvisionExternalUser를 호출 — 외부 sub/email로 user 자동 생성.
// 같은 (tenant, email) user가 이미 있으면 link 모드 (role 변경 X).
type ssoIdentityResolverAdapter struct {
	tenantSvc tenant.Service
}

func (a *ssoIdentityResolverAdapter) ResolveOIDCIdentity(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, providerID string, claims sso.IDTokenClaims) (string, error) {
	user, err := a.tenantSvc.ProvisionExternalUser(ctx, tx, tenant.ProvisionExternalUserRequest{
		TenantID:        tenantID,
		Email:           claims.Email,
		DisplayName:     claims.Name,
		AuthProvider:    tenant.AuthProviderOIDC,
		ExternalSubject: claims.Subject,
	})
	if err != nil {
		return "", err
	}
	return user.ID, nil
}

func (a *ssoIdentityResolverAdapter) ResolveSAMLIdentity(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, providerID string, assertion sso.SAMLAssertion) (string, error) {
	user, err := a.tenantSvc.ProvisionExternalUser(ctx, tx, tenant.ProvisionExternalUserRequest{
		TenantID:        tenantID,
		Email:           assertion.Email,
		DisplayName:     assertion.NameID, // SAML은 별 displayName attribute가 있을 수 있지만 본 stage는 단순화.
		AuthProvider:    tenant.AuthProviderSAML,
		ExternalSubject: assertion.NameID,
	})
	if err != nil {
		return "", err
	}
	return user.ID, nil
}

// EmitProviderChanged는 sso.AuditEmitter 구현 (E20-D — Provider CRUD).
// action: "created"|"updated"|"deleted".
func (a *auditEmitterAdapter) EmitProviderChanged(ctx context.Context, tx storage.Tx, p sso.Provider, action string) error {
	payload := fmt.Sprintf(`{"providerId":%q,"type":%q,"name":%q,"enabled":%t}`,
		p.ID, string(p.Type), p.Name, p.Enabled)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: p.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "sso.provider." + action,
		Target:   audit.Target{Type: "sso_provider", ID: p.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitLoginStarted는 sso.AuditEmitter 구현 (E20-D — StartLogin 시점).
func (a *auditEmitterAdapter) EmitLoginStarted(ctx context.Context, tx storage.Tx, attempt sso.LoginAttempt) error {
	payload := fmt.Sprintf(`{"attemptId":%q,"providerId":%q,"expiresAt":%q}`,
		attempt.ID, attempt.ProviderID, attempt.ExpiresAt.Format(time.RFC3339))
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: attempt.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "sso.login.started",
		Target:   audit.Target{Type: "sso_login_attempt", ID: attempt.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitLoginCompleted는 sso.AuditEmitter 구현 (E20-D — CompleteLogin 시점, 성공/실패 양쪽).
// ok=false면 outcome=failure + identity는 빈 값.
func (a *auditEmitterAdapter) EmitLoginCompleted(ctx context.Context, tx storage.Tx, attempt sso.LoginAttempt, identity sso.ExternalIdentity, ok bool) error {
	outcome := audit.OutcomeSuccess
	if !ok {
		outcome = audit.OutcomeFailure
	}
	payload := fmt.Sprintf(`{"attemptId":%q,"providerId":%q,"externalSubject":%q,"email":%q,"userId":%q,"ok":%t}`,
		attempt.ID, attempt.ProviderID, identity.ExternalSubject, identity.Email, identity.UserID, ok)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: attempt.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "sso.login.completed",
		Target:   audit.Target{Type: "sso_login_attempt", ID: attempt.ID},
		Payload:  []byte(payload),
		Outcome:  outcome,
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

	// E25 — sqlite + HAEnabled 조합 거부 (R30-2 부속2 = 부팅 실패).
	// PG advisory lock 동등 기능이 없는 sqlite에서 HA를 켜면 audit chain 손상 위험.
	if cfg.HAEnabled {
		switch cfg.StorageDriver {
		case "", "sqlite":
			return nil, errors.New("bootstrap: --ha-enabled requires --storage=postgres (sqlite has no advisory lock equivalent — single-instance only)")
		case "postgres", "pg":
			// OK
		default:
			return nil, fmt.Errorf("bootstrap: --ha-enabled with unknown storage driver %q", cfg.StorageDriver)
		}
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

	store, dbPath, err := openStorage(cfg)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: open storage: %w", err)
	}

	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: migrate: %w", err)
	}

	bus := inproc.New(inproc.Deps{Logger: logger, Clock: clk, IDGen: ids})

	// E34 — KeyStore 추상 (file = 현재 동작, tpm = Stage 2+ 본격). 동작 차이 0 (file 시).
	ks, err := buildKeystore(cfg)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: keystore: %w", err)
	}

	keyPath := filepath.Join(cfg.DataDir, "keys", "platform.ed25519")
	platformPriv, err := ks.LoadOrCreatePrivateKey(keyPath)
	if err != nil {
		_ = ks.Close()
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: signer: %w", err)
	}
	sgn := soft.WrapPrivateKey(platformPriv)

	// JWT 별도 키 — audit checkpoint 키와 분리(B4 결정).
	// 키 회전 주기·키 손실 영향이 다르므로 결선 단계에서 두 개 별도 키.
	// jwt 라이브러리(`golang-jwt/jwt/v5`)는 raw ed25519.PrivateKey/PublicKey를 요구.
	jwtKeyPath := filepath.Join(cfg.DataDir, "keys", "jwt.ed25519")
	jwtPrivateKey, err := ks.LoadOrCreatePrivateKey(jwtKeyPath)
	if err != nil {
		_ = ks.Close()
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: jwt key: %w", err)
	}
	jwtPublicKey := jwtPrivateKey.Public().(ed25519.PublicKey)

	sch := cronsched.New(cronsched.Deps{Logger: logger})

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})

	emitter := &auditEmitterAdapter{svc: auditSvc}

	// O6 — Email sender + InvitationNotifier 어댑터 결선 (옵트인).
	// EmailProvider="" 또는 "noop"이면 NoopSender, "smtp"이면 SMTPSender.
	emailSender, err := buildEmailSender(cfg, logger)
	if err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: email: %w", err)
	}
	invitationNotifier := &invitationEmailNotifier{sender: emailSender, logger: logger}
	urlBuilder := buildAcceptURLBuilder(cfg.PublicBaseURL)

	tenantRepo := tenantrepo.New(tenantrepo.Deps{
		Clock:                      clk,
		IDGen:                      ids,
		Audit:                      emitter,
		InvitationAudit:            emitter, // E21 — 같은 어댑터가 InvitationAuditEmitter도 구현.
		InvitationNotifier:         invitationNotifier,
		InvitationAcceptURLBuilder: urlBuilder,
		JWTPrivateKey:              jwtPrivateKey,
		JWTPublicKey:               jwtPublicKey,
		// AccessTTL/RefreshTTL는 0 → tenant.DefaultAccessTTL/DefaultRefreshTTL.
	})
	tenantSvc := tenantRepo
	invitationSvc := tenantRepo // E21 — 같은 Repo가 두 인터페이스 모두 만족.

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

	// E16 — LLM 어댑터 결선 (R14-1 옵트인, 기본값 noop). compliance Suggester 주입 전 단계로 위로 이동.
	llmAdapter, err := buildLLMAdapter(cfg)
	if err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: llm: %w", err)
	}

	// E17 — LLMSuggester 결선 (compliance.SuggestMappings에서 사용).
	// noop이어도 결선만 하고, SuggestMappings 호출 시 ErrLLMDisabled가 도메인 sentinel로 매핑.
	llmSuggester := llmmapper.New(llmAdapter, cfg.LLMModel)

	// E15 Compliance 도메인 결선 — reporting 결선 전에 만들어 framework 어댑터를 reporting Deps에 주입 (E18).
	complianceSvc := compliancerepo.New(compliancerepo.Deps{
		Clock:       clk,
		IDGen:       ids,
		Audit:       emitter,
		ScanReader:  &complianceScanAdapter{svc: scanSvc},
		AuditReader: &complianceAuditReaderAdapter{svc: auditSvc},
		Suggester:   llmSuggester, // E17
	})

	// E8 Stage D — Reporting 도메인 결선 (R10-1 signintech/gopdf, R10-7 키 분리).
	// Report signer는 audit checkpoint signer와 별도 키 파일(역할 격리·키 회전 분리).
	reportKeyPath := filepath.Join(cfg.DataDir, "keys", "report.ed25519")
	reportPriv, err := ks.LoadOrCreatePrivateKey(reportKeyPath)
	if err != nil {
		_ = sch.Close(ctx)
		_ = ks.Close()
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: report signer: %w", err)
	}
	reportSigner := soft.WrapPrivateKey(reportPriv)
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

	// (LLM·Compliance는 위에서 결선됨 — E17 Suggester 주입 흐름)

	// E16 — Insight 도메인 결선 (E14 + scan/audit 어댑터 주입).
	insightSvc := insightrepo.New(insightrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: emitter,
		Scan:  &insightScanAdapter{svc: scanSvc},
	})

	// (Compliance 도메인은 E18 결선 순서 변경으로 reporting 위에서 만듦)

	// E16 — Advisor 결선 (옵트인, LLM 어댑터 noop이면 ErrAdvisorDisabled).
	advisorRepoSvc := advisorrepo.New(advisorrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: emitter,
	})
	advisorDispatcher := advisorrun.NewDispatcher(scanSvc, evidenceSvc, clk)
	advisorLLMClient := advisorrun.NewLLMClient(llmAdapter)
	advisorSvc := advisorrun.NewOrchestrator(advisorrun.OrchestratorDeps{
		Repo:         advisorRepoSvc,
		LLM:          advisorLLMClient,
		Dispatcher:   advisorDispatcher,
		DefaultModel: cfg.LLMModel,
	})

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

	// E20-D + E20-C + O5 — SSO 도메인 결선 (Provider CRUD + OIDC + SAML + IdentityResolver).
	// O5(Phase 4): IdentityResolver를 tenant.Service.ProvisionExternalUser로 결선 → SSO 첫 로그인
	// 시 user 자동 생성 + 기본 role(operator) 할당.
	ssoSvc := ssorepo.New(ssorepo.Deps{
		Clock:            clk,
		IDGen:            ids,
		Audit:            emitter,
		OIDC:             sso.NewOIDCClient(),
		SAML:             sso.NewSAMLClient(),
		IdentityResolver: &ssoIdentityResolverAdapter{tenantSvc: tenantSvc},
	})

	// E23 — Webhook 도메인 결선 (sqliterepo 어댑터).
	webhookSvc := webhookrepo.New(webhookrepo.Deps{
		Clock: clk,
		IDGen: ids,
	})

	// E23-B — Webhook dispatcher (Process worker) 결선 + 백그라운드 시작.
	webhookDispatcher := webhookrun.New(webhookrun.Deps{
		Logger:       logger,
		Storage:      store,
		Clock:        clk,
		Webhook:      webhookSvc,
		TickInterval: cfg.WebhookTickInterval,
	})
	go webhookDispatcher.Run(context.Background())

	// E23-D — EventBus → webhook.Enqueue bridge 결선 + 구독 시작.
	// 본 bridge는 scan.completed·insight.created·audit.checkpoint 3종을 구독해
	// webhook.Service.Enqueue로 전달. 실 HTTP 송출은 dispatcher 책임.
	webhookBridge := webhookrun.NewBridge(webhookrun.BridgeDeps{
		Logger:  logger,
		Storage: store,
		Webhook: webhookSvc,
	})
	webhookBridge.Start(ctx, bus)

	// E27 — Prometheus metrics Registry + EventBus bridge 결선.
	// /metrics endpoint mount는 main.go --metrics-addr 옵트인 시점에 별 mux로.
	metricsReg := metrics.New()
	metricsBridge := metrics.NewBridge(logger, metricsReg)
	metricsBridge.Start(ctx, bus)

	// E24 — License 결선 (옵트인). 토큰 + public key 둘 다 있어야 검증 진입.
	// E24-D — UsageReader는 robot/scan/advisor SQL 집계 어댑터 (P5 격리 — license는 도메인 import 안 함).
	licenseUsage := newLicenseUsageAdapter(store, clk)
	licenseEnforcer, licenseEdition, err := buildLicenseEnforcer(cfg, clk, licenseUsage)
	if err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: build license enforcer: %w", err)
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
		"llmProvider", llmAdapter.Provider(),
		"licenseEdition", licenseEdition)

	platform := &Platform{
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
		Advisor:           advisorSvc,
		License:           licenseEnforcer,
		Webhook:           webhookSvc,
		WebhookDispatcher: webhookDispatcher,
		WebhookBridge:     webhookBridge,
		SSO:               ssoSvc,
		Invitation:        invitationSvc,
		Metrics:           metricsReg,
		MetricsBridge:     metricsBridge,
		Keystore:          ks,
		systemTenant:      systemTenant,
		insightAutorunSub: insightAutorunSub,
	}

	// E25 — HA leader-election (R30-2 = PG advisory lock + leader/follower).
	// HAEnabled=true + storage=postgres 조합에서만 결선 (sqlite 거부는 위에서 체크).
	if cfg.HAEnabled {
		haMgr, err := buildHAManager(cfg, store, logger)
		if err != nil {
			_ = platform.Shutdown(ctx)
			return nil, fmt.Errorf("bootstrap: ha manager: %w", err)
		}
		platform.HA = haMgr
		// E25 Stage 2 — audit append/checkpoint leader-gate. Start() 전에 주입해
		// heartbeat goroutine이 promote 콜백으로 진입하기 전부터 follower 상태에서 차단.
		auditSvc.SetRoleProvider(haMgr)
		// E25 Stage 4a — scheduler tick leader-gate. follower는 cron tick silent skip.
		sch.SetRoleProvider(haMgr)
		platform.HA.Start(context.Background())
		logger.Info("ha enabled — leader-election started",
			"lockId", haCfgLockID(cfg),
			"interval", haCfgInterval(cfg),
			"leaderId", haMgr.LeaderID())
	}

	return platform, nil
}

// buildKeystore는 cfg.KeystoreType 기반으로 KeyStore 어댑터를 생성합니다 (E34).
//
// "" / "file" → file 어댑터 (현재 동작, soft.LoadOrCreatePrivateKey 위임)
// "tpm" → TPM 2.0 어댑터 (Stage 1 placeholder — LoadOrCreate 호출 시 ErrTpmNotImplemented)
//
// 후속 stage에서 tpm 어댑터에 PCR selection·EK/SRK 옵션이 추가됨.
func buildKeystore(cfg Config) (keystore.KeyStore, error) {
	switch cfg.KeystoreType {
	case "", "file":
		return keystorefile.New(), nil
	case "tpm":
		return keystoretpm.New(), nil
	default:
		return nil, fmt.Errorf("%w: %q (allowed: file|tpm)", keystore.ErrUnsupportedDriver, cfg.KeystoreType)
	}
}

// buildHAManager는 PG advisory lock 기반 HA Manager를 생성합니다.
// storage가 PG 어댑터가 아니면 에러 (Bootstrap 진입 가드와 중복이지만 안전).
func buildHAManager(cfg Config, store storage.Storage, logger *slog.Logger) (*ha.Manager, error) {
	pg, ok := store.(*postgres.Postgres)
	if !ok {
		return nil, errors.New("ha requires postgres storage")
	}
	lockID := haCfgLockID(cfg)
	interval := haCfgInterval(cfg)
	leaderID := cfg.HALeaderID
	if leaderID == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown-host"
		}
		leaderID = fmt.Sprintf("%s:%d", host, os.Getpid())
	}
	pgLock := ha.NewPGLock(pg.Pool(), lockID)
	return ha.NewManager(pgLock, leaderID, interval, &slogHALogger{l: logger}), nil
}

func haCfgLockID(cfg Config) int64 {
	if cfg.HALockID == 0 {
		return 12345
	}
	return cfg.HALockID
}

func haCfgInterval(cfg Config) time.Duration {
	if cfg.HAHeartbeatInterval <= 0 {
		return 5 * time.Second
	}
	return cfg.HAHeartbeatInterval
}

// slogHALogger는 *slog.Logger를 ha.Logger interface로 어댑팅합니다.
// 도메인 경계: ha 패키지가 platform/logger를 import하지 않게 하기 위한 결선 글루.
type slogHALogger struct{ l *slog.Logger }

func (s *slogHALogger) Info(msg string, args ...any)  { s.l.Info(msg, args...) }
func (s *slogHALogger) Warn(msg string, args ...any)  { s.l.Warn(msg, args...) }
func (s *slogHALogger) Error(msg string, args ...any) { s.l.Error(msg, args...) }

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
// WebhookDispatcher Stop → InsightAutorun Sub → Scheduler → EventBus → Storage 순.
// ctx 만료 시 ctx.Err() 반환.
func (p *Platform) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		var errs []error

		// E23-D + E27 — EventBus subscriber bridge 먼저 cancel (구독 해제하면 EventBus.Close가 깨끗).
		if p.WebhookBridge != nil {
			p.WebhookBridge.Stop()
		}
		if p.MetricsBridge != nil {
			p.MetricsBridge.Stop()
		}

		// E23-B — webhook dispatcher 먼저 종료 (in-flight POST는 ctx 통해 cancel).
		if p.WebhookDispatcher != nil {
			p.WebhookDispatcher.Stop()
			select {
			case <-p.WebhookDispatcher.Done():
			case <-ctx.Done():
				errs = append(errs, fmt.Errorf("webhook dispatcher: %w", ctx.Err()))
			}
		}

		// E19 — subscription 먼저 cancel하면 EventBus.Close 시 worker가 깨끗이 종료됨.
		if p.insightAutorunSub != nil {
			p.insightAutorunSub.Cancel()
		}

		// E25 — HA leader-election 정지 + advisory lock 해제. Scheduler·Storage 종료 전에
		// release해 다음 인스턴스가 즉시 leader를 가져갈 수 있게 함.
		if p.HA != nil {
			if err := p.HA.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("ha stop: %w", err))
			}
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
		// E34 — Keystore close (file은 no-op, tpm은 TPM session close).
		if p.Keystore != nil {
			if err := p.Keystore.Close(); err != nil {
				errs = append(errs, fmt.Errorf("keystore close: %w", err))
			}
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

// buildLicenseEnforcer는 cfg.LicenseToken + cfg.LicensePublicKeyHex로 license.Enforcer를 만듭니다.
//
// 두 값이 모두 비면 community SKU (nil enforcer 반환 — 호출 측 nil-safe).
// 하나라도 비면 에러 — 부분 설정은 운영 실수 의심으로 빠른 실패.
// 검증 실패(서명/만료/포맷)는 부트스트랩 에러로 즉시 보고.
//
// E24-D — usage 인자: 라이선스 quota check 시점에 호출되는 read-only 사용량 조회 어댑터.
// nil이면 quota check가 호출됐을 때 panic — community SKU(라이선스 nil)는 enforcer 자체가 nil이라 무관.
func buildLicenseEnforcer(cfg Config, clk clock.Clock, usage license.UsageReader) (*license.Enforcer, license.Edition, error) {
	if cfg.LicenseToken == "" && cfg.LicensePublicKeyHex == "" {
		return nil, license.EditionCommunity, nil
	}
	if cfg.LicenseToken == "" || cfg.LicensePublicKeyHex == "" {
		return nil, "", errors.New("license: token and public key must be both set or both empty")
	}
	pubBytes, err := hex.DecodeString(cfg.LicensePublicKeyHex)
	if err != nil {
		return nil, "", fmt.Errorf("license: decode public key hex: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return nil, "", fmt.Errorf("license: public key size %d, want %d", len(pubBytes), ed25519.PublicKeySize)
	}
	payload, err := license.Verify(ed25519.PublicKey(pubBytes), cfg.LicenseToken)
	if err != nil {
		return nil, "", fmt.Errorf("license: verify token: %w", err)
	}
	if payload.IsExpired(clk.Now()) {
		return nil, "", fmt.Errorf("license: token expired (expires=%s)", payload.ExpiresAt.Format(time.RFC3339))
	}
	enforcer := license.NewEnforcer(payload, usage, clk.Now)
	return enforcer, payload.Edition, nil
}

// === O6 — Email + InvitationNotifier 결선 헬퍼 ===

// buildEmailSender는 cfg.EmailProvider 값에 따라 NoopSender 또는 SMTPSender를 반환합니다.
//
// "" 또는 "noop" → NoopSender (기본값, 실 SMTP 호출 X). "smtp" → SMTPSender (Host/Port 필수).
//
// noop은 logger.Info로 발송 시도를 기록 (subcommand stdout 오염 방지). smtp는 실 송신.
func buildEmailSender(cfg Config, logger *slog.Logger) (email.Sender, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.EmailProvider)) {
	case "", "noop":
		// logger.Info로 라우팅 — JSON handler를 거쳐 stdout에 가지만 message가 식별 가능 ("email noop send").
		// subcommand는 자체 logger를 io.Discard로 셋업하면 출력 없음.
		return email.NewNoopSenderWith(slogInfoWriter{logger: logger}, time.Now), nil
	case "smtp":
		return email.NewSMTPSender(email.SMTPConfig{
			Host:        cfg.SMTPHost,
			Port:        cfg.SMTPPort,
			Username:    cfg.SMTPUsername,
			Password:    cfg.SMTPPassword,
			DefaultFrom: cfg.SMTPFrom,
		})
	default:
		return nil, fmt.Errorf("email: unknown provider %q (allowed: noop|smtp)", cfg.EmailProvider)
	}
}

// slogInfoWriter는 io.Writer를 slog.Logger.Info 호출로 어댑팅합니다.
//
// NoopSender가 한 줄에 한 메시지만 쓰므로 buffering 없이 message로 그대로 전달.
// logger가 nil이면 silent (Discard 효과).
type slogInfoWriter struct {
	logger *slog.Logger
}

func (w slogInfoWriter) Write(p []byte) (int, error) {
	if w.logger != nil {
		w.logger.Info("email noop send", "payload", strings.TrimSpace(string(p)))
	}
	return len(p), nil
}

// buildAcceptURLBuilder는 PublicBaseURL 기반 acceptURL 빌더 closure를 반환합니다.
//
// PublicBaseURL이 비어 있으면 nil을 반환 — sqliterepo는 빈 acceptURL을 Notifier에 전달.
// trailing slash는 정규화 (있으면 trim).
func buildAcceptURLBuilder(publicBaseURL string) func(token string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" {
		return nil
	}
	return func(token string) string {
		return base + "/invitations/accept/" + token
	}
}

// invitationEmailNotifier는 tenant.InvitationNotifier 구현입니다 (O6).
//
// 도메인이 platform/email을 직접 import하지 않게 어댑팅 (P5). subject·body는 본 어댑터가
// 빌드 — 도메인은 메시지 내용을 모름. 실패는 logger에 warn으로만 기록 — invitation 자체는
// commit (best-effort delivery).
type invitationEmailNotifier struct {
	sender email.Sender
	logger *slog.Logger
}

func (n *invitationEmailNotifier) NotifyInvitationSent(ctx context.Context, inv tenant.Invitation, acceptURL string) error {
	subject := fmt.Sprintf("rosshield 초대 — %s 역할", inv.RoleName)
	textBody := buildInvitationTextBody(inv, acceptURL)
	htmlBody := buildInvitationHTMLBody(inv, acceptURL)
	err := n.sender.SendMessage(ctx, email.Message{
		To:       inv.Email,
		Subject:  subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	})
	if err != nil && n.logger != nil {
		n.logger.Warn("invitation email send failed",
			"invitationId", inv.ID,
			"to", inv.Email,
			"provider", n.sender.Provider(),
			"err", err.Error())
	}
	return err
}

func buildInvitationTextBody(inv tenant.Invitation, acceptURL string) string {
	var b strings.Builder
	b.WriteString("rosshield 초대\r\n\r\n")
	fmt.Fprintf(&b, "역할: %s\r\n", inv.RoleName)
	fmt.Fprintf(&b, "만료: %s\r\n", inv.ExpiresAt.Format(time.RFC3339))
	if acceptURL != "" {
		b.WriteString("\r\n다음 링크에서 계정을 활성화하세요:\r\n")
		b.WriteString(acceptURL)
		b.WriteString("\r\n")
	} else {
		b.WriteString("\r\n토큰은 관리자가 별도로 전달합니다.\r\n")
	}
	return b.String()
}

func buildInvitationHTMLBody(inv tenant.Invitation, acceptURL string) string {
	if acceptURL == "" {
		return ""
	}
	return fmt.Sprintf(
		`<p>rosshield 초대</p><p>역할: %s</p><p>만료: %s</p><p><a href="%s">계정 활성화</a></p>`,
		inv.RoleName,
		inv.ExpiresAt.Format(time.RFC3339),
		acceptURL,
	)
}
