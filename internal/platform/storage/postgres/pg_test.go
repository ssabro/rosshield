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

// Migrate 통합 검증은 PG 인스턴스가 필요해 별도 환경(testcontainers·CI service)에서 수행.
// 본 unit 테스트 파일에서는 docs 명세만 잠가두고, 실 적용은 README §운영 절차 참조.
//
// E22-D: golang-migrate/v4 + iofs source + postgres database driver 결선 완료.
//
//	bootstrap이 store.Migrate(ctx)를 호출하면 0001~0021 마이그레이션이 순서대로 적용됨.

// 내부 rebind 동작 검증 — black-box 으로는 노출되지 않으므로 export 테스트는
// 별도 파일(rebind_test.go in package)에서 검증합니다.
//
// 본 파일에서는 rebind 결과가 실제 Tx.Exec 경로에서 사용된다는 사실만 명시.
var _ = errors.New // pin import
