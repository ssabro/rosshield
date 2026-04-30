// Package insightautorun은 scan.completed 이벤트를 구독하여 insight detector를 자동 실행합니다 (E19 Phase 2).
//
// 동기:
//   - E14 RunForFleet은 명시 호출만 가능 — UX 개선 (사용자가 매번 수동 트리거 불필요)
//   - 컴포지션 위치: app layer (P5) — bootstrap이 Platform 의존성을 주입
//
// 흐름:
//
//  1. EventBus.Subscribe("scan.completed", handle)
//  2. handle: Event.Payload → CompletedEventPayload 디코딩
//  3. payload.Status != "completed" → skip (failed/cancelled는 insight 없음)
//  4. SessionID로 ScanSession 조회 → FleetID·TenantID 확보
//  5. Storage.Tx 안에서 Insight.RunForFleet 호출 (best-effort, 에러는 로그만)
//
// 도메인 결합: scan/insight 도메인을 직접 호출하나 도메인 services interface 통과 — P5 유지.
// 본 패키지는 cmd/* bootstrap에서만 결선되며, 도메인이 본 패키지를 import하지 않음 (역방향 안전).
package insightautorun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Deps는 Subscriber 결선 의존성입니다.
type Deps struct {
	Logger  *slog.Logger
	Storage storage.Storage
	Scan    scan.Service
	Insight insight.Service
}

// Subscriber는 scan.completed 이벤트 구독자입니다 (Start 시 bus에 등록).
type Subscriber struct {
	deps Deps
}

// New는 새 Subscriber를 반환합니다.
func New(deps Deps) *Subscriber {
	return &Subscriber{deps: deps}
}

// Start는 bus에 scan.completed 핸들러를 등록하고 Subscription을 반환합니다.
//
// 호출자(bootstrap)는 반환된 Subscription을 platform graceful shutdown 시 Cancel.
// 핸들러는 best-effort: 도메인 에러는 로그만 남기고 nil 반환 (이벤트 ack 보장).
func (s *Subscriber) Start(ctx context.Context, bus eventbus.Bus) eventbus.Subscription {
	return bus.Subscribe(ctx, scan.EventTypeCompleted, s.handle)
}

// handle은 scan.completed 이벤트 1건을 처리합니다.
//
// 반환 nil — bus에 ack. 도메인 에러는 로그로만 표면화 (재시도 X — Phase 2 단순화).
func (s *Subscriber) handle(ctx context.Context, evt eventbus.Event) error {
	var payload scan.CompletedEventPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		s.deps.Logger.Warn("insightautorun: payload decode failed",
			"eventId", evt.ID, "err", err.Error())
		return nil
	}

	// failed·cancelled는 insight 없음 — 데이터 부족·중단이라 detect 의미 없음.
	if payload.Status != string(scan.StatusCompleted) {
		return nil
	}

	tenantID := storage.TenantID(evt.TenantID)
	if tenantID == "" {
		s.deps.Logger.Warn("insightautorun: empty tenantID in event",
			"eventId", evt.ID, "sessionId", payload.SessionID)
		return nil
	}

	// 1) session 조회로 FleetID 확보 — payload에 fleetId가 없어 추가 조회 필요.
	// 2) RunForFleet 실행 — 둘 다 같은 tenantCtx + 동일 Tx에 묶음.
	tenantCtx := storage.WithTenantID(ctx, tenantID)
	err := s.deps.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		session, err := s.deps.Scan.GetSession(ctx, tx, payload.SessionID)
		if err != nil {
			return fmt.Errorf("get session: %w", err)
		}
		if session.FleetID == "" {
			return errors.New("session has empty FleetID")
		}
		produced, err := s.deps.Insight.RunForFleet(ctx, tx, session.FleetID)
		if err != nil {
			return fmt.Errorf("run for fleet %s: %w", session.FleetID, err)
		}
		s.deps.Logger.Info("insightautorun: insights produced",
			"sessionId", payload.SessionID,
			"fleetId", session.FleetID,
			"count", len(produced))
		return nil
	})
	if err != nil {
		s.deps.Logger.Warn("insightautorun: handle failed",
			"eventId", evt.ID, "sessionId", payload.SessionID, "err", err.Error())
	}
	return nil
}
