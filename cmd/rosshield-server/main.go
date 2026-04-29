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

	"github.com/ssabro/rosshield/internal/platform/storage"
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

func newMux(p *Platform) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(p))
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

	addr := flag.String("addr", "127.0.0.1:0", "bind address")
	dataDir := flag.String("data-dir", defaultDataDir(), "data directory (SQLite DB, keys, etc.)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	bootCtx, cancelBoot := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelBoot()

	platform, err := Bootstrap(bootCtx, Config{
		DataDir: *dataDir,
		Logger:  logger,
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
