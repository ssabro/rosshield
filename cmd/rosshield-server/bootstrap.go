package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// Config는 부트스트랩 입력입니다.
type Config struct {
	DataDir string       // SQLite 파일·키·로그 저장 디렉토리 (예: ~/.rosshield).
	Logger  *slog.Logger // nil이면 stdout JSON 핸들러로 자동 생성.
}

// Platform은 초기화된 모든 platform 서비스의 묶음입니다.
// 도메인 서비스는 이 구조체에서 필요한 의존성만 주입받습니다 (§03.4 시작 시퀀스).
type Platform struct {
	Logger    *slog.Logger
	Clock     clock.Clock
	IDGen     idgen.IDGen
	Storage   storage.Storage
	EventBus  eventbus.Bus
	Signer    signer.Signer
	Scheduler scheduler.Scheduler

	shutdownOnce sync.Once
	shutdownErr  error
	shutdown     bool
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

	sgn, err := soft.New()
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: signer: %w", err)
	}

	sch := cronsched.New(cronsched.Deps{Logger: logger})

	logger.Info("platform bootstrap complete",
		"dataDir", cfg.DataDir,
		"dbPath", dbPath,
		"signerKeyId", sgn.KeyID())

	return &Platform{
		Logger:    logger,
		Clock:     clk,
		IDGen:     ids,
		Storage:   store,
		EventBus:  bus,
		Signer:    sgn,
		Scheduler: sch,
	}, nil
}

// Shutdown은 platform 서비스를 역순으로 정상 종료합니다 (idempotent).
// Scheduler → EventBus → Storage 순. ctx 만료 시 ctx.Err() 반환.
func (p *Platform) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		var errs []error

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
