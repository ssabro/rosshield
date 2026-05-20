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
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	benchmarkrepo "github.com/ssabro/rosshield/internal/domain/benchmark/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/compliance"
	compliancerepo "github.com/ssabro/rosshield/internal/domain/compliance/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/evidence"
	evidencerepo "github.com/ssabro/rosshield/internal/domain/evidence/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/insight"
	insightrepo "github.com/ssabro/rosshield/internal/domain/insight/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/intake"
	intakerepo "github.com/ssabro/rosshield/internal/domain/intake/sqliterepo"
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
	llmvllm "github.com/ssabro/rosshield/internal/platform/llm/vllm"
	"github.com/ssabro/rosshield/internal/platform/metrics"
	"github.com/ssabro/rosshield/internal/platform/replication"
	"github.com/ssabro/rosshield/internal/platform/replication/lagmetric"
	replicationsetup "github.com/ssabro/rosshield/internal/platform/replication/setup"
	replicationrepo "github.com/ssabro/rosshield/internal/platform/replication/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/scheduler/replicationcleanupjob"
	"github.com/ssabro/rosshield/internal/platform/scheduler/rotationjob"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
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
	// LLMProvider: "" → noop, "ollama" → Ollama, "vllm" → vLLM(OpenAI-compat), "anthropic" → Anthropic.
	// 그 외는 부트스트랩 에러.
	// LLMModel·LLMBaseURL·LLMAPIKey·LLMTimeout은 provider별 의미가 다름 (provider 주석 참조).
	//
	// LLM private deployment 추가 (D-LLM-1·D-LLM-5·D-LLM-7):
	//   - LLMMaxTokens: vllm용 응답 토큰 상한 (0이면 어댑터 default 1024).
	//   - LLMKeepAlive: ollama용 모델 메모리 유지 시간 (0이면 default 5분, 음수면 즉시 unload).
	//   - LLMAutoPull: ollama AutoPull 옵션 (true면 customer가 미리 받지 않은 모델을 부팅 후
	//     PullModel로 다운로드 — 에어갭 환경은 반드시 false 유지).
	LLMProvider  string
	LLMModel     string
	LLMBaseURL   string        // ollama daemon URL / vllm endpoint / anthropic API base
	LLMAPIKey    string        // anthropic 필수, vllm 옵션, ollama 미사용
	LLMTimeout   time.Duration // 0이면 어댑터 기본값
	LLMMaxTokens int           // vllm 응답 토큰 상한 (0이면 1024 default)
	LLMKeepAlive time.Duration // ollama keep_alive (0=default 5m, <0=즉시 unload)
	LLMAutoPull  bool          // ollama AutoPull 옵션 (에어갭 환경은 false 유지)

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

	// E-MR (Phase 8) — Multi-region HA (옵션 A = PG logical replication + Route53 DNS).
	//
	// 본 round Stage 1·2: Config 등록 + standby-mode middleware + manual failover handler 결선.
	// 본 round 미진행 (Stage 3~7): PG publication·subscription 자동 setup, DNS hook 실 SDK,
	// 자동 failover, cross-region audit witness.
	//
	// ReplicationConfig.Enabled=false (default)면 single-region 동작 그대로 — 본 코드 도입으로
	// 동작 변경 없음. true + Role=standby면 write API가 standby middleware로 차단.
	ReplicationConfig replication.Config

	// E-MR Stage 3 — PG logical replication publication/subscription 자동 setup.
	//
	// ReplicationConfig.Enabled=true + StorageDriver=postgres 조합에서만 결선.
	// sqlite·single-region·standalone 배포에는 영향 0.
	//
	// 동작:
	//   - Role=primary → bootstrap 시 CREATE PUBLICATION (idempotent)
	//   - Role=standby → bootstrap 시 CREATE SUBSCRIPTION (idempotent)
	//
	// 자동 setup 비활성: ReplicationAutoSetup=false (default). 운영자가 수동으로
	// PUBLICATION/SUBSCRIPTION을 생성한 환경(권장 — 권한 분리)에서는 false 유지.
	// 부팅 시 자동 생성을 원할 때만 true로.
	ReplicationAutoSetup bool

	// ReplicationPublicationName은 primary가 publish할 PUBLICATION 이름입니다.
	// 빈 값이면 default "rosshield_main".
	ReplicationPublicationName string

	// ReplicationPublicationAllTables=true (권장 default)면 `FOR ALL TABLES` —
	// 신규 application 테이블 자동 포함 (multi-region-ha-design §4.5).
	ReplicationPublicationAllTables bool

	// ReplicationSubscriptionName은 standby가 생성할 SUBSCRIPTION 이름입니다.
	// 빈 값이면 default "rosshield_main_sub".
	ReplicationSubscriptionName string

	// ReplicationPrimaryConnString은 standby가 primary PG에 logical replication
	// 연결할 때 사용하는 conn string. Role=standby + AutoSetup=true 시 필수.
	// password 포함 — env(ROSSHIELD_REPLICATION_PRIMARY_CONN_STRING)에만 두고
	// 파일로 dump 금지.
	ReplicationPrimaryConnString string

	// E34 — KeyStore 어댑터 선택 (Phase 5 어플라이언스 트랙).
	//
	// "" 또는 "file" → file 어댑터(현재 동작, soft.LoadOrCreatePrivateKey 위임).
	// "tpm" → TPM 2.0 PCR-sealed (Stage 1 placeholder = 즉시 부팅 실패, Stage 2+ 본격 구현).
	//
	// R40-2 결정(2026-05-11): TPM 시뮬레이터 = swtpm. R41 결정 후 본격 구현.
	KeystoreType string

	// B7 후속 — 자동 백업 schedule (Phase 5).
	//
	// BackupSchedule이 비지 않으면 cronsched에 자동 백업 job 등록. HA 활성 환경은
	// cronsched가 follower tick을 silent skip(E25 Stage 4a)하므로 leader만 백업 수행.
	// BackupDir 빈 값이면 DataDir/backups. BackupSkipEvidence=true면 메타데이터만 백업.
	BackupSchedule     string // cron spec (예: "@every 24h" 또는 "0 15 3 * * *"). 빈 값 = 자동 백업 비활성.
	BackupDir          string // 빈 값이면 DataDir/backups.
	BackupSkipEvidence bool

	// E32 Stage 6 — Audit chain rotation 자동 cron schedule.
	//
	// AuditRotationSchedule이 비지 않으면 cronsched에 rotation job 등록 — 매 tick에
	// 모든 활성 tenant에 대해 직전 segment 이후 신규 entry를 새 segment로 archive.
	// 빈 값 = 자동 rotation 비활성 (manual API only).
	//
	// design doc default = 월 1회 (`@every 720h` 또는 `0 0 1 * *`). 빈 chain·신규 entry 없는
	// tenant는 silent skip. HA 활성 시 leader 단일 인스턴스만 수행 (cronsched RoleProvider gate).
	AuditRotationSchedule string

	// E-MR Stage 3 후속 — 정기 PG replication slot cleanup cron (v0.6.9 carryover 해소).
	//
	// ReplicationSlotCleanupSchedule이 비지 않으면 cronsched에 cleanup job 등록 — 매 tick에
	// pg_replication_slots에서 비활성·stale slot을 detect + drop. 빈 값 = 자동 cleanup 비활성
	// (manual setup.CleanupInactiveSlots 호출만).
	//
	// HA 활성 시 leader 단일 인스턴스만 수행. SlotPrefix는 다른 application slot 실수 drop
	// 방지를 위한 안전 가드 — 자동 cleanup 활성 시 prefix 명시 필수.
	//
	// 권장 default: 일 1회 (`@every 24h`), prefix "rosshield_", MinInactiveAge 24h.
	ReplicationSlotCleanupSchedule       string        // cron spec. 빈 값 = 자동 cleanup 비활성.
	ReplicationSlotCleanupPrefix         string        // "rosshield_" 권장. 자동 활성 시 필수.
	ReplicationSlotCleanupMinInactiveAge time.Duration // default 24h (0이면 setup 패키지 기본).
	ReplicationSlotCleanupDryRun         bool          // true면 후보만 logging (운영자 검토용).

	// Phase 8 MR.T8 — pg_stat_replication lag metric collector.
	//
	// ReplicationLagMetricEnabled=true이면 primary PG + replication enabled 조합에서
	// goroutine이 30초 간격으로 pg_stat_replication.replay_lag을 polling해
	// rosshield_replication_lag_seconds gauge를 emit합니다. 조합 미일치(sqlite/standby)는
	// silent skip.
	ReplicationLagMetricEnabled  bool
	ReplicationLagMetricInterval time.Duration // default 30s (0이면 lagmetric.DefaultInterval)

	// D-AR-4 cosign keyless 서명 옵션 (Audit rotation).
	//
	// CosignEnabled=true일 때 매 rotation 후 archive를 cosign sign-blob으로 서명 →
	// bundle을 audit_rotation_segments.cosign_bundle 컬럼에 저장. 활성 시 cosign binary가
	// PATH에 또는 CosignBinaryPath에 존재해야 함 (옵션 A 외부 CLI 채택).
	//
	// 에어갭 customer는 CosignEnabled=false (default) — bundle은 NULL, segment_hash·
	// archive_sha256만으로 결정론적 검증 유지. cosign verify는 verify CLI에서 별도 수행.
	//
	// env 매핑: ROSSHIELD_COSIGN_ENABLED · _BINARY · _IDENTITY · _FULCIO_URL · _REKOR_URL.
	CosignEnabled    bool
	CosignBinaryPath string // 빈 값이면 "cosign" PATH lookup.
	CosignIdentity   string // OIDC sub claim 기대치 (운영 doc · verify 측에서 사용).
	CosignFulcioURL  string // 빈 값이면 Sigstore public Fulcio.
	CosignRekorURL   string // 빈 값이면 Sigstore public Rekor.

	// E32 + D-AR-9 — Audit rotation cold backend 선택.
	//
	// AuditColdBackend="" 또는 "file" (default) → DataDir/audit-archives 로컬 디렉토리 (Apache-2.0).
	// AuditColdBackend="s3"                     → AWS S3 (BSL 1.1 enterprise, build tag `rosshield_enterprise`).
	//
	// 코어 빌드에서 "s3" 지정 시 ErrS3BackendNotAvailable → file backend로 graceful fallback +
	// warning log. enterprise 빌드에서 "s3" 지정 + 아래 필수 필드 누락 시 부트스트랩 에러.
	AuditColdBackend string

	// AuditS3Bucket·AuditS3Region·AuditS3Prefix·AuditS3Endpoint·AuditS3SSE·AuditS3KMSKeyID는
	// AuditColdBackend="s3" 일 때 의미. enterprise 빌드에서만 실제 S3 호출.
	AuditS3Bucket         string
	AuditS3Region         string
	AuditS3Prefix         string
	AuditS3Endpoint       string
	AuditS3ForcePathStyle bool
	AuditS3SSE            string
	AuditS3KMSKeyID       string

	// E32 + D-AR-9 후속 — S3 lifecycle policy 자동 적용 (v0.6.9 carryover 해소).
	//
	// AuditS3LifecycleEnabled=true 시 NewS3Backend 시점에 PutBucketLifecycleConfiguration
	// 자동 호출 (rule ID "rosshield-rotation", Filter.Prefix=cfg.AuditS3Prefix). 적용은
	// idempotent — 반복 부팅에 안전.
	//
	// 표준 audit retention 시나리오 cover:
	//   - 첫 N일 STANDARD, IADays 후 STANDARD_IA, GlacierDays 후 GLACIER, DeepArchiveDays
	//     후 DEEP_ARCHIVE, 마지막 ExpireDays 후 자동 삭제 (옵션).
	//   - 각 *Days=0이면 해당 단계 transition 없음. ExpireDays=0이면 영구 보존.
	//
	// MinIO 등 일부 S3 호환 storage는 GLACIER·DEEP_ARCHIVE를 silent ignore — error 0,
	// rule 자체는 정상 등록 (호환성 우선).
	AuditS3LifecycleEnabled                   bool
	AuditS3LifecycleTransitionIADays          int32 // STANDARD → STANDARD_IA 전환 일수
	AuditS3LifecycleTransitionGlacierDays     int32 // STANDARD → GLACIER 전환 일수
	AuditS3LifecycleTransitionDeepArchiveDays int32 // STANDARD → DEEP_ARCHIVE 전환 일수
	AuditS3LifecycleExpireDays                int32 // object 만료 일수 (0=영구)

	// CheckTimeoutDefaultSec는 scanrun.Orchestrator가 CheckDef.TimeoutSec=0인 항목에
	// 적용할 default SSH exec timeout. 0이면 scan.DefaultCheckTimeoutSec(10초). per-check
	// TimeoutSec은 항상 우선 — 본 값은 fallback default만 조정.
	//
	// 운영자 시나리오: 합성 multi-line bash 또는 base64 sub-shell wrap이 customer 환경에서
	// 더 긴 시간이 필요하면 ↑, fail-fast 정책이면 ↓.
	CheckTimeoutDefaultSec int
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
	Intake            intake.Service           // Phase 6 후보 1 R1 Stage 3+4 — customer intake CRUD + auto-provisioning wrap
	Webhook           webhook.Service          // E23 — webhook + SIEM 통합 도메인
	WebhookDispatcher *webhookrun.Dispatcher   // E23-B — Process worker
	WebhookBridge     *webhookrun.EventBridge  // E23-D — EventBus → webhook.Enqueue bridge
	SSO               sso.Service              // E20-D — SSO Provider CRUD + IdP 호출
	SSOGroupMapping   sso.GroupMappingService  // RBAC fleet 정밀화 Stage 5 — group → role 자동 매핑 CRUD + resolve
	Invitation        tenant.InvitationService // E21 — 초대·역할 관리
	Metrics           *metrics.Registry        // E27 — Prometheus exposition (옵트인)
	MetricsBridge     *metrics.MetricsBridge   // E27 — EventBus → counter 결선
	HA                *ha.Manager              // E25 — leader-election (HAEnabled 시 non-nil, 아니면 nil)
	HotGC             *rotation.HotGC          // E32 Stage 4 — audit hot GC (sqlite marker mode + PG GUC 양쪽)
	Replication       replication.Repository   // E-MR Stage 1 — replication metadata 어댑터 (sqlite/PG 양쪽)
	ReplicationConfig replication.Config       // E-MR Stage 1~2 — 본 인스턴스의 region·role + standby middleware 활성 여부
	Keystore          keystore.KeyStore        // E34 — KeyStore 어댑터 (file 기본, tpm은 Stage 2+)
	BackupDir         string                   // B7 후속 — 자동 백업 디렉터리 (handlers/backup이 list 시 사용)
	FleetScanSched    *FleetScanScheduler      // dynamic cron re-registration on fleet mutation
	SSHPool           sshpool.Pool             // scanrun Stage 5b — idle 재사용 + keepalive (Shutdown 시 Close)

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

// EmitFleetUpdated는 fleet.updated 엔트리를 audit에 emit합니다.
func (a *auditEmitterAdapter) EmitFleetUpdated(ctx context.Context, tx storage.Tx, f robot.Fleet) error {
	payload := fmt.Sprintf(`{"fleetId":%q,"name":%q,"description":%q}`, f.ID, f.Name, f.Description)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: f.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "fleet.updated",
		Target:   audit.Target{Type: "fleet", ID: f.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// EmitFleetDeleted는 fleet.deleted 엔트리를 audit에 emit합니다 (soft delete).
func (a *auditEmitterAdapter) EmitFleetDeleted(ctx context.Context, tx storage.Tx, f robot.Fleet) error {
	payload := fmt.Sprintf(`{"fleetId":%q,"name":%q}`, f.ID, f.Name)
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: f.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "fleet.deleted",
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

	// E34 — 어댑터별 handle 형식 분기.
	//   file 어댑터: handle = 전체 디스크 경로 (현재 동작 호환, $DataDir/keys/platform.ed25519)
	//   tpm  어댑터: handle = 단순 식별자 ("platform" → SealingDir/platform.sealed)
	// 동일 KeyStore 인터페이스에 두 형식이 공존 — bootstrap 단에서 결정.
	platformHandle := keyHandle(cfg, "platform")
	platformPriv, err := ks.LoadOrCreatePrivateKey(platformHandle)
	if err != nil {
		_ = ks.Close()
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: signer: %w", err)
	}
	sgn := soft.WrapPrivateKey(platformPriv)

	// JWT 별도 키 — audit checkpoint 키와 분리(B4 결정).
	// 키 회전 주기·키 손실 영향이 다르므로 결선 단계에서 두 개 별도 키.
	// jwt 라이브러리(`golang-jwt/jwt/v5`)는 raw ed25519.PrivateKey/PublicKey를 요구.
	jwtHandle := keyHandle(cfg, "jwt")
	jwtPrivateKey, err := ks.LoadOrCreatePrivateKey(jwtHandle)
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

	// E27 — metrics.Registry 사전 생성 (scanrun SSH 통합 Stage 4 — sshExec metrics
	// 결선 위해 일찍 만듦). EventBus bridge 결선은 후속 line에서.
	metricsReg := metrics.New()

	// E6 Stage D.2 — scan Orchestrator 결선 (R6-1~R6-8) + E7 evidence 결선.
	// scanrun SSH 통합 Stage 3 — KnownHostsManager로 robot 별 TOFU host key callback 결선.
	// 부팅 실패 시 server start abort (data dir 권한·생성 실패 등은 운영자 즉시 수정 필요).
	khMgr, err := sshpool.NewKnownHostsManager(robotSvc, store, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: KnownHostsManager: %w", err)
	}
	// scanrun SSH 통합 Stage 5b — sshpool.Pool 결선(idle 재사용 활성화).
	// Stage 4 idle 인프라(IdleTimeout > 0이면 release된 conn 재사용)를 본 결선으로 활성화.
	// IdleTimeout default 5min — customer 부하 측정 후 cfg로 조정 가능(향후).
	sshExecMetricsAdapt := &sshExecMetricsAdapter{reg: metricsReg}
	sshPool := sshpool.NewPool(sshpool.PoolConfig{
		IdleTimeout:       5 * time.Minute,
		KeepaliveInterval: 30 * time.Second,
		Metrics:           &sshPoolMetricsAdapter{reg: metricsReg},
	})
	scanRun := scanrun.New(scanrun.Deps{
		Scan:    scanSvc,
		Storage: store,
		Executor: &sshExecutorAdapter{
			pool:     sshPool,
			robot:    robotSvc,
			storage:  store,
			khMgr:    khMgr, // robot 별 TOFU callback (D-SCAN-2 권장 default)
			logger:   logger,
			execMetr: sshExecMetricsAdapt,
		},
		Evaluator: &benchmarkEvaluatorAdapter{},
		Bus:       bus,
		Clock:     clk,
		Evidence:  evidenceSvc,
		// WorkerLimit은 default(R4-4 — 10).
		CheckTimeoutDefaultSec: cfg.CheckTimeoutDefaultSec,
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

	// E12 Stage 8 — system tenant row 자동 시드 (idempotent).
	// packs(tenant_id='system') FK가 tenants(id)를 참조하므로, system tenant row가 없으면
	// seedBuiltinPacks의 InstallPack이 FK 위반으로 silent fail. 본 시드가 선결.
	if err := seedSystemTenant(ctx, store, cfg.StorageDriver, systemTenant); err != nil {
		logger.Warn("bootstrap: seed system tenant failed (non-fatal)", "err", err)
	}

	// E12 — first-boot built-in pack seed loader (idempotent).
	// internal/builtin/packs._archives 의 dev signer 서명 pack을 systemTenant에 자동 install.
	// 이미 install된 pack은 ErrPackAlreadyInstalled로 silent skip. 비-fatal — seed 실패해도
	// server boot 유지(운영자가 수동 install 가능).
	if err := seedBuiltinPacks(ctx, store, benchmarkSvc, systemTenant, logger); err != nil {
		logger.Warn("bootstrap: seed builtin packs failed (non-fatal, server boot continues)",
			"err", err)
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

	// B7 후속 — 자동 백업 schedule (옵트인). BackupSchedule="" → no-op.
	// HA 활성 시 cronsched의 RoleProvider gate(E25 Stage 4a)가 follower tick을 silent skip.
	resolvedBackupDir := cfg.BackupDir
	if resolvedBackupDir == "" {
		resolvedBackupDir = filepath.Join(cfg.DataDir, "backups")
	}
	if err := registerBackupJob(sch, cfg.BackupSchedule, cfg.DataDir, resolvedBackupDir, cfg.BackupSkipEvidence, logger); err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: register backup job: %w", err)
	}

	// E32 Stage 6 — Audit chain rotation 자동 cron job 등록 (옵트인).
	//
	// AuditRotationSchedule="" → 자동 rotation 비활성 (manual API only).
	// HA 활성 시 cronsched RoleProvider gate(E25 Stage 4a)가 follower tick을 silent skip.
	// rotation.Backend는 cfg.AuditColdBackend로 분기 (file default, "s3" enterprise).
	if cfg.AuditRotationSchedule != "" {
		rotBackend, rotBackendDesc, err := buildRotationBackend(ctx, cfg, logger)
		if err != nil {
			_ = sch.Close(ctx)
			_ = store.Close()
			return nil, fmt.Errorf("bootstrap: rotation backend: %w", err)
		}
		// D-AR-4 — cosign keyless signer (옵션). CosignEnabled=false면 nil로 두면 Rotator가
		// 서명 skip + cosign_bundle 컬럼 NULL.
		var rotSigner rotation.Signer
		if cfg.CosignEnabled {
			rotSigner = rotation.NewCosignSigner(rotation.SignerConfig{
				Enabled:    true,
				BinaryPath: cfg.CosignBinaryPath,
				Identity:   cfg.CosignIdentity,
				FulcioURL:  cfg.CosignFulcioURL,
				RekorURL:   cfg.CosignRekorURL,
			})
			logger.Info("audit rotation cosign signing enabled",
				"binaryPath", cfg.CosignBinaryPath,
				"identity", cfg.CosignIdentity,
				"fulcio", cfg.CosignFulcioURL,
				"rekor", cfg.CosignRekorURL)
		}
		rotator, err := rotation.New(rotation.Deps{
			Clock:    clk,
			Backend:  rotBackend,
			Appender: auditSvc,
			Signer:   rotSigner,
		})
		if err != nil {
			_ = sch.Close(ctx)
			_ = store.Close()
			return nil, fmt.Errorf("bootstrap: rotation.New: %w", err)
		}
		tenantLister := rotationjob.TenantListerFunc(func(c context.Context) ([]storage.TenantID, error) {
			return listAllTenantIDs(c, store)
		})
		if err := rotationjob.Register(sch, rotationjob.Deps{
			Storage: store,
			Audit:   auditSvc,
			Rotator: rotator,
			Tenants: tenantLister,
			Logger:  logger,
		}, rotationjob.DefaultJobID, cfg.AuditRotationSchedule); err != nil {
			_ = sch.Close(ctx)
			_ = store.Close()
			return nil, fmt.Errorf("bootstrap: register rotation job: %w", err)
		}
		logger.Info("audit rotation auto-schedule active",
			"spec", cfg.AuditRotationSchedule, "backend", rotBackendDesc)
	}

	// E32 Stage 4 — audit hot GC 생성 (v0.7.x carryover).
	//
	// 항상 HotGC 생성 (handler가 nil이면 503 응답). cfg.StorageDriver로 분기 — sqlite는
	// 0036 marker 모드 (audit_gc_mode marker row), PG는 0034 GUC 모드 (SET LOCAL).
	// platform 변수는 본 위치에서 아직 선언 전이라 hotGC 변수만 만들고 결선은 후속 단계.
	hotGC, err := rotation.NewHotGC(rotation.HotGCDeps{
		Policy:        rotation.DefaultPolicy(),
		Appender:      auditSvc,
		Clock:         clk,
		UseMarkerMode: cfg.StorageDriver == "sqlite" || cfg.StorageDriver == "",
	})
	if err != nil {
		_ = sch.Close(ctx)
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: NewHotGC: %w", err)
	}

	// E-MR Stage 3 후속 — 정기 PG replication slot cleanup cron 등록 (v0.6.9 carryover).
	//
	// 조건:
	//   - ReplicationSlotCleanupSchedule != ""
	//   - ReplicationConfig.Enabled = true (PG replication 활성)
	//   - ReplicationConfig.Role = primary (slot은 primary에만 존재)
	//   - PG storage (sqlite는 logical replication 미지원)
	//
	// 위 조건이 모두 만족돼야 cron 등록. 그 외에는 silent skip (운영자 의도와 무관한 등록 회피).
	// HA 활성 시 cronsched RoleProvider gate가 follower tick을 silent skip.
	if cfg.ReplicationSlotCleanupSchedule != "" &&
		cfg.ReplicationConfig.Enabled &&
		cfg.ReplicationConfig.Role == replication.RolePrimary {
		if pg, ok := store.(*postgres.Postgres); ok {
			if err := replicationcleanupjob.Register(sch, replicationcleanupjob.Deps{
				Executor:       replicationsetup.NewPgxExecutor(pg.Pool()),
				SlotPrefix:     cfg.ReplicationSlotCleanupPrefix,
				MinInactiveAge: cfg.ReplicationSlotCleanupMinInactiveAge,
				DryRun:         cfg.ReplicationSlotCleanupDryRun,
				Logger:         logger,
			}, replicationcleanupjob.DefaultJobID, cfg.ReplicationSlotCleanupSchedule); err != nil {
				_ = sch.Close(ctx)
				_ = store.Close()
				return nil, fmt.Errorf("bootstrap: register replication slot cleanup: %w", err)
			}
		} else {
			logger.Warn("replication slot cleanup schedule set but storage is not PG — silent skip",
				"schedule", cfg.ReplicationSlotCleanupSchedule)
		}
	}

	// Phase 8 MR.T8 — replication lag metric collector (v0.7.x carryover 일소).
	//
	// 조건: ReplicationLagMetricEnabled=true + ReplicationConfig.Enabled=true +
	// Role=primary + PG storage. 그 외는 silent skip. HA gate는 collector 단계 미적용 —
	// 단일 primary 전체에서 emit (HA leader-only 결선은 후속 carryover).
	if cfg.ReplicationLagMetricEnabled &&
		cfg.ReplicationConfig.Enabled &&
		cfg.ReplicationConfig.Role == replication.RolePrimary {
		if pg, ok := store.(*postgres.Postgres); ok {
			lagCollector, err := lagmetric.New(lagmetric.Deps{
				Querier:  pg.Pool(),
				Registry: metricsReg,
				Interval: cfg.ReplicationLagMetricInterval,
				Logger:   logger,
			})
			if err != nil {
				_ = sch.Close(ctx)
				_ = store.Close()
				return nil, fmt.Errorf("bootstrap: lagmetric.New: %w", err)
			}
			lagCollector.Start(ctx)
			logger.Info("replication lag metric collector started",
				"interval", cfg.ReplicationLagMetricInterval)
		} else {
			logger.Warn("replication lag metric requested but storage is not PG — silent skip")
		}
	}

	// FleetPolicy.ScanSchedule cron — best-effort 등록.
	// 등록 실패는 fatal 아님 (단일 fleet 등록 실패가 부트 차단 X).
	fleetScanSch := NewFleetScanScheduler(store, robotSvc, benchmarkSvc, scanSvc, scanRun, sch, logger)
	if err := fleetScanSch.RegisterAll(ctx); err != nil {
		logger.Warn("bootstrap: register fleet scan jobs failed (non-fatal)", "err", err.Error())
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

	// E27 — Prometheus EventBus bridge 결선 (metricsReg는 위에서 생성됨).
	// /metrics endpoint mount는 main.go --metrics-addr 옵트인 시점에 별 mux로.
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

	// Phase 6 후보 1 R1 Stage 3+4 — Customer onboarding intake 결선 + auto-provisioning wrap.
	//
	// Stage 3: raw intake.Service(sqliterepo) 도메인 → handler.Deps.Intake 주입.
	// Stage 4: 그 위에 intakeProvisioningAdapter wrap → AcceptIntake 호출 시 같은 Tx에
	//          tenant.Service.Create + license 발급 placeholder + intake row UPDATE 묶음
	//          (cmd/rosshield-server/intake_provisioning.go).
	//
	// handler RBAC gate: RequirePermission(ResourceTenantAdmin, ActionAdmin) — design doc
	// §6.1 + §7 R1 Stage 3. licenseEnforcer nil 허용(paying customer 0 단계).
	rawIntakeSvc := intakerepo.New(intakerepo.Deps{
		Clock: clk,
		IDGen: ids,
	})
	intakeSvc := newIntakeProvisioningAdapter(rawIntakeSvc, tenantSvc, licenseEnforcer)

	logger.Info("platform bootstrap complete",
		"dataDir", cfg.DataDir,
		"dbPath", dbPath,
		"keyHandle", platformHandle,
		"keystoreType", keystoreLogLabel(cfg.KeystoreType),
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
		Intake:            intakeSvc,
		Webhook:           webhookSvc,
		WebhookDispatcher: webhookDispatcher,
		WebhookBridge:     webhookBridge,
		SSO:               ssoSvc,
		SSOGroupMapping:   ssoSvc, // RBAC fleet 정밀화 Stage 5 — *ssorepo.Repo가 GroupMappingService도 구현.
		Invitation:        invitationSvc,
		Metrics:           metricsReg,
		MetricsBridge:     metricsBridge,
		HotGC:             hotGC,
		Keystore:          ks,
		BackupDir:         resolvedBackupDir,
		FleetScanSched:    fleetScanSch,
		SSHPool:           sshPool,
		systemTenant:      systemTenant,
		insightAutorunSub: insightAutorunSub,
		Replication:       replicationrepo.New(),
		ReplicationConfig: cfg.ReplicationConfig,
	}

	// E-MR Stage 3 — PG logical replication publication/subscription 자동 setup.
	// cfg.ReplicationAutoSetup=true + PG storage 조합에서만 실행 (sqlite는 logical
	// replication 지원 X). 실패 시 부팅 fail-fast — 부분 setup 상태 회피.
	if cfg.ReplicationConfig.Enabled && cfg.ReplicationAutoSetup {
		if err := runReplicationSetup(ctx, cfg, store, logger); err != nil {
			_ = platform.Shutdown(ctx)
			return nil, fmt.Errorf("bootstrap: replication setup: %w", err)
		}
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
		// E25 Stage 4 잔여 — HA metric bridge (Grafana dashboard placeholder 활성).
		// promote/demote callback에서 rosshield_ha_role/leader_epoch/failover_total 갱신.
		haMgr.OnLeaderAcquired(func() {
			metricsReg.OnHAPromoted(haMgr.CurrentEpoch())
		})
		haMgr.OnLeaderLost(func() {
			metricsReg.OnHADemoted()
		})
		platform.HA.Start(context.Background())
		logger.Info("ha enabled — leader-election started",
			"lockId", haCfgLockID(cfg),
			"interval", haCfgInterval(cfg),
			"leaderId", haMgr.LeaderID())
	}

	return platform, nil
}

// keystoreLogLabel은 빈 KeystoreType을 "file"로 정규화합니다 (관측 일관성).
func keystoreLogLabel(t string) string {
	if t == "" {
		return "file"
	}
	return t
}

// keyHandle은 cfg.KeystoreType에 따라 KeyStore에 전달할 handle을 만듭니다 (E34).
//
// file 어댑터: handle = 전체 디스크 경로 ($DataDir/keys/<name>.ed25519)
// tpm  어댑터: handle = 단순 식별자 (<name>) — SealingDir/<name>.sealed로 매핑
func keyHandle(cfg Config, name string) string {
	if cfg.KeystoreType == "tpm" {
		return name
	}
	return filepath.Join(cfg.DataDir, "keys", name+".ed25519")
}

// buildKeystore는 cfg.KeystoreType 기반으로 KeyStore 어댑터를 생성합니다 (E34).
//
// "" / "file" → file 어댑터 (현재 동작, soft.LoadOrCreatePrivateKey 위임)
// "tpm" → TPM 2.0 PCR-sealed 어댑터 (Stage 2-B):
//   - SealingDir = $DataDir/keys/tpm/
//   - PCRSelection = R41-3 기본 [0,2,4,7]
//   - DevicePath = "" (Linux 기본 /dev/tpmrm0 → /dev/tpm0)
//   - Linux 외 환경 또는 TPM 디바이스 부재 시 ErrTpmDeviceNotAvailable로 부팅 실패
//     (조용한 file fallback 금지 — 디스크 평문 키 위협 노출 방지).
func buildKeystore(cfg Config) (keystore.KeyStore, error) {
	switch cfg.KeystoreType {
	case "", "file":
		return keystorefile.New(), nil
	case "tpm":
		opts := keystoretpm.Options{
			SealingDir: filepath.Join(cfg.DataDir, "keys", "tpm"),
		}
		store, err := keystoretpm.New(opts)
		if err != nil {
			return nil, err
		}
		return store, nil
	default:
		return nil, fmt.Errorf("%w: %q (allowed: file|tpm)", keystore.ErrUnsupportedDriver, cfg.KeystoreType)
	}
}

// runReplicationSetup은 PG logical replication publication/subscription을 자동
// 생성합니다 (E-MR Stage 3).
//
// 조건:
//   - cfg.ReplicationConfig.Enabled=true
//   - cfg.ReplicationAutoSetup=true
//   - storage가 PG 어댑터 (sqlite 거부 — logical replication 미지원)
//
// idempotent: 이미 존재하면 skip. 부팅 반복에 안전.
//
// fail-fast: setup 실패 시 에러 반환 → 호출자(Bootstrap)가 platform.Shutdown 후
// 부팅 중단. 부분 setup 상태(publication만 있고 subscription 없음)는 운영자가
// 수동 점검 필요.
func runReplicationSetup(ctx context.Context, cfg Config, store storage.Storage, logger *slog.Logger) error {
	pg, ok := store.(*postgres.Postgres)
	if !ok {
		return errors.New("replication auto-setup requires postgres storage (sqlite has no logical replication)")
	}

	exec := replicationsetup.NewPgxExecutor(pg.Pool())

	pubName := cfg.ReplicationPublicationName
	if pubName == "" {
		pubName = replicationsetup.DefaultPublicationSpec().Name
	}
	subName := cfg.ReplicationSubscriptionName
	if subName == "" {
		subName = replicationsetup.DefaultSubscriptionSpec("").Name
	}

	switch cfg.ReplicationConfig.Role {
	case replication.RolePrimary:
		pubSpec := &replicationsetup.PublicationSpec{
			Name:      pubName,
			AllTables: cfg.ReplicationPublicationAllTables,
		}
		if err := replicationsetup.Setup(ctx, exec, replication.RolePrimary, pubSpec, nil); err != nil {
			return err
		}
		logger.Info("replication primary publication ensured",
			"name", pubSpec.Name,
			"allTables", pubSpec.AllTables,
			"region", cfg.ReplicationConfig.Region)
		return nil

	case replication.RoleStandby:
		if cfg.ReplicationPrimaryConnString == "" {
			return errors.New("replication standby auto-setup requires --replication-primary-conn-string (ROSSHIELD_REPLICATION_PRIMARY_CONN_STRING)")
		}
		subSpec := &replicationsetup.SubscriptionSpec{
			Name:              subName,
			PublicationName:   pubName,
			PrimaryConnString: cfg.ReplicationPrimaryConnString,
			Copy:              false, // 운영 default: 초기 데이터 복사는 사전 완료 가정
		}
		if err := replicationsetup.Setup(ctx, exec, replication.RoleStandby, nil, subSpec); err != nil {
			return err
		}
		logger.Info("replication standby subscription ensured",
			"name", subSpec.Name,
			"publication", subSpec.PublicationName,
			"region", cfg.ReplicationConfig.Region)
		return nil

	default:
		return fmt.Errorf("unknown replication role: %q", cfg.ReplicationConfig.Role)
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
//	"ollama"      → ollama.New(BaseURL, DefaultModel, Timeout, KeepAlive, AutoPull)
//	"vllm"        → vllm.New(BaseURL, APIKey, DefaultModel, Timeout, MaxTokens) — D-LLM-1.
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
			KeepAlive:    cfg.LLMKeepAlive,
			AutoPull:     cfg.LLMAutoPull,
		}), nil
	case "vllm":
		// D-LLM-1 — OpenAI-compatible self-hosted inference (vLLM, TGI 등).
		// APIKey는 옵션(자체 host 환경이 흔히 인증 없음 — 있으면 Bearer로 전송).
		return llmvllm.New(llmvllm.Options{
			BaseURL:      cfg.LLMBaseURL,
			APIKey:       cfg.LLMAPIKey,
			DefaultModel: cfg.LLMModel,
			HTTPTimeout:  cfg.LLMTimeout,
			MaxTokens:    cfg.LLMMaxTokens,
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
		return nil, fmt.Errorf("unknown LLMProvider %q (allowed: noop|ollama|vllm|anthropic)", cfg.LLMProvider)
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

		// scanrun Stage 5b — sshpool.Pool 종료(keepalive goroutine + idle conn 모두 close).
		if p.SSHPool != nil {
			if err := p.SSHPool.Close(); err != nil {
				errs = append(errs, fmt.Errorf("sshpool close: %w", err))
			}
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
