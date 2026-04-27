package sqliterepo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// E5 Stage E — cross-tenant fuzzer (E3 `031fa05` 패턴 답습).
//
// 두 tenant A·B를 부팅하고, A의 데이터를 B 컨텍스트에서 조회·변경 시도가 모두 차단되는지
// 모든 Service 메서드에 대해 회귀 검증합니다. 도메인 격리(P5·R3-5)의 마지막 안전망.

type twoTenantFixture struct {
	repo   *sqliterepo.Repo
	store  storage.Storage
	tA, tB string
	fleetA string
	fleetB string
	robotA string
	robotB string
}

func setupTwoTenantsWithRobots(t *testing.T) *twoTenantFixture {
	t.Helper()
	tester := &fakeSSHTester{} // SSHTester 사용 안 하지만 nil 회피용
	repo, _, store := newTestRepoWithTester(t, tester)
	const tA, tB = "tn_X01", "tn_Y01"
	seedTenant(t, store, tA)
	seedTenant(t, store, tB)

	fleetA := createFleetForTest(t, store, repo, tA, "Fleet-A")
	fleetB := createFleetForTest(t, store, repo, tB, "Fleet-B")

	create := func(tenant, fleetID string) string {
		t.Helper()
		var id string
		if err := store.Tx(tenantCtx(tenant), func(ctx context.Context, tx storage.Tx) error {
			res, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetID))
			id = res.Robot.ID
			return err
		}); err != nil {
			t.Fatalf("CreateRobot for %s: %v", tenant, err)
		}
		return id
	}
	return &twoTenantFixture{
		repo:   repo,
		store:  store,
		tA:     tA,
		tB:     tB,
		fleetA: fleetA,
		fleetB: fleetB,
		robotA: create(tA, fleetA),
		robotB: create(tB, fleetB),
	}
}

// TestCrossTenantFuzzer는 8 Service 메서드가 cross-tenant 격리를 강제하는지 검증합니다.
// (CreateFleet·CreateRobot은 tx.TenantID()로 자기 tenant에만 INSERT — 검증 불필요.)
func TestCrossTenantFuzzer(t *testing.T) {
	t.Parallel()
	fx := setupTwoTenantsWithRobots(t)

	cases := []struct {
		name string
		// tenant B의 컨텍스트에서 A의 ID로 호출 → ErrNotFound 또는 ErrFleetNotFound 기대.
		op func(ctx context.Context, tx storage.Tx) error
		// 어느 에러 타입을 기대하는지.
		wantErrs []error
	}{
		{
			name: "GetFleet(A) from B → ErrNotFound",
			op: func(ctx context.Context, tx storage.Tx) error {
				_, err := fx.repo.GetFleet(ctx, tx, fx.fleetA)
				return err
			},
			wantErrs: []error{storage.ErrNotFound},
		},
		{
			name: "GetRobot(A) from B → ErrNotFound",
			op: func(ctx context.Context, tx storage.Tx) error {
				_, err := fx.repo.GetRobot(ctx, tx, fx.robotA)
				return err
			},
			wantErrs: []error{storage.ErrNotFound},
		},
		{
			name: "ListRobots(fleetA) from B → empty (FK FleetID는 B의 fleet 아님)",
			op: func(ctx context.Context, tx storage.Tx) error {
				list, err := fx.repo.ListRobots(ctx, tx, fx.fleetA)
				if err != nil {
					return err
				}
				if len(list) != 0 {
					return errors.New("non-empty list — leak")
				}
				return nil
			},
			wantErrs: nil, // 빈 리스트 = 성공 (격리 OK)
		},
		{
			name: "DeleteRobot(A) from B → ErrNotFound",
			op: func(ctx context.Context, tx storage.Tx) error {
				return fx.repo.DeleteRobot(ctx, tx, fx.robotA)
			},
			wantErrs: []error{storage.ErrNotFound},
		},
		{
			name: "GetCredentialMaterial(A) from B → ErrNotFound",
			op: func(ctx context.Context, tx storage.Tx) error {
				_, err := fx.repo.GetCredentialMaterial(ctx, tx, fx.robotA)
				return err
			},
			wantErrs: []error{storage.ErrNotFound},
		},
		{
			name: "RotateCredential(A) from B → ErrNotFound",
			op: func(ctx context.Context, tx storage.Tx) error {
				_, err := fx.repo.RotateCredential(ctx, tx, robot.RotateCredentialRequest{
					RobotID:  fx.robotA,
					Material: samplePrivateKey(),
				})
				return err
			},
			wantErrs: []error{storage.ErrNotFound},
		},
		{
			name: "TestConnection(A) from B → ErrNotFound",
			op: func(ctx context.Context, tx storage.Tx) error {
				return fx.repo.TestConnection(ctx, tx, fx.robotA)
			},
			wantErrs: []error{storage.ErrNotFound},
		},
		{
			name: "CreateRobot in B with fleetA (cross-tenant fleet ref) → ErrFleetNotFound",
			op: func(ctx context.Context, tx storage.Tx) error {
				_, err := fx.repo.CreateRobot(ctx, tx, sampleCreateRobot(fx.fleetA))
				return err
			},
			wantErrs: []error{robot.ErrFleetNotFound},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := fx.store.Tx(tenantCtx(fx.tB), func(ctx context.Context, tx storage.Tx) error {
				return tc.op(ctx, tx)
			})
			if len(tc.wantErrs) == 0 {
				if err != nil {
					t.Errorf("err = %v, want nil (op should succeed but find no leaked data)", err)
				}
				return
			}
			matched := false
			for _, w := range tc.wantErrs {
				if errors.Is(err, w) {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("err = %v, want one of %v", err, tc.wantErrs)
			}
		})
	}

	// ListFleets in B는 B의 fleet 1개만 반환해야.
	t.Run("ListFleets(B) shows only B's fleet", func(t *testing.T) {
		var list []robot.Fleet
		if err := fx.store.Tx(tenantCtx(fx.tB), func(ctx context.Context, tx storage.Tx) error {
			l, err := fx.repo.ListFleets(ctx, tx)
			list = l
			return err
		}); err != nil {
			t.Fatalf("ListFleets B: %v", err)
		}
		if len(list) != 1 || list[0].ID != fx.fleetB {
			t.Errorf("tenant B sees %+v, want only Fleet-B (id=%s)", list, fx.fleetB)
		}
	})

	// ListRobots in B (전체)도 B의 robot 1개만.
	t.Run("ListRobots(all) in B shows only B's robot", func(t *testing.T) {
		var list []robot.Robot
		if err := fx.store.Tx(tenantCtx(fx.tB), func(ctx context.Context, tx storage.Tx) error {
			l, err := fx.repo.ListRobots(ctx, tx, "")
			list = l
			return err
		}); err != nil {
			t.Fatalf("ListRobots B: %v", err)
		}
		if len(list) != 1 || list[0].ID != fx.robotB {
			t.Errorf("tenant B sees %+v, want only robotB (id=%s)", list, fx.robotB)
		}
	})
}
