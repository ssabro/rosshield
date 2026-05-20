package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/api/handlers"
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	"github.com/ssabro/rosshield/internal/platform/replication"
	"github.com/ssabro/rosshield/internal/platform/storage"
	webembed "github.com/ssabro/rosshield/internal/web"
)

// healthResponse는 /healthz 응답 본문입니다.
type healthResponse struct {
	Status     string            `json:"status"`
	Components componentStatuses `json:"components"`
	Audit      auditHealth       `json:"audit"`
	HA         *haHealth         `json:"ha,omitempty"` // E25 — HA 활성 시에만 노출.
}

type componentStatuses struct {
	Storage   string `json:"storage"`
	EventBus  string `json:"eventbus"`
	Scheduler string `json:"scheduler"`
	Signer    string `json:"signer"` // keyID 노출 (운영 식별용).
}

// auditHealth는 system tenant audit 체인 상태입니다.
type auditHealth struct {
	HeadSeq        int64  `json:"headSeq"`
	LastCheckpoint int64  `json:"lastCheckpointSeq"` // 0이면 아직 없음
	Status         string `json:"status"`            // "ok" | "no-entries" | "error: ..."
}

// haHealth는 E25 HA 상태입니다 (HAEnabled 시에만 응답에 포함).
//
// LB·운영자가 follower vs leader를 구분하는 1차 신호. write 라우팅·재시작 결정에 사용.
type haHealth struct {
	Enabled         bool   `json:"enabled"`
	Role            string `json:"role"`            // "leader" | "follower"
	Epoch           int64  `json:"epoch"`           // 0이면 아직 leader 아님
	LeaderID        string `json:"leaderId"`        // 본 인스턴스 ID
	LastHeartbeatAt string `json:"lastHeartbeatAt"` // RFC3339, 빈 문자열이면 첫 tick 전
}

func healthHandler(p *Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if p.IsShutdown() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(healthResponse{
				Status: "shutting_down",
				Components: componentStatuses{
					Storage:   "down",
					EventBus:  "down",
					Scheduler: "down",
					Signer:    p.Signer.KeyID(),
				},
				Audit: auditHealth{Status: "down"},
			})
			return
		}

		// Storage 살아있는지 + audit head·checkpoint 조회를 같은 Bootstrap Tx에서.
		storageOK := "ok"
		auditState := auditHealth{Status: "ok"}
		if err := p.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
			head, err := p.Audit.Head(ctx, tx, p.systemTenantID())
			if err != nil {
				return err
			}
			auditState.HeadSeq = head.Seq
			cp, err := p.Audit.LatestCheckpoint(ctx, tx, p.systemTenantID())
			switch {
			case err == nil:
				auditState.LastCheckpoint = cp.Seq
			case errors.Is(err, storage.ErrNotFound):
				if head.Seq == 0 {
					auditState.Status = "no-entries"
				}
			default:
				return err
			}
			return nil
		}); err != nil {
			storageOK = "error: " + err.Error()
			auditState.Status = "error"
		}

		status := http.StatusOK
		body := healthResponse{
			Status: "ok",
			Components: componentStatuses{
				Storage:   storageOK,
				EventBus:  "ok",
				Scheduler: "ok",
				Signer:    p.Signer.KeyID(),
			},
			Audit: auditState,
		}
		if storageOK != "ok" {
			body.Status = "degraded"
			status = http.StatusServiceUnavailable
		}

		// E25 — HA 활성 시 role/epoch/leaderId 노출. LB가 follower 자동 제외에 사용.
		if p.HA != nil {
			st := p.HA.State()
			ha := &haHealth{
				Enabled:  st.Enabled,
				Role:     st.Role.String(),
				Epoch:    st.Epoch,
				LeaderID: st.LeaderID,
			}
			if !st.LastHeartbeatAt.IsZero() {
				ha.LastHeartbeatAt = st.LastHeartbeatAt.UTC().Format(time.RFC3339)
			}
			body.HA = ha
		}

		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

// newMux는 /healthz + /api/v1/* 핸들러를 mount한 http.Handler를 반환합니다.
//
// E9 Stage B 결선:
//   - /healthz: 기존 healthHandler 유지 (stdlib mux)
//   - /api/v1/*: chi 라우터에 handlers.Mount — Login·Me·ListRobots·StartScan·ListReports
//     5개 endpoint + 미구현 endpoint 자동 501.
//
// 반환 타입은 http.Handler — http.Server.Handler가 interface이므로 호환 유지.
func newMux(p *Platform) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(p))

	// E9 Stage B — chi 라우터로 API mount.
	apiRouter := chi.NewRouter()
	// E25 Stage 3 — HA 활성 시 write request leader gate (method 기반).
	// rp == nil이면 미들웨어가 모든 request 통과 — single-instance 호환.
	if p.HA != nil {
		apiRouter.Use(handlers.RequireLeaderForWrites(p.HA))
	}
	// E-MR (Phase 8) Stage 2 — standby-mode middleware (write 차단).
	// cfg.ReplicationConfig.Enabled=false (default)면 middleware가 no-op — single-region
	// 호환. Enabled=true + Role=standby면 POST/PUT/PATCH/DELETE → 409 Conflict.
	apiRouter.Use(replication.StandbyReadOnlyMiddleware(p.ReplicationConfig))

	h := handlers.New(handlers.Deps{
		Storage:           p.Storage,
		Clock:             p.Clock,
		Tenant:            p.Tenant,
		Robot:             p.Robot,
		FleetScanSched:    p.FleetScanSched,
		Scan:              p.Scan,
		ScanRun:           p.ScanRun,
		Benchmark:         p.Benchmark,
		Reporting:         p.Reporting,
		Insight:           p.Insight,
		Compliance:        p.Compliance,
		Advisor:           p.Advisor,
		Audit:             p.Audit,
		EventBus:          p.EventBus,
		License:           p.License,
		Webhook:           p.Webhook,
		WebhookDispatcher: p.WebhookDispatcher,
		SSO:               p.SSO,
		SSOGroupMapping:   p.SSOGroupMapping, // RBAC fleet 정밀화 Stage 5 — group → role 자동 sync.
		Invitation:        p.Invitation,
		Metrics:           p.Metrics,
		ReportSigner:      p.ReportSigner,
		Intake:            p.Intake, // Phase 6 후보 1 R1 Stage 3 — customer intake handler 결선 (운영자 admin gate).

		// E-MR (Phase 8) Stage 1~2 — Multi-region HA 결선.
		// Replication repo: sqlite·PG 양쪽에서 동일한 `?` placeholder SQL.
		// ReplicationConfig: bootstrap에서 env override 로드 — Enabled=false면 endpoint 503/no-op.
		Replication:       p.Replication,
		ReplicationConfig: p.ReplicationConfig,
		// RBAC fleet 정밀화 Stage 3 + Stage 6 closing — robot/scan/insight/reporting service
		// 위임 ScopeResolver. cross-resource fleet lookup이 필요한 7 mutation endpoint(DELETE
		// /robots/{id}, POST /robots/{id}/credential:rotate, POST /scans/{id}:cancel,
		// POST /reports/{id}:verify, POST /insights/{id}:dismiss 등)에서 사용.
		// tenant scope 격리는 storage.Tx 진입에서 자동 적용 (ctx의 TenantID).
		ScopeResolver: newScopeResolver(p.Storage, p.Robot, p.Scan, p.Insight, p.Reporting),
	})
	h.Mount(apiRouter)

	// B7 후속 — GET /api/v1/backups + GET /api/v1/backups/{filename}/download.
	// chi 직접 mount + AuthMiddleware. RBAC Stage 2: list는 모든 인증 사용자, download는
	// admin/auditor만(감사 자료 다운로드는 권한 영역).
	apiRouter.Group(func(r chi.Router) {
		r.Use(h.AuthMiddleware)
		r.Get("/api/v1/backups", listBackupsHandler(p))
		r.Group(func(r chi.Router) {
			r.Use(h.RequireRole("admin", "auditor"))
			r.Get("/api/v1/backups/{filename}/download", downloadBackupHandler(p))
		})
	})

	mux.Handle("/api/v1/", apiRouter)

	// E10 Stage D — Web Console 정적 자산 서빙 (R12-11 single binary).
	// 빌드 결과 부재(`make web-build` 미실행)는 graceful skip — /api는 계속 동작,
	// /과 /assets/* 만 503/안내 메시지 반환.
	if webHandler, err := webembed.Handler(); err == nil {
		mux.Handle("/", webHandler)
	} else {
		// dist 미빌드 — 진단용 fallback (production에서는 build 시 항상 존재).
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("rosshield Web Console not built — run `make web-build` then rebuild server\n"))
		})
	}

	return mux
}

// defaultDataDir은 ~/.rosshield 또는 임시 fallback을 반환합니다.
func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "rosshield")
	}
	return filepath.Join(home, ".rosshield")
}

func main() {
	// `report` 서브커맨드 분기 — 서버 부팅 없이 오프라인 검증만 수행.
	// 사용 예: rosshield-server report verify report.tar.gz
	if len(os.Args) > 1 && os.Args[1] == "report" {
		os.Exit(reportSubcommand(os.Args[2:]))
	}
	// `seed` 서브커맨드 분기 — 부팅 직후 system tenant + admin user 시드 (Phase 1 Exit 데모).
	// 사용 예: rosshield-server seed admin --email admin@example.com --password verylongpassword1
	if len(os.Args) > 1 && os.Args[1] == "seed" {
		os.Exit(seedSubcommand(os.Args[2:]))
	}
	// `backup` 서브커맨드 분기 — DataDir 일관 스냅샷 tar.gz 생성 (E28).
	// 사용 예: rosshield-server backup --output /backups/2026-05-08.tar.gz [--skip-evidence]
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		os.Exit(backupSubcommand(os.Args[2:]))
	}
	// `restore` 서브커맨드 분기 — backup tar.gz를 빈 DataDir에 복원 (E28).
	// 사용 예: rosshield-server restore --input /backups/2026-05-08.tar.gz --data-dir /var/lib/rosshield
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		os.Exit(restoreSubcommand(os.Args[2:]))
	}

	addr := flag.String("addr", "127.0.0.1:0", "bind address")
	metricsAddr := flag.String("metrics-addr", "", "Prometheus /metrics bind address (e.g. 127.0.0.1:9090). Empty = disabled (E27 — opt-in).")
	dataDir := flag.String("data-dir", defaultDataDir(), "data directory (SQLite DB, keys, etc.)")
	storageDriver := flag.String("storage", "sqlite", "storage driver: sqlite (default) | postgres")
	storageDSN := flag.String("storage-dsn", "", "storage DSN. SQLite ignores (uses data-dir/data.db). Postgres requires postgres://user:pass@host:port/db")
	llmProvider := flag.String("llm-provider", "", "LLM provider: noop (default) | ollama | vllm | anthropic — opt-in (R14-1). vllm is OpenAI-compatible self-hosted (D-LLM-1).")
	llmModel := flag.String("llm-model", "", "LLM model name (provider-specific, e.g. llama3.2 / meta-llama/Llama-3.1-8B-Instruct / claude-haiku-4-5-20251001)")
	llmBaseURL := flag.String("llm-base-url", "", "LLM base URL (ollama daemon / vllm endpoint / Anthropic API base)")
	llmAPIKey := flag.String("llm-api-key", "", "LLM API key (anthropic required, vllm optional Bearer — prefer env ANTHROPIC_API_KEY or ROSSHIELD_LLM_API_KEY)")
	llmTimeout := flag.Duration("llm-timeout", 0, "LLM request timeout (0 = adapter default)")
	llmMaxTokens := flag.Int("llm-max-tokens", 0, "vllm response token cap (0 = adapter default 1024). Anthropic uses CompleteRequest.MaxTokens directly.")
	llmKeepAlive := flag.Duration("llm-keep-alive", 0, "ollama model memory retention (0 = default 5m; <0 = immediate unload).")
	llmAutoPull := flag.Bool("llm-auto-pull", false, "ollama AutoPull (D-LLM-7). false = airgap safe default; true = allow PullModel on boot.")
	licenseToken := flag.String("license-token", "", "Enterprise license token (E24 — opt-in). Prefer env ROSSHIELD_LICENSE_TOKEN.")
	licensePubHex := flag.String("license-pubkey-hex", "", "Ed25519 public key (32B hex) for license verification. Prefer env ROSSHIELD_LICENSE_PUBKEY_HEX.")
	webhookTick := flag.Duration("webhook-tick-interval", 0, "Webhook dispatcher polling interval (E23-B). 0 = default 30s.")
	emailProvider := flag.String("email-provider", "noop", "Email provider for invite delivery (O6): noop (default — log only via slog Info, no SMTP) | smtp")
	smtpHost := flag.String("email-smtp-host", "", "SMTP server host (required when --email-provider=smtp)")
	smtpPort := flag.Int("email-smtp-port", 587, "SMTP server port (default 587)")
	smtpUser := flag.String("email-smtp-user", "", "SMTP username for AUTH PLAIN (empty = unauthenticated submission)")
	smtpPassword := flag.String("email-smtp-password", "", "SMTP password for AUTH PLAIN. Prefer env ROSSHIELD_SMTP_PASSWORD.")
	smtpFrom := flag.String("email-from", "", "Email From header (e.g. 'rosshield <noreply@example.com>'). Required when --email-provider=smtp.")
	publicBaseURL := flag.String("public-base-url", "", "Public base URL used to build invite accept links (e.g. https://app.example.com). Empty = Notifier receives empty acceptURL.")
	haEnabled := flag.Bool("ha-enabled", false, "Enable HA leader-election (E25, R30-2 = PG advisory lock). Requires --storage=postgres. Default: single-instance.")
	haLockID := flag.Int64("ha-lock-id", 0, "PG advisory lock ID for leader-election (E25). 0 = default 12345. Single value per cluster.")
	haHeartbeat := flag.Duration("ha-heartbeat-interval", 0, "HA heartbeat interval (E25). 0 = default 5s.")
	haLeaderID := flag.String("ha-leader-id", "", "HA instance identifier (E25). Empty = auto (hostname:pid).")
	haAdvertised := flag.String("ha-advertised-addr", "", "HA advertised URL for redirect from followers (E25, optional).")
	// E-MR Stage 3 — PG logical replication 자동 setup (env override 권장).
	replicationAutoSetup := flag.Bool("replication-auto-setup", false, "Auto-create PG PUBLICATION/SUBSCRIPTION on boot (E-MR Stage 3). Requires --storage=postgres + replication.Enabled=true. Default: false (operators provision manually).")
	replicationPubName := flag.String("replication-publication-name", "", "PUBLICATION name for primary role. Empty = default 'rosshield_main'.")
	replicationPubAllTables := flag.Bool("replication-publication-all-tables", true, "PUBLICATION FOR ALL TABLES (recommended — auto-includes new tables). false = use explicit table list (advanced).")
	replicationSubName := flag.String("replication-subscription-name", "", "SUBSCRIPTION name for standby role. Empty = default 'rosshield_main_sub'.")
	replicationPrimaryConnStr := flag.String("replication-primary-conn-string", "", "Standby PG conn string to primary (required when --replication-auto-setup=true + role=standby). Prefer env ROSSHIELD_REPLICATION_PRIMARY_CONN_STRING.")
	keystoreType := flag.String("keystore", "file", "KeyStore adapter (E34): file (default, soft.LoadOrCreate) | tpm (Stage 1 placeholder — Stage 2+ TPM 2.0 PCR-sealed).")
	backupSchedule := flag.String("backup-schedule", "", "Auto backup cron spec (B7 후속). Empty = disabled. Examples: '@every 24h', '0 15 3 * * *' (daily 03:15 UTC).")
	backupDir := flag.String("backup-dir", "", "Auto backup output directory (B7 후속). Empty = <data-dir>/backups.")
	backupSkipEvidence := flag.Bool("backup-skip-evidence", false, "Auto backup excludes evidence/ (faster, smaller, metadata-only).")
	auditRotationSchedule := flag.String("audit-rotation-schedule", "", "Audit chain rotation cron spec (E32 Stage 6). Empty = disabled (manual API only). Examples: '@every 720h' (monthly), '0 0 1 * *' (1st day of each month). HA: leader-only.")
	replicationSlotCleanupSchedule := flag.String("replication-slot-cleanup-schedule", "", "PG replication slot cleanup cron spec (E-MR Stage 3 후속). Empty = disabled. Example: '@every 24h'. PG + primary role + Enabled=true 조합에서만 활성. HA: leader-only.")
	replicationSlotCleanupPrefix := flag.String("replication-slot-cleanup-prefix", "rosshield_", "Safety prefix — only slots whose name starts with this string are cleanup candidates. Empty/whitespace value disables the cron job. Env: ROSSHIELD_REPLICATION_SLOT_CLEANUP_PREFIX.")
	replicationSlotCleanupMinInactiveAge := flag.Duration("replication-slot-cleanup-min-inactive-age", 24*time.Hour, "Slots inactive longer than this duration are cleanup candidates. Env: ROSSHIELD_REPLICATION_SLOT_CLEANUP_MIN_INACTIVE_AGE.")
	replicationSlotCleanupDryRun := flag.Bool("replication-slot-cleanup-dry-run", false, "When true, log cleanup candidates but do not drop them. Env: ROSSHIELD_REPLICATION_SLOT_CLEANUP_DRY_RUN=1.")
	cosignEnabled := flag.Bool("cosign-enabled", false, "Enable cosign keyless signing of audit rotation archives (D-AR-4). Requires cosign binary on PATH or via --cosign-binary. Default: disabled (airgap-friendly).")
	cosignBinary := flag.String("cosign-binary", "", "Path to cosign binary (D-AR-4). Empty = 'cosign' from PATH.")
	cosignIdentity := flag.String("cosign-identity", "", "OIDC identity expected for cosign keyless signing (e.g. ci@example.com). Used by verify CLI as --certificate-identity; recorded in operator logs.")
	cosignFulcioURL := flag.String("cosign-fulcio-url", "", "Fulcio CA URL for cosign keyless. Empty = Sigstore public Fulcio.")
	cosignRekorURL := flag.String("cosign-rekor-url", "", "Rekor transparency log URL for cosign keyless. Empty = Sigstore public Rekor.")
	auditColdBackend := flag.String("audit-cold-backend", "", "Audit rotation cold backend (D-AR-9): '' or 'file' (default, <data-dir>/audit-archives, Apache-2.0) | 's3' (BSL 1.1 enterprise — build with rosshield_enterprise tag). Env: ROSSHIELD_AUDIT_COLD_BACKEND.")
	auditS3Bucket := flag.String("audit-s3-bucket", "", "S3 bucket for audit cold archives (required when --audit-cold-backend=s3). Env: ROSSHIELD_AUDIT_S3_BUCKET.")
	auditS3Region := flag.String("audit-s3-region", "", "AWS region for audit S3 backend (required when --audit-cold-backend=s3). Env: ROSSHIELD_AUDIT_S3_REGION.")
	auditS3Prefix := flag.String("audit-s3-prefix", "", "Key prefix inside the S3 bucket (e.g. 'audit-archives/tn_acme/'). Empty = bucket root. Env: ROSSHIELD_AUDIT_S3_PREFIX.")
	auditS3Endpoint := flag.String("audit-s3-endpoint", "", "S3-compatible endpoint URL for MinIO/Wasabi/Backblaze B2. Empty = AWS default. Env: ROSSHIELD_AUDIT_S3_ENDPOINT.")
	auditS3ForcePathStyle := flag.Bool("audit-s3-force-path-style", false, "Force path-style addressing (required by MinIO and some self-hosted gateways). Env: ROSSHIELD_AUDIT_S3_FORCE_PATH_STYLE=1.")
	auditS3SSE := flag.String("audit-s3-sse", "", "S3 server-side encryption mode: 'AES256' or 'aws:kms'. Empty = no SSE. Env: ROSSHIELD_AUDIT_S3_SSE.")
	auditS3KMSKeyID := flag.String("audit-s3-kms-key-id", "", "SSE-KMS CMK ARN/ID (used when --audit-s3-sse=aws:kms). Env: ROSSHIELD_AUDIT_S3_KMS_KEY_ID.")
	auditS3LifecycleEnabled := flag.Bool("audit-s3-lifecycle-enabled", false, "Apply S3 bucket lifecycle policy on backend init (D-AR-9 후속). Env: ROSSHIELD_AUDIT_S3_LIFECYCLE_ENABLED=1. Rule ID 'rosshield-rotation', Filter.Prefix=--audit-s3-prefix. idempotent.")
	auditS3LifecycleIADays := flag.Int("audit-s3-lifecycle-transition-ia-days", 0, "STANDARD → STANDARD_IA 전환 일수. 0 = 단계 비활성. Env: ROSSHIELD_AUDIT_S3_LIFECYCLE_TRANSITION_IA_DAYS.")
	auditS3LifecycleGlacierDays := flag.Int("audit-s3-lifecycle-transition-glacier-days", 0, "STANDARD → GLACIER 전환 일수. 0 = 단계 비활성. Env: ROSSHIELD_AUDIT_S3_LIFECYCLE_TRANSITION_GLACIER_DAYS. MinIO 등은 silent ignore.")
	auditS3LifecycleDeepArchiveDays := flag.Int("audit-s3-lifecycle-transition-deep-archive-days", 0, "STANDARD → DEEP_ARCHIVE 전환 일수. 0 = 단계 비활성. Env: ROSSHIELD_AUDIT_S3_LIFECYCLE_TRANSITION_DEEP_ARCHIVE_DAYS.")
	auditS3LifecycleExpireDays := flag.Int("audit-s3-lifecycle-expire-days", 0, "Object 만료 일수 (S3 lifecycle Expiration). 0 = 영구 보존. Env: ROSSHIELD_AUDIT_S3_LIFECYCLE_EXPIRE_DAYS.")
	checkTimeoutDefaultSec := flag.Int("check-timeout-default-sec", 0, "Default SSH exec timeout for checks with TimeoutSec=0. 0 uses scan.DefaultCheckTimeoutSec (10s). Per-check TimeoutSec always wins.")
	flag.Parse()

	// API key fallback to env to avoid leaking on shell history.
	// 우선순위: flag → ROSSHIELD_LLM_API_KEY (provider-neutral) → ANTHROPIC_API_KEY (legacy).
	apiKey := *llmAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ROSSHIELD_LLM_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	// LLM private deployment env overrides (D-LLM-1·D-LLM-5·D-LLM-7).
	// flag가 default(빈 값/0/false)일 때만 env로 덮어쓰기 — flag 우선.
	llmProviderVal := *llmProvider
	if llmProviderVal == "" {
		llmProviderVal = os.Getenv("ROSSHIELD_LLM_PROVIDER")
	}
	llmBaseURLVal := *llmBaseURL
	if llmBaseURLVal == "" {
		llmBaseURLVal = os.Getenv("ROSSHIELD_LLM_BASE_URL")
	}
	llmModelVal := *llmModel
	if llmModelVal == "" {
		llmModelVal = os.Getenv("ROSSHIELD_LLM_MODEL")
	}
	llmTimeoutVal := *llmTimeout
	if llmTimeoutVal == 0 {
		if s := os.Getenv("ROSSHIELD_LLM_TIMEOUT_SEC"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				llmTimeoutVal = time.Duration(n) * time.Second
			}
		}
	}
	llmMaxTokensVal := *llmMaxTokens
	if llmMaxTokensVal == 0 {
		if s := os.Getenv("ROSSHIELD_LLM_MAX_TOKENS"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				llmMaxTokensVal = n
			}
		}
	}
	llmKeepAliveVal := *llmKeepAlive
	if llmKeepAliveVal == 0 {
		if s := os.Getenv("ROSSHIELD_LLM_KEEP_ALIVE_SEC"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n != 0 {
				llmKeepAliveVal = time.Duration(n) * time.Second
			}
		}
	}
	llmAutoPullVal := *llmAutoPull
	if !llmAutoPullVal {
		if s := os.Getenv("ROSSHIELD_LLM_AUTO_PULL"); s == "1" || strings.EqualFold(s, "true") {
			llmAutoPullVal = true
		}
	}
	// License token/pubkey도 env fallback (운영에선 secret manager 권장).
	licTok := *licenseToken
	if licTok == "" {
		licTok = os.Getenv("ROSSHIELD_LICENSE_TOKEN")
	}
	licPub := *licensePubHex
	if licPub == "" {
		licPub = os.Getenv("ROSSHIELD_LICENSE_PUBKEY_HEX")
	}
	// SMTP password env fallback (avoid shell history leak).
	smtpPw := *smtpPassword
	if smtpPw == "" {
		smtpPw = os.Getenv("ROSSHIELD_SMTP_PASSWORD")
	}

	// D-AR-4 cosign keyless env fallback.
	// 우선순위: flag → ROSSHIELD_COSIGN_*. flag default(false / "") 일 때만 env 적용.
	cosignCfg := resolveCosignConfig(*cosignEnabled, *cosignBinary, *cosignIdentity, *cosignFulcioURL, *cosignRekorURL)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	bootCtx, cancelBoot := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelBoot()

	// Storage DSN — env fallback (PG: ROSSHIELD_DATABASE_URL).
	dsn := *storageDSN
	if dsn == "" {
		dsn = os.Getenv("ROSSHIELD_DATABASE_URL")
	}

	// E-MR (Phase 8) — Multi-region HA config from env.
	// 모든 env 미설정 시 single-region default (Enabled=false) — 기존 배포 동작 그대로.
	replicationCfg, replErr := replication.LoadConfigFromEnv()
	if replErr != nil {
		logger.Error("replication config from env failed", "err", replErr.Error())
		os.Exit(1)
	}

	// E-MR Stage 3 — auto-setup env override (flag default 시에만 env).
	// 비밀 정보(conn string)는 env-only가 권장 — flag 사용 시 process 목록 노출.
	replAutoSetup := *replicationAutoSetup
	if !replAutoSetup {
		if s := os.Getenv("ROSSHIELD_REPLICATION_AUTO_SETUP"); s == "1" || strings.EqualFold(s, "true") {
			replAutoSetup = true
		}
	}
	replPubName := *replicationPubName
	if replPubName == "" {
		replPubName = os.Getenv("ROSSHIELD_REPLICATION_PUBLICATION_NAME")
	}
	replPubAllTables := *replicationPubAllTables
	if s := os.Getenv("ROSSHIELD_REPLICATION_PUBLICATION_ALL_TABLES"); s != "" {
		switch strings.ToLower(s) {
		case "1", "true", "yes", "on":
			replPubAllTables = true
		case "0", "false", "no", "off":
			replPubAllTables = false
		default:
			logger.Error("invalid ROSSHIELD_REPLICATION_PUBLICATION_ALL_TABLES", "value", s)
			os.Exit(1)
		}
	}
	replSubName := *replicationSubName
	if replSubName == "" {
		replSubName = os.Getenv("ROSSHIELD_REPLICATION_SUBSCRIPTION_NAME")
	}
	replPrimaryConnStr := *replicationPrimaryConnStr
	if replPrimaryConnStr == "" {
		replPrimaryConnStr = os.Getenv("ROSSHIELD_REPLICATION_PRIMARY_CONN_STRING")
	}

	platform, err := Bootstrap(bootCtx, Config{
		DataDir:                               *dataDir,
		Logger:                                logger,
		StorageDriver:                         *storageDriver,
		StorageDSN:                            dsn,
		LLMProvider:                           llmProviderVal,
		LLMModel:                              llmModelVal,
		LLMBaseURL:                            llmBaseURLVal,
		LLMAPIKey:                             apiKey,
		LLMTimeout:                            llmTimeoutVal,
		LLMMaxTokens:                          llmMaxTokensVal,
		LLMKeepAlive:                          llmKeepAliveVal,
		LLMAutoPull:                           llmAutoPullVal,
		LicenseToken:                          licTok,
		LicensePublicKeyHex:                   licPub,
		WebhookTickInterval:                   *webhookTick,
		EmailProvider:                         *emailProvider,
		SMTPHost:                              *smtpHost,
		SMTPPort:                              *smtpPort,
		SMTPUsername:                          *smtpUser,
		SMTPPassword:                          smtpPw,
		SMTPFrom:                              *smtpFrom,
		PublicBaseURL:                         *publicBaseURL,
		HAEnabled:                             *haEnabled,
		HALockID:                              *haLockID,
		HAHeartbeatInterval:                   *haHeartbeat,
		HALeaderID:                            *haLeaderID,
		HAAdvertisedAddr:                      *haAdvertised,
		ReplicationConfig:                     replicationCfg,
		ReplicationAutoSetup:                  replAutoSetup,
		ReplicationPublicationName:            replPubName,
		ReplicationPublicationAllTables:       replPubAllTables,
		ReplicationSubscriptionName:           replSubName,
		ReplicationPrimaryConnString:          replPrimaryConnStr,
		KeystoreType:                          *keystoreType,
		BackupSchedule:                        *backupSchedule,
		BackupDir:                             *backupDir,
		BackupSkipEvidence:                    *backupSkipEvidence,
		AuditRotationSchedule:                 resolveAuditRotationSchedule(*auditRotationSchedule),
		ReplicationSlotCleanupSchedule:        resolveEnvFallback(*replicationSlotCleanupSchedule, "ROSSHIELD_REPLICATION_SLOT_CLEANUP_SCHEDULE"),
		ReplicationSlotCleanupPrefix:          resolveEnvFallback(*replicationSlotCleanupPrefix, "ROSSHIELD_REPLICATION_SLOT_CLEANUP_PREFIX"),
		ReplicationSlotCleanupMinInactiveAge:  *replicationSlotCleanupMinInactiveAge,
		ReplicationSlotCleanupDryRun:          resolveBoolEnvFallback(*replicationSlotCleanupDryRun, "ROSSHIELD_REPLICATION_SLOT_CLEANUP_DRY_RUN"),
		CosignEnabled:                         cosignCfg.Enabled,
		CosignBinaryPath:                      cosignCfg.BinaryPath,
		CosignIdentity:                        cosignCfg.Identity,
		CosignFulcioURL:                       cosignCfg.FulcioURL,
		CosignRekorURL:                        cosignCfg.RekorURL,
		AuditColdBackend:                      resolveEnvFallback(*auditColdBackend, "ROSSHIELD_AUDIT_COLD_BACKEND"),
		AuditS3Bucket:                         resolveEnvFallback(*auditS3Bucket, "ROSSHIELD_AUDIT_S3_BUCKET"),
		AuditS3Region:                         resolveEnvFallback(*auditS3Region, "ROSSHIELD_AUDIT_S3_REGION"),
		AuditS3Prefix:                         resolveEnvFallback(*auditS3Prefix, "ROSSHIELD_AUDIT_S3_PREFIX"),
		AuditS3Endpoint:                       resolveEnvFallback(*auditS3Endpoint, "ROSSHIELD_AUDIT_S3_ENDPOINT"),
		AuditS3ForcePathStyle:                 resolveBoolEnvFallback(*auditS3ForcePathStyle, "ROSSHIELD_AUDIT_S3_FORCE_PATH_STYLE"),
		AuditS3SSE:                            resolveEnvFallback(*auditS3SSE, "ROSSHIELD_AUDIT_S3_SSE"),
		AuditS3KMSKeyID:                       resolveEnvFallback(*auditS3KMSKeyID, "ROSSHIELD_AUDIT_S3_KMS_KEY_ID"),
		AuditS3LifecycleEnabled:               resolveBoolEnvFallback(*auditS3LifecycleEnabled, "ROSSHIELD_AUDIT_S3_LIFECYCLE_ENABLED"),
		AuditS3LifecycleTransitionIADays:      int32(resolveIntEnvFallback(*auditS3LifecycleIADays, "ROSSHIELD_AUDIT_S3_LIFECYCLE_TRANSITION_IA_DAYS")),
		AuditS3LifecycleTransitionGlacierDays: int32(resolveIntEnvFallback(*auditS3LifecycleGlacierDays, "ROSSHIELD_AUDIT_S3_LIFECYCLE_TRANSITION_GLACIER_DAYS")),
		AuditS3LifecycleTransitionDeepArchiveDays: int32(resolveIntEnvFallback(*auditS3LifecycleDeepArchiveDays, "ROSSHIELD_AUDIT_S3_LIFECYCLE_TRANSITION_DEEP_ARCHIVE_DAYS")),
		AuditS3LifecycleExpireDays:                int32(resolveIntEnvFallback(*auditS3LifecycleExpireDays, "ROSSHIELD_AUDIT_S3_LIFECYCLE_EXPIRE_DAYS")),
		CheckTimeoutDefaultSec:                    *checkTimeoutDefaultSec,
	})
	if err != nil {
		logger.Error("bootstrap failed", "err", err.Error())
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           newMux(platform),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// E27 — Prometheus /metrics 별 mux + 별 서버 (옵트인 --metrics-addr).
	// API mux와 격리해 외부 노출 risk 분리. 보통 internal network bind (127.0.0.1).
	var metricsSrv *http.Server
	if *metricsAddr != "" && platform.Metrics != nil {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", platform.Metrics.Handler())
		metricsSrv = &http.Server{
			Addr:              *metricsAddr,
			Handler:           metricsMux,
			ReadHeaderTimeout: 10 * time.Second,
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("server starting", "addr", *addr, "dataDir", *dataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "err", err.Error())
			stop <- syscall.SIGTERM
		}
	}()

	if metricsSrv != nil {
		go func() {
			logger.Info("metrics server starting", "addr", *metricsAddr)
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics server failed", "err", err.Error())
			}
		}()
	}

	<-stop
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error("metrics shutdown error", "err", err.Error())
		}
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown error", "err", err.Error())
	}
	if err := platform.Shutdown(shutdownCtx); err != nil {
		logger.Error("platform shutdown error", "err", err.Error())
	}
}

// resolveAuditRotationSchedule은 flag 값이 비어있으면 env로 fallback합니다 (E32 Stage 6).
//
// 우선순위: flag → ROSSHIELD_AUDIT_ROTATION_SCHEDULE.
// 두 값 모두 빈 값이면 자동 rotation 비활성 (manual API only) — bootstrap에서 no-op.
//
// 본 함수는 단순 분기만 — env에 값이 있으면 그대로 사용 (cron spec validation은 cronsched가
// register 시점에 수행).
func resolveAuditRotationSchedule(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv("ROSSHIELD_AUDIT_ROTATION_SCHEDULE")
}

// resolveCosignConfig는 flag 값 5건을 ROSSHIELD_COSIGN_* env로 fallback합니다 (D-AR-4).
func resolveCosignConfig(flagEnabled bool, flagBinary, flagIdentity, flagFulcio, flagRekor string) rotation.SignerConfig {
	envCfg := rotation.LoadSignerConfigFromEnv()
	out := rotation.SignerConfig{
		Enabled:    flagEnabled,
		BinaryPath: flagBinary,
		Identity:   flagIdentity,
		FulcioURL:  flagFulcio,
		RekorURL:   flagRekor,
	}
	if !out.Enabled {
		out.Enabled = envCfg.Enabled
	}
	if out.BinaryPath == "" {
		out.BinaryPath = envCfg.BinaryPath
	}
	if out.Identity == "" {
		out.Identity = envCfg.Identity
	}
	if out.FulcioURL == "" {
		out.FulcioURL = envCfg.FulcioURL
	}
	if out.RekorURL == "" {
		out.RekorURL = envCfg.RekorURL
	}
	return out
}

// resolveEnvFallback는 flag 값이 비어있으면 env로 fallback합니다 (D-AR-9).
func resolveEnvFallback(flagVal, envKey string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv(envKey)
}

// resolveBoolEnvFallback는 flag가 false일 때만 env로 fallback합니다.
func resolveBoolEnvFallback(flagVal bool, envKey string) bool {
	if flagVal {
		return true
	}
	v := os.Getenv(envKey)
	return v == "1" || strings.EqualFold(v, "true")
}

// resolveIntEnvFallback는 flag가 0이면 env로 fallback해 정수로 파싱합니다.
//
// env 값이 비어있거나 파싱 실패면 0 반환 (silent — log은 호출자가 별도 처리).
// 음수는 음수 그대로 반환 — 호출자 측 의미론에 따라 검증.
func resolveIntEnvFallback(flagVal int, envKey string) int {
	if flagVal != 0 {
		return flagVal
	}
	v := strings.TrimSpace(os.Getenv(envKey))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}
