//go:build integration

// replication_integration_test.go — Phase 8 Stage 7 multi-region HA e2e.
//
// 본 파일은 `-tags=integration` 빌드 태그가 붙어야 컴파일됩니다. testcontainers-go로
// PG 2개(primary + standby) container를 spawn해 logical replication 동작을 실측.
//
// 실행:
//
//	go test -tags=integration -count=1 -timeout=10m -run TestReplication \
//	    ./internal/platform/storage/postgres/
//
// 검증 항목 (multi-region-ha-design.md §4 MR.T1~T8):
//
//   - MR.T1 TestReplicationLagWithin1Second: primary INSERT → standby propagation < 1s
//   - MR.T7 TestTenantMetaReplicated: primary tenant CREATE → standby에서 조회 가능
//
// carryover (별 round):
//
//   - MR.T2/T3: standby read-only middleware (이미 replication_test.go unit test)
//   - MR.T4: failover promote (pg_promote() + leader-election 재시작)
//   - MR.T5: audit chain cross-region SHA 검증
//   - MR.T6: split-brain 방어 (fence token / leader_epoch)
//   - MR.T8: rosshield_replication_lag_seconds Prometheus metric
//
// 본 round는 PG replication 자체 동작 검증이 우선 — application-level 통합은 후속.

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

// replicationFixture는 logical replication이 설정된 2 PG container를 묶습니다.
//
// primary: wal_level=logical + PUBLICATION rosshield_main FOR ALL TABLES
// standby: subscription rosshield_main_sub CONNECTION primary PUBLICATION rosshield_main
//
// 같은 docker network에 두 container를 spawn해 standby가 primary의 wal stream에 접근
// 가능. 양쪽 모두 Migrate 적용 (subscription은 DDL 전파 안 함).
type replicationFixture struct {
	network       *testcontainers.DockerNetwork
	primaryC      testcontainers.Container
	primaryDSN    string
	primaryStore  storage.Storage
	standbyC      testcontainers.Container
	standbyDSN    string
	standbyStore  storage.Storage
	primaryAlias  string // standby가 primary에 붙을 때 사용하는 host alias
}

const (
	repPGImage         = "postgres:16-alpine"
	repPGUser          = "test"
	repPGPassword      = "test"
	repPGDatabase      = "rosshield_test"
	repPrimaryAlias    = "primary-pg"
	repPublicationName = "rosshield_main"
	repSubscriptionN   = "rosshield_main_sub"
)

func newReplicationFixture(t *testing.T) *replicationFixture {
	t.Helper()
	ctx := context.Background()

	// 같은 docker network에 두 container — standby가 primary alias로 접근.
	net, err := tcnetwork.New(ctx)
	if err != nil {
		t.Skipf("docker network create failed (docker unavailable?): %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = net.Remove(shutdownCtx)
	})

	primaryC := runPGContainer(t, ctx, net.Name, repPrimaryAlias, true /*wal_level=logical*/)
	standbyC := runPGContainer(t, ctx, net.Name, "standby-pg", true)

	primaryDSN := dsnForContainer(t, ctx, primaryC, "sslmode=disable")
	standbyDSN := dsnForContainer(t, ctx, standbyC, "sslmode=disable")

	primaryStore, err := postgres.Open(storage.Config{Driver: "postgres", DSN: primaryDSN})
	if err != nil {
		t.Fatalf("primary postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = primaryStore.Close() })

	standbyStore, err := postgres.Open(storage.Config{Driver: "postgres", DSN: standbyDSN})
	if err != nil {
		t.Fatalf("standby postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = standbyStore.Close() })

	// 양쪽 모두 schema 적용 (subscription은 DDL 전파 안 함 — pre-create 필요).
	if err := primaryStore.Migrate(ctx); err != nil {
		t.Fatalf("primary Migrate: %v", err)
	}
	if err := standbyStore.Migrate(ctx); err != nil {
		t.Fatalf("standby Migrate: %v", err)
	}

	// Primary: PUBLICATION 생성
	execOnPrimary(t, ctx, primaryC,
		fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", repPublicationName))

	// Standby: SUBSCRIPTION 생성 (primary의 docker network alias로 접근)
	subConn := fmt.Sprintf(
		"host=%s port=5432 user=%s password=%s dbname=%s",
		repPrimaryAlias, repPGUser, repPGPassword, repPGDatabase,
	)
	execOnStandby(t, ctx, standbyC,
		fmt.Sprintf(
			"CREATE SUBSCRIPTION %s CONNECTION '%s' PUBLICATION %s WITH (copy_data = true)",
			repSubscriptionN, subConn, repPublicationName,
		))

	return &replicationFixture{
		network:      net,
		primaryC:     primaryC,
		primaryDSN:   primaryDSN,
		primaryStore: primaryStore,
		standbyC:     standbyC,
		standbyDSN:   standbyDSN,
		standbyStore: standbyStore,
		primaryAlias: repPrimaryAlias,
	}
}

// runPGContainer는 wal_level=logical로 부팅된 PG container를 spawn합니다.
//
// primary는 wal_level=logical 필수, standby는 subscription 호스트라 동일 설정 권장.
// network에 alias로 등록되어 다른 container가 host name으로 접근 가능.
func runPGContainer(t *testing.T, ctx context.Context, networkName, alias string, walLogical bool) testcontainers.Container {
	t.Helper()

	cmd := []string{"postgres"}
	if walLogical {
		cmd = append(cmd,
			"-c", "wal_level=logical",
			"-c", "max_wal_senders=10",
			"-c", "max_replication_slots=10",
		)
	}

	req := testcontainers.ContainerRequest{
		Image:        repPGImage,
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       repPGDatabase,
			"POSTGRES_USER":     repPGUser,
			"POSTGRES_PASSWORD": repPGPassword,
		},
		Cmd:      cmd,
		Networks: []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {alias},
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("PG container start failed (%s): %v", alias, err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Terminate(shutdownCtx)
	})
	return c
}

func dsnForContainer(t *testing.T, ctx context.Context, c testcontainers.Container, opts string) string {
	t.Helper()
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("container Host: %v", err)
	}
	port, err := c.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("container MappedPort: %v", err)
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?%s",
		repPGUser, repPGPassword, host, port.Port(), repPGDatabase, opts)
}

// execOnPrimary는 primary container에서 psql -c "<sql>" 실행.
func execOnPrimary(t *testing.T, ctx context.Context, c testcontainers.Container, sql string) {
	t.Helper()
	execOnContainer(t, ctx, c, sql)
}

// execOnStandby는 standby container에서 psql -c "<sql>" 실행.
func execOnStandby(t *testing.T, ctx context.Context, c testcontainers.Container, sql string) {
	t.Helper()
	execOnContainer(t, ctx, c, sql)
}

func execOnContainer(t *testing.T, ctx context.Context, c testcontainers.Container, sql string) {
	t.Helper()
	exitCode, reader, err := c.Exec(ctx, []string{
		"psql", "-U", repPGUser, "-d", repPGDatabase, "-c", sql,
	})
	if err != nil {
		t.Fatalf("container Exec %q: %v", sql, err)
	}
	if exitCode != 0 {
		buf := make([]byte, 4096)
		n, _ := reader.Read(buf)
		t.Fatalf("container Exec %q exit %d: %s", sql, exitCode, string(buf[:n]))
	}
}

// waitForReplication은 standby가 primary의 변경을 propagate할 때까지 대기.
//
// 동작:
//   - primary에서 sentinel row insert (테스트 tenants 외 임의 marker)
//   - standby에서 같은 row가 보일 때까지 polling (max 5s)
//
// 본 helper는 MR.T1과 MR.T7에서 공통 사용.
func waitForReplication(t *testing.T, fix *replicationFixture, sentinelID string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var found bool
		err := fix.standbyStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
			return tx.QueryRow(c,
				"SELECT EXISTS (SELECT 1 FROM tenants WHERE id = $1)", sentinelID,
			).Scan(&found)
		})
		if err == nil && found {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("standby did not receive sentinel %q within 5s", sentinelID)
}

// ────────────────────────────────────────────────────────────────────────
// MR.T1 — replication lag < 1초
// ────────────────────────────────────────────────────────────────────────

// TestReplicationLagWithin1Second은 primary에 row INSERT 후 standby가 1초 안에
// 그 row를 보는지 검증합니다 (multi-region-ha-design.md MR.T1).
//
// 검증 의도: RPO ≤ 1분 목표는 logical replication의 정상 동작 시 lag < 1초가
// 일반 — 본 test는 worst case 1초 window 보장.
func TestReplicationLagWithin1Second(t *testing.T) {
	t.Parallel()
	fix := newReplicationFixture(t)
	ctx := context.Background()

	const tenantID = "tn-mrtest-t1"
	insertStart := time.Now()
	err := fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		_, err := tx.Exec(c, `
			INSERT INTO tenants (id, slug, display_name, status, created_at, updated_at)
			VALUES ($1, $2, $3, 'active', NOW(), NOW())
		`, tenantID, "mrtest-t1", "MR.T1 Tenant")
		return err
	})
	if err != nil {
		t.Fatalf("primary INSERT: %v", err)
	}

	// Polling 100ms 간격으로 standby가 row 보일 때까지 대기. 5초 timeout으로 1초
	// 목표 충분히 cover (worst case window).
	waitForReplication(t, fix, tenantID)
	lag := time.Since(insertStart)

	if lag > time.Second {
		t.Errorf("replication lag = %v, want < 1s (logical replication 정상 동작 가정)", lag)
	}
	t.Logf("replication lag observed: %v", lag)
}

// ────────────────────────────────────────────────────────────────────────
// MR.T7 — tenant 메타 cross-region 복제
// ────────────────────────────────────────────────────────────────────────

// TestTenantMetaReplicated은 primary에 tenant CREATE → standby에서 tenant 메타
// 정확 조회 가능 검증 (MR.T7).
//
// R5 요구: tenant 격리가 cross-region 일관 유지 — replica가 primary의 tenant 메타를
// 받아야 tenant scope 자체가 깨지지 않음.
func TestTenantMetaReplicated(t *testing.T) {
	t.Parallel()
	fix := newReplicationFixture(t)
	ctx := context.Background()

	const (
		tenantID   = "tn-mrtest-t7"
		tenantSlug = "mrtest-t7"
		tenantName = "MR.T7 Cross-region Tenant"
	)

	// Primary에 tenant INSERT (실 tenant.Service 우회 — schema 직접 사용으로 단순화).
	err := fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		_, err := tx.Exec(c, `
			INSERT INTO tenants (id, slug, display_name, status, created_at, updated_at)
			VALUES ($1, $2, $3, 'active', NOW(), NOW())
		`, tenantID, tenantSlug, tenantName)
		return err
	})
	if err != nil {
		t.Fatalf("primary INSERT tenant: %v", err)
	}

	// Standby에서 tenant 조회 (replication propagation 대기 포함).
	waitForReplication(t, fix, tenantID)

	var (
		gotSlug, gotName, gotStatus string
	)
	err = fix.standbyStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		return tx.QueryRow(c, `
			SELECT slug, display_name, status FROM tenants WHERE id = $1
		`, tenantID).Scan(&gotSlug, &gotName, &gotStatus)
	})
	if err != nil {
		t.Fatalf("standby SELECT tenant: %v", err)
	}

	if gotSlug != tenantSlug {
		t.Errorf("standby tenant.slug = %q, want %q", gotSlug, tenantSlug)
	}
	if gotName != tenantName {
		t.Errorf("standby tenant.display_name = %q, want %q", gotName, tenantName)
	}
	if gotStatus != "active" {
		t.Errorf("standby tenant.status = %q, want active", gotStatus)
	}
}

// ────────────────────────────────────────────────────────────────────────
// fixture helper 검증 (sanity)
// ────────────────────────────────────────────────────────────────────────

// TestReplicationFixtureSetsUpPublicationSubscription은 fixture가 publication +
// subscription을 모두 등록한 상태인지 sanity 검증.
func TestReplicationFixtureSetsUpPublicationSubscription(t *testing.T) {
	t.Parallel()
	fix := newReplicationFixture(t)
	ctx := context.Background()

	// Primary: pg_publication에 rosshield_main 존재
	var pubExists bool
	err := fix.primaryStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		return tx.QueryRow(c,
			"SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)",
			repPublicationName,
		).Scan(&pubExists)
	})
	if err != nil {
		t.Fatalf("query pg_publication: %v", err)
	}
	if !pubExists {
		t.Errorf("primary pg_publication missing %q", repPublicationName)
	}

	// Standby: pg_subscription에 rosshield_main_sub 존재
	var subExists bool
	err = fix.standbyStore.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		return tx.QueryRow(c,
			"SELECT EXISTS (SELECT 1 FROM pg_subscription WHERE subname = $1)",
			repSubscriptionN,
		).Scan(&subExists)
	})
	if err != nil {
		// 일부 PG 버전은 pg_subscription read에 superuser 필요 — graceful warning.
		if strings.Contains(err.Error(), "permission denied") {
			t.Skipf("pg_subscription read requires superuser: %v", err)
		}
		t.Fatalf("query pg_subscription: %v", err)
	}
	if !subExists {
		t.Errorf("standby pg_subscription missing %q", repSubscriptionN)
	}
}
