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
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/api/handlers"
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
	h := handlers.New(handlers.Deps{
		Storage:           p.Storage,
		Clock:             p.Clock,
		Tenant:            p.Tenant,
		Robot:             p.Robot,
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
		Invitation:        p.Invitation,
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
	llmProvider := flag.String("llm-provider", "", "LLM provider: noop (default) | ollama | anthropic — opt-in (R14-1)")
	llmModel := flag.String("llm-model", "", "LLM model name (provider-specific, e.g. llama3.2 / claude-haiku-4-5-20251001)")
	llmBaseURL := flag.String("llm-base-url", "", "LLM base URL (ollama daemon or Anthropic API base)")
	llmAPIKey := flag.String("llm-api-key", "", "LLM API key (anthropic only — prefer env ANTHROPIC_API_KEY)")
	llmTimeout := flag.Duration("llm-timeout", 0, "LLM request timeout (0 = adapter default)")
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
	keystoreType := flag.String("keystore", "file", "KeyStore adapter (E34): file (default, soft.LoadOrCreate) | tpm (Stage 1 placeholder — Stage 2+ TPM 2.0 PCR-sealed).")
	backupSchedule := flag.String("backup-schedule", "", "Auto backup cron spec (B7 후속). Empty = disabled. Examples: '@every 24h', '0 15 3 * * *' (daily 03:15 UTC).")
	backupDir := flag.String("backup-dir", "", "Auto backup output directory (B7 후속). Empty = <data-dir>/backups.")
	backupSkipEvidence := flag.Bool("backup-skip-evidence", false, "Auto backup excludes evidence/ (faster, smaller, metadata-only).")
	flag.Parse()

	// API key fallback to env to avoid leaking on shell history.
	apiKey := *llmAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	bootCtx, cancelBoot := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelBoot()

	// Storage DSN — env fallback (PG: ROSSHIELD_DATABASE_URL).
	dsn := *storageDSN
	if dsn == "" {
		dsn = os.Getenv("ROSSHIELD_DATABASE_URL")
	}

	platform, err := Bootstrap(bootCtx, Config{
		DataDir:             *dataDir,
		Logger:              logger,
		StorageDriver:       *storageDriver,
		StorageDSN:          dsn,
		LLMProvider:         *llmProvider,
		LLMModel:            *llmModel,
		LLMBaseURL:          *llmBaseURL,
		LLMAPIKey:           apiKey,
		LLMTimeout:          *llmTimeout,
		LicenseToken:        licTok,
		LicensePublicKeyHex: licPub,
		WebhookTickInterval: *webhookTick,
		EmailProvider:       *emailProvider,
		SMTPHost:            *smtpHost,
		SMTPPort:            *smtpPort,
		SMTPUsername:        *smtpUser,
		SMTPPassword:        smtpPw,
		SMTPFrom:            *smtpFrom,
		PublicBaseURL:       *publicBaseURL,
		HAEnabled:           *haEnabled,
		HALockID:            *haLockID,
		HAHeartbeatInterval: *haHeartbeat,
		HALeaderID:          *haLeaderID,
		HAAdvertisedAddr:    *haAdvertised,
		KeystoreType:        *keystoreType,
		BackupSchedule:      *backupSchedule,
		BackupDir:           *backupDir,
		BackupSkipEvidence:  *backupSkipEvidence,
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
