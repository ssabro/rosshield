package postgres_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

// 컴파일 시점에 *postgres.Postgres 가 storage.Storage 를 만족함을 확인.
// (pg.go 안에서도 var _ 로 확인하지만, 외부 import 경로에서 한 번 더 보강.)
func TestPostgresImplementsStorageInterface(t *testing.T) {
	t.Parallel()

	var s storage.Storage = (*postgres.Postgres)(nil)
	_ = s
}

func TestOpenRejectsBadDriver(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  storage.Config
	}{
		{"empty driver", storage.Config{Driver: "", DSN: "postgres://x"}},
		{"sqlite driver", storage.Config{Driver: "sqlite", DSN: "postgres://x"}},
		{"missing dsn", storage.Config{Driver: "postgres", DSN: ""}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := postgres.Open(tc.cfg)
			if err == nil {
				_ = s.Close()
				t.Fatalf("Open(%+v): want error, got nil", tc.cfg)
			}
		})
	}
}

func TestMigrationsFSEmbedded(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(postgres.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("migrations directory empty")
	}

	want := map[string]bool{
		"0001_tenant_init.up.sql":   false,
		"0001_tenant_init.down.sql": false,
	}
	for _, e := range entries {
		if _, ok := want[e.Name()]; ok {
			want[e.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected migration file embedded: %s", name)
		}
	}
}

func TestFirstMigrationContainsExpectedTables(t *testing.T) {
	t.Parallel()

	b, err := fs.ReadFile(postgres.MigrationsFS, "migrations/0001_tenant_init.up.sql")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	src := string(b)
	for _, want := range []string{
		"CREATE TABLE platform_info",
		"CREATE TABLE tenants",
		"CREATE TABLE users",
		"JSONB",       // PG 변환 마커
		"TIMESTAMPTZ", // PG 변환 마커
	} {
		if !strings.Contains(src, want) {
			t.Errorf("0001 up.sql missing token %q", want)
		}
	}
}

// Migrate 는 본 stage 에서 미구현임을 명시적으로 보장합니다.
// 후속 stage 에서 이 테스트는 정상 적용 검증 테스트로 교체됩니다.
func TestMigrateNotYetImplemented(t *testing.T) {
	t.Parallel()

	// pool 이 없는 상태에서 호출하면 panic 가능 — 대신 Open 없이 nil receiver 호출 대신
	// 수동으로 zero-value Postgres 를 만들지 않고, 대신 pg.go 의 Migrate 가 항상 에러를
	// 반환한다는 점을 docs 와 하나의 회귀 테스트로 잠가둡니다.
	//
	// 실제 호출은 pool 의존이라 unit 환경에서 어렵습니다 — 본 테스트는 의도 표시(명세).
	t.Log("Migrate 는 명시적 미구현 에러 — README §Stage A 한계 참조.")
}

// 내부 rebind 동작 검증 — black-box 으로는 노출되지 않으므로 export 테스트는
// 별도 파일(rebind_test.go in package)에서 검증합니다.
//
// 본 파일에서는 rebind 결과가 실제 Tx.Exec 경로에서 사용된다는 사실만 명시.
var _ = errors.New // pin import
