package main

// scope_resolver.go — RBAC fleet 정밀화 Stage 3 산출:
//
//   1. handlers.ScopeResolver 인터페이스 구체 구현 — robot/scan 도메인 Service 위임.
//   2. tenant scope 격리 — context.Context의 TenantID로 자동 격리 (storage.Tx 진입).
//   3. 미지원 resourceType은 빈 fleetID 반환 (D-RBACEX-9 일관 — middleware는 fleet binding
//      자연 deny).
//
// 본 구체는 cmd/rosshield-server에 위치 — handlers 패키지는 도메인 의존성 0(인터페이스만)
// 보존, application bootstrap이 robot/scan service를 주입합니다 (DDD 경계 §5).
//
// design doc `docs/design/notes/rbac-fleet-scope-precision-design.md` §7 Stage 3 + D-RBACEX-2
// 권장 default A(인터페이스 + 도메인 repo wrap) 정확 일치.

import (
	"context"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// scopeResolver는 handlers.ScopeResolver 인터페이스 구체 구현입니다.
//
// resourceType 분기:
//   - "robot" → robot.Service.GetRobot(ctx, tx, robotID).FleetID
//   - "scan"  → scan.Service.GetSession(ctx, tx, sessionID).FleetID
//   - 그 외   → 빈 fleetID + nil error (D-RBACEX-9 — 미지원 type은 fleet binding 자연 deny)
//
// tenant scope 격리: ctx에 TenantID가 없으면 storage.Tx 진입에서 ErrTenantMissing 반환 →
// 빈 fleetID + error 전파. middleware는 D-RBACEX-9에 따라 빈 fleetID로 PDP 평가 → fleet
// binding 자연 deny.
//
// 본 구체는 cross-tenant lookup을 차단합니다 — 다른 tenant의 robot/scan ID로 호출해도
// storage.Tx의 tenant_id 필터로 ErrNotFound가 반환됩니다.
type scopeResolver struct {
	storage storage.Storage
	robot   robot.Service
	scan    scan.Service
}

// newScopeResolver는 robot/scan service 위임 ScopeResolver를 만듭니다.
//
// 본 함수는 main.go bootstrap 시점에 *Platform.Storage / *Platform.Robot / *Platform.Scan을
// 받아 handlers.Deps.ScopeResolver에 주입합니다.
func newScopeResolver(s storage.Storage, r robot.Service, sc scan.Service) *scopeResolver {
	return &scopeResolver{storage: s, robot: r, scan: sc}
}

// ResolveFleet은 resourceType 분기로 도메인 service에 위임합니다.
//
// storage.Tx 진입 — tenant scope 격리(ctx의 TenantID로 자동). robot/scan repo가 tenant_id
// 필터를 적용하므로 cross-tenant lookup은 storage.ErrNotFound가 반환됩니다.
//
// error는 호출자(middleware)가 D-RBACEX-9에 따라 빈 fleetID fallback 처리 — 본 함수는
// 그대로 전파합니다.
func (r *scopeResolver) ResolveFleet(ctx context.Context, resourceType, resourceID string) (string, error) {
	if resourceID == "" {
		return "", nil
	}

	var fleetID string
	err := r.storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		switch resourceType {
		case "robot":
			rb, err := r.robot.GetRobot(ctx, tx, resourceID)
			if err != nil {
				return err
			}
			fleetID = rb.FleetID
			return nil
		case "scan":
			ss, err := r.scan.GetSession(ctx, tx, resourceID)
			if err != nil {
				return err
			}
			fleetID = ss.FleetID
			return nil
		default:
			// 미지원 resourceType — D-RBACEX-9 일관, 빈 fleetID + nil error.
			return nil
		}
	})
	if err != nil {
		return "", err
	}
	return fleetID, nil
}
