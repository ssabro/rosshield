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
// trust bundle: 각 SeedPack의 TrustBundle을 차례로 시도(dev → release). 첫 ErrSignatureInvalid
// 가 아닌 결과로 종료(success 또는 다른 에러). 모두 ErrSignatureInvalid면 archive 의심.
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
		matched, err := installWithTrustBundle(tenantCtx, store, svc, tenantID, p, logger)
		switch {
		case err == nil && matched:
			installed++
		case err == nil && !matched:
			// 모든 trust 키가 ErrSignatureInvalid — archive 의심.
			logger.Warn("seedBuiltinPacks: signature mismatch (all trust keys failed)",
				"filename", p.Filename, "trustEntries", len(p.TrustBundle))
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

// installWithTrustBundle은 trust bundle을 차례로 시도해 첫 통과 키로 InstallPack 합니다.
//
// 반환:
//   - (true, nil): 성공
//   - (true, ErrPackAlreadyInstalled): 이미 install (호출자가 silent skip)
//   - (false, nil): 모든 trust 키가 ErrSignatureInvalid (archive 의심, 호출자가 warn 로그)
//   - (_, err): 다른 에러 (호출자가 에러 wrap)
func installWithTrustBundle(ctx context.Context, store storage.Storage, svc benchmark.Service,
	tenantID storage.TenantID, p builtinpacks.SeedPack, logger *slog.Logger) (matched bool, err error) {
	for _, te := range p.TrustBundle {
		var installed benchmark.Pack
		txErr := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			pk, e := svc.InstallPack(ctx, tx, tenantID, p.TarGz, te.PublicKey, te.SignerKeyID, "system-bootstrap")
			if e != nil {
				return e
			}
			installed = pk
			return nil
		})
		switch {
		case txErr == nil:
			logger.Info("seedBuiltinPacks: installed",
				"filename", p.Filename, "tenant", string(tenantID),
				"signerKeyId", te.SignerKeyID, "packKey", installed.PackKey)
			return true, nil
		case errors.Is(txErr, benchmark.ErrPackAlreadyInstalled):
			return true, txErr
		case errors.Is(txErr, benchmark.ErrSignatureInvalid):
			// 다음 trust 키 시도.
			continue
		default:
			return false, txErr
		}
	}
	return false, nil
}
