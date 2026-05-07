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
	h := handlers.New(handlers.Deps{
		Storage:    p.Storage,
		Clock:      p.Clock,
		Tenant:     p.Tenant,
		Robot:      p.Robot,
		Scan:       p.Scan,
		Reporting:  p.Reporting,
		Insight:    p.Insight,
		Compliance: p.Compliance,
		Advisor:    p.Advisor,
		Audit:      p.Audit,
		EventBus:   p.EventBus,
		License:    p.License,
		Webhook:    p.Webhook,
	})
	h.Mount(apiRouter)
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

	addr := flag.String("addr", "127.0.0.1:0", "bind address")
	dataDir := flag.String("data-dir", defaultDataDir(), "data directory (SQLite DB, keys, etc.)")
	llmProvider := flag.String("llm-provider", "", "LLM provider: noop (default) | ollama | anthropic — opt-in (R14-1)")
	llmModel := flag.String("llm-model", "", "LLM model name (provider-specific, e.g. llama3.2 / claude-haiku-4-5-20251001)")
	llmBaseURL := flag.String("llm-base-url", "", "LLM base URL (ollama daemon or Anthropic API base)")
	llmAPIKey := flag.String("llm-api-key", "", "LLM API key (anthropic only — prefer env ANTHROPIC_API_KEY)")
	llmTimeout := flag.Duration("llm-timeout", 0, "LLM request timeout (0 = adapter default)")
	licenseToken := flag.String("license-token", "", "Enterprise license token (E24 — opt-in). Prefer env ROSSHIELD_LICENSE_TOKEN.")
	licensePubHex := flag.String("license-pubkey-hex", "", "Ed25519 public key (32B hex) for license verification. Prefer env ROSSHIELD_LICENSE_PUBKEY_HEX.")
	webhookTick := flag.Duration("webhook-tick-interval", 0, "Webhook dispatcher polling interval (E23-B). 0 = default 30s.")
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	bootCtx, cancelBoot := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelBoot()

	platform, err := Bootstrap(bootCtx, Config{
		DataDir:             *dataDir,
		Logger:              logger,
		LLMProvider:         *llmProvider,
		LLMModel:            *llmModel,
		LLMBaseURL:          *llmBaseURL,
		LLMAPIKey:           apiKey,
		LLMTimeout:          *llmTimeout,
		LicenseToken:        licTok,
		LicensePublicKeyHex: licPub,
		WebhookTickInterval: *webhookTick,
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

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("server starting", "addr", *addr, "dataDir", *dataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "err", err.Error())
			stop <- syscall.SIGTERM
		}
	}()

	<-stop
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown error", "err", err.Error())
	}
	if err := platform.Shutdown(shutdownCtx); err != nil {
		logger.Error("platform shutdown error", "err", err.Error())
	}
}
