package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	builtinpacks "github.com/ssabro/rosshield/internal/builtin/packs"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// seedBuiltinPacks는 binary에 embed된 built-in pack을 systemTenant에 자동 install합니다 (E12 Stage E).
//
// idempotent: 이미 install된 pack은 ErrPackAlreadyInstalled로 silent skip. server 재부팅 시
// 매번 호출되지만 첫 부팅에만 실 INSERT.
//
// 비-fatal 호출자: 본 함수가 에러를 반환해도 server boot는 막지 않음 — 운영자가 별도로
// 수동 install 가능 (degraded mode). _archives/ 누락(ErrNoBuiltinsEmbedded)은 빌드 누락 신호.
//
// actor: "system-bootstrap" — audit log가 자동 seed 흐름을 식별 가능하게.
func seedBuiltinPacks(ctx context.Context, store storage.Storage, svc benchmark.Service,
	tenantID storage.TenantID, logger *slog.Logger) error {
	packs, err := builtinpacks.Builtins()
	if err != nil {
		return fmt.Errorf("seedBuiltinPacks: %w", err)
	}

	tenantCtx := storage.WithTenantID(ctx, tenantID)
	installed, skipped := 0, 0
	for _, p := range packs {
		err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
			_, e := svc.InstallPack(ctx, tx, tenantID, p.TarGz, p.PublicKey, p.SignerKeyID, "system-bootstrap")
			return e
		})
		switch {
		case err == nil:
			installed++
			logger.Info("seedBuiltinPacks: installed",
				"filename", p.Filename, "tenant", string(tenantID), "signerKeyId", p.SignerKeyID)
		case errors.Is(err, benchmark.ErrPackAlreadyInstalled):
			skipped++
		default:
			return fmt.Errorf("seedBuiltinPacks: install %s: %w", p.Filename, err)
		}
	}

	logger.Info("seedBuiltinPacks: done",
		"installed", installed, "skipped", skipped, "total", len(packs))
	return nil
}
