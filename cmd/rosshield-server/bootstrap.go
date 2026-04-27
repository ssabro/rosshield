package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	benchmarkrepo "github.com/ssabro/rosshield/internal/domain/benchmark/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	robotrepo "github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	tenantrepo "github.com/ssabro/rosshield/internal/domain/tenant/sqliterepo"
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

	// SystemTenantID는 부팅 시 자동 등록되는 audit checkpoint 잡의 테넌트 식별자.
	// 빈 값이면 "system" 사용. 도메인 진입(E3 Tenant) 후에도 시스템 자체 액션은 이 테넌트.
	SystemTenantID storage.TenantID

	// CheckpointSpec은 audit checkpoint 잡의 cron spec.
	// 빈 값이면 "@every 1h" (§10.5 매시간 기본). 테스트에서 `@every 1s` 등으로 단축.
	CheckpointSpec string
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
	Audit     audit.Service
	Tenant    tenant.Service
	Benchmark benchmark.Service
	Robot     robot.Service

	systemTenant storage.TenantID

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

	robotSvc := robotrepo.New(robotrepo.Deps{
		Clock: clk,
		IDGen: ids,
		Audit: emitter,
	})

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
		"systemTenant", string(systemTenant),
		"checkpointSpec", checkpointSpec)

	return &Platform{
		Logger:       logger,
		Clock:        clk,
		IDGen:        ids,
		Storage:      store,
		EventBus:     bus,
		Signer:       sgn,
		Scheduler:    sch,
		Audit:        auditSvc,
		Tenant:       tenantSvc,
		Benchmark:    benchmarkSvc,
		Robot:        robotSvc,
		systemTenant: systemTenant,
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
