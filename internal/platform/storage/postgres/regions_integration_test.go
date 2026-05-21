//go:build integration

// regions_integration_test.go — Phase 10 Stage 10.A-6 — multi-region cutover →
// ListReplicas + ListFailovers + audit head sha 응답이 한 흐름으로 검증되는
// testcontainers e2e.
//
// 본 파일은 replication_integration_test.go의 fixture를 재사용합니다 (newReplicationFixture).
// 검증 시나리오 (TestMultiRegionFailoverEndToEnd):
//
//  1. 초기 상태: replication_replicas에 us-east(primary) + us-west(standby) 시드.
//  2. heartbeat 갱신: standby에서 last_replay_lsn ping → ListReplicas에서 lag 정상.
//  3. failover trigger: replicationrepo.SetRole + RecordFailover + LinkFailoverAudit 흐름.
//     primary↔standby role swap + audit.Service 통해 chain entry emit.
//  4. failover record 등장: ListFailovers에서 새 row 정확 노출 + audit_entry_id link.
//
// 본 test는 backend 단독 통합 — UI fetch 통합은 web/playwright/tests/regions.spec.ts가 cover.

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/replication"
	replicationrepo "github.com/ssabro/rosshield/internal/platform/replication/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// TestMultiRegionFailoverEndToEnd는 us-east(primary) → us-west(standby) 2-region
// 환경에서 manual failover 호출 시 replication_replicas role swap +
// replication_failovers 이력 INSERT + audit.replication.failover entry emit이 모두
// 한 트랜잭션에 묶여 정확 동작함을 검증합니다.
//
// Phase 10.A-6 — `/regions` UI가 fetch하는 backend endpoint(ListReplicas +
// ListFailovers + GetAuditHeadSHA) 3종이 multi-region cutover 흐름에서 일관 동작.
func TestMultiRegionFailoverEndToEnd(t *testing.T) {
	t.Parallel()
	fix := newReplicationFixture(t)
	ctx := context.Background()

	const (
		usEast    = "us-east"
		usWest    = "us-west"
		tenantID  = "tn-mr-failover-e2e"
		adminUser = "admin-e2e-1"
	)

	repo := replicationrepo.New()
	auditService := auditrepo.New(auditrepo.Deps{
		Clock: clock.System(),
		Role:  &fakeAuditRole{leader: true, epoch: 1},
	})

	// 1. 초기 상태 시드 — us-east primary + us-west standby. tenant 메타 + audit chain
	//    seed entry (cross-region head sha 검증 base).
	seedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	err := fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		if _, e := tx.Exec(c, `
			INSERT INTO tenants (id, name, plan, created_at)
			VALUES ($1, $2, 'desktop_free', NOW()::TEXT)
		`, tenantID, "MR e2e tenant"); e != nil {
			return e
		}
		if _, e := repo.RegisterReplica(c, tx, replication.RegisterReplicaRequest{
			Region:   usEast,
			Role:     replication.RolePrimary,
			Endpoint: "https://us-east.example",
		}, seedAt); e != nil {
			return e
		}
		if _, e := repo.RegisterReplica(c, tx, replication.RegisterReplicaRequest{
			Region:   usWest,
			Role:     replication.RoleStandby,
			Endpoint: "https://us-west.example",
		}, seedAt); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed primary state: %v", err)
	}

	// Assertion 1 — 초기 role 매트릭스.
	var initial []replication.Replica
	err = fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		rs, e := repo.ListReplicas(c, tx)
		if e != nil {
			return e
		}
		initial = rs
		return nil
	})
	if err != nil {
		t.Fatalf("ListReplicas initial: %v", err)
	}
	if len(initial) != 2 {
		t.Fatalf("len(initial replicas) = %d, want 2", len(initial))
	}
	roleOf := func(rs []replication.Replica, region string) replication.Role {
		for _, r := range rs {
			if r.Region == region {
				return r.Role
			}
		}
		return ""
	}
	if roleOf(initial, usEast) != replication.RolePrimary {
		t.Errorf("initial us-east role = %q, want primary", roleOf(initial, usEast))
	}
	if roleOf(initial, usWest) != replication.RoleStandby {
		t.Errorf("initial us-west role = %q, want standby", roleOf(initial, usWest))
	}

	// 2. Heartbeat — us-west가 LSN ping → lag 정상 (last_replay_at != zero).
	heartbeatAt := seedAt.Add(15 * time.Second)
	err = fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		return repo.UpdateHeartbeat(c, tx, replication.HeartbeatRequest{
			Region:        usWest,
			LastReplayLSN: "0/19000060",
			Now:           heartbeatAt,
		})
	})
	if err != nil {
		t.Fatalf("UpdateHeartbeat us-west: %v", err)
	}

	// Assertion 2 — heartbeat 반영 + lag 측정 가능.
	var afterHB []replication.Replica
	err = fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		rs, e := repo.ListReplicas(c, tx)
		if e != nil {
			return e
		}
		afterHB = rs
		return nil
	})
	if err != nil {
		t.Fatalf("ListReplicas after heartbeat: %v", err)
	}
	var westAfterHB replication.Replica
	for _, r := range afterHB {
		if r.Region == usWest {
			westAfterHB = r
			break
		}
	}
	if westAfterHB.LastReplayLSN != "0/19000060" {
		t.Errorf("us-west LastReplayLSN = %q, want \"0/19000060\"", westAfterHB.LastReplayLSN)
	}
	if westAfterHB.LastReplayAt.IsZero() {
		t.Error("us-west LastReplayAt is zero after heartbeat — lag 측정 불가")
	}

	// 3. Failover trigger — primary(us-east) → us-west swap + audit chain entry.
	swapAt := seedAt.Add(30 * time.Second)
	fReq := replication.FailoverRequest{
		FromRegion:      usEast,
		ToRegion:        usWest,
		InitiatedByUser: adminUser,
		Reason:          "primary region outage drill",
		Now:             swapAt,
	}
	if vErr := replication.ValidateFailoverRequest(fReq); vErr != nil {
		t.Fatalf("ValidateFailoverRequest: %v", vErr)
	}

	tenantCtx := storage.WithTenantID(ctx, tenantID)
	var auditEntrySeq int64
	err = fix.primaryStore.Tx(tenantCtx, func(c context.Context, tx storage.Tx) error {
		if e := repo.SetRole(c, tx, fReq.FromRegion, replication.RoleStandby); e != nil {
			return e
		}
		if e := repo.SetRole(c, tx, fReq.ToRegion, replication.RolePrimary); e != nil {
			return e
		}
		if _, e := repo.RecordFailover(c, tx, fReq); e != nil {
			return e
		}

		entry, e := auditService.Append(c, tx, audit.AppendRequest{
			TenantID: storage.TenantID(tenantID),
			Actor:    audit.Actor{Type: audit.ActorUser, ID: adminUser},
			Action:   "audit.replication.failover",
			Target:   audit.Target{Type: "region", ID: fReq.ToRegion},
			Outcome:  audit.OutcomeSuccess,
		})
		if e != nil {
			return e
		}
		auditEntrySeq = entry.Seq
		return nil
	})
	if err != nil {
		t.Fatalf("failover sequence Tx: %v", err)
	}
	if auditEntrySeq == 0 {
		t.Error("auditEntrySeq = 0, want > 0 (audit chain head 진척)")
	}

	// PG는 LastInsertId 미지원 — RecordFailover.ID가 0. 실 row id 추출은
	// ListFailovers로 우회 (sqliterepo 주석 §60 한계 참조). 후속 LinkFailoverAudit는
	// 본 test에서 생략(완료 시각 채움은 별 round의 PG-native RETURNING 도입 후 cover).
	var newRow *replication.Failover
	err = fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		rs, e := repo.ListFailovers(c, tx, 10)
		if e != nil {
			return e
		}
		for i := range rs {
			if rs[i].FromRegion == usEast && rs[i].ToRegion == usWest && rs[i].InitiatedByUser == adminUser {
				newRow = &rs[i]
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ListFailovers post-record: %v", err)
	}
	if newRow == nil {
		t.Fatalf("newly recorded failover not found in ListFailovers")
	}
	failoverID := newRow.ID
	if failoverID == 0 {
		t.Error("failoverID = 0 even after PG INSERT (SERIAL not assigned)")
	}

	// LinkFailoverAudit는 ListFailovers로 id 추출 후 별 Tx에서 호출 — completed_at +
	// audit_entry_id 채우기 검증.
	linkAt := swapAt.Add(time.Second)
	err = fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		return repo.LinkFailoverAudit(c, tx, failoverID, auditEntrySeq, linkAt)
	})
	if err != nil {
		t.Fatalf("LinkFailoverAudit (post-id): %v", err)
	}

	// Assertion 3 — role swap 확인.
	var afterSwap []replication.Replica
	err = fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		rs, e := repo.ListReplicas(c, tx)
		if e != nil {
			return e
		}
		afterSwap = rs
		return nil
	})
	if err != nil {
		t.Fatalf("ListReplicas after swap: %v", err)
	}
	if roleOf(afterSwap, usEast) != replication.RoleStandby {
		t.Errorf("post-swap us-east role = %q, want standby", roleOf(afterSwap, usEast))
	}
	if roleOf(afterSwap, usWest) != replication.RolePrimary {
		t.Errorf("post-swap us-west role = %q, want primary", roleOf(afterSwap, usWest))
	}

	// Assertion 4 — failover record가 ListFailovers에 등장 + audit_entry_id link.
	var history []replication.Failover
	err = fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		rs, e := repo.ListFailovers(c, tx, 10)
		if e != nil {
			return e
		}
		history = rs
		return nil
	})
	if err != nil {
		t.Fatalf("ListFailovers: %v", err)
	}
	if len(history) < 1 {
		t.Fatalf("len(history) = %d, want >= 1 (recorded failover missing)", len(history))
	}
	var found *replication.Failover
	for i := range history {
		if history[i].ID == failoverID {
			found = &history[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("recorded failover id=%d not found in ListFailovers", failoverID)
	}
	if found.FromRegion != usEast || found.ToRegion != usWest {
		t.Errorf("failover row direction = %s → %s, want %s → %s",
			found.FromRegion, found.ToRegion, usEast, usWest)
	}
	if found.AuditEntryID != auditEntrySeq {
		t.Errorf("failover.AuditEntryID = %d, want %d (audit link broken)",
			found.AuditEntryID, auditEntrySeq)
	}
	if found.CompletedAt.IsZero() {
		t.Error("failover.CompletedAt is zero — LinkFailoverAudit이 완료 시각 안 채움")
	}
	if found.InitiatedByUser != adminUser {
		t.Errorf("failover.InitiatedByUser = %q, want %q", found.InitiatedByUser, adminUser)
	}

	// Audit head sha — primary에 chain head 진척 확인 (GetAuditHeadSHA endpoint와 동등).
	var headSeq int64
	err = fix.primaryStore.Tx(tenantCtx, func(c context.Context, tx storage.Tx) error {
		head, e := auditService.Head(c, tx, storage.TenantID(tenantID))
		if e != nil {
			return e
		}
		headSeq = head.Seq
		return nil
	})
	if err != nil {
		t.Fatalf("audit Head primary: %v", err)
	}
	if headSeq < auditEntrySeq {
		t.Errorf("primary audit head.Seq = %d, want >= %d (failover entry 미반영)",
			headSeq, auditEntrySeq)
	}
}
