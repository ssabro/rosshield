package sqliterepo_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/domain/benchmark/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// auditAdapter는 audit.Service를 benchmark.AuditEmitter로 감싸는 테스트용 구현입니다.
// (cmd/rosshield-server/bootstrap.go의 packEmitterAdapter와 동일 패턴)
type auditAdapter struct {
	svc audit.Service
}

func (a *auditAdapter) EmitPackInstalled(ctx context.Context, tx storage.Tx, p benchmark.Pack, actorID string) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: p.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: actorID},
		Action:   "pack.installed",
		Target:   audit.Target{Type: "pack", ID: p.ID},
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

func (a *auditAdapter) EmitPackLifecycleChanged(ctx context.Context, tx storage.Tx, packID string, from, to benchmark.State, actorID, reason string) error {
	tenantID := tx.TenantID()
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: tenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: actorID},
		Action:   "pack.lifecycle." + string(to),
		Target:   audit.Target{Type: "pack", ID: packID},
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

const testTenant storage.TenantID = "tn_test"

// pack tar.gz 빌드 헬퍼.
const validPackYAML = `apiVersion: rosshield.io/v1
kind: Pack
metadata:
  name: cis-ubuntu-2404
  version: v1.0.0
  vendor: CIS
  description: "CIS Ubuntu 24.04 Benchmark"
spec:
  schemaVersion: 1
`

const validCheckYAML = `apiVersion: rosshield.io/v1
kind: Check
metadata:
  id: CIS-1.1.1.1
  title: "Disable cramfs kernel module"
  severity: high
spec:
  auditCommand: "lsmod | grep cramfs"
  evaluationRule:
    op: empty
`

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func buildTarGz(t *testing.T, files map[string][]byte, packKey string, priv ed25519.PrivateKey) []byte {
	t.Helper()
	entries := make([]benchmark.ManifestEntry, 0, len(files))
	for path, body := range files {
		sum := sha256.Sum256(body)
		entries = append(entries, benchmark.ManifestEntry{
			Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body)),
		})
	}
	manifestBytes, _ := benchmark.CanonicalManifest(benchmark.Manifest{
		SchemaVersion: 1, PackKey: packKey, Files: entries,
	})
	signature := ed25519.Sign(priv, manifestBytes)

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{
		{"MANIFEST.json", manifestBytes},
		{"SIGNATURE", signature},
	} {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body))})
		_, _ = tw.Write(f.body)
	}
	for path, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: path, Mode: 0o644, Size: int64(len(body))})
		_, _ = tw.Write(body)
	}
	_ = tw.Close()
	_ = gz.Close()
	return gzBuf.Bytes()
}

func newTestRepo(t *testing.T) (*sqliterepo.Repo, audit.Service, storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "bench.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// system tenant 한 번 INSERT — packs.tenant_id FK reference (FK는 tenants를 참조하므로 row 필요).
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'Test', 'desktop_free', ?)`,
			string(testTenant), "2026-04-24T00:00:00Z")
		return e
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock:              clock.System(),
		IDGen:              idgen.NewULID(),
		Audit:              &auditAdapter{svc: auditSvc},
		DefaultSignerKeyID: "key_test_default",
	})
	return repo, auditSvc, store
}

// E4.T8 본체 (sqliterepo) — InstallPack은 packs/checks/lifecycle 모두 INSERT + audit emit.
func TestPackInstallIsAudited(t *testing.T) {
	t.Parallel()
	repo, auditSvc, store := newTestRepo(t)
	pub, priv := newKey(t)

	tarGz := buildTarGz(t, map[string][]byte{
		"pack.yaml":               []byte(validPackYAML),
		"checks/CIS-1.1.1.1.yaml": []byte(validCheckYAML),
	}, "cis-cis-ubuntu-2404-v1.0.0", priv)

	tenantCtx := storage.WithTenantID(context.Background(), testTenant)

	var installed benchmark.Pack
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		p, err := repo.InstallPack(ctx, tx, testTenant, tarGz, pub, "key_signer", "us_admin")
		installed = p
		return err
	}); err != nil {
		t.Fatalf("InstallPack: %v", err)
	}

	if installed.ID == "" || installed.PackKey != "cis-cis-ubuntu-2404-v1.0.0" {
		t.Errorf("Pack metadata wrong: %+v", installed)
	}

	// audit_entries에 pack.installed 기록.
	var head audit.ChainHead
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		h, err := auditSvc.Head(ctx, tx, testTenant)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 1 {
		t.Errorf("audit head seq = %d, want 1", head.Seq)
	}

	// CurrentState = installed.
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		state, err := repo.CurrentState(ctx, tx, installed.ID)
		if err != nil {
			return err
		}
		if state != benchmark.StateInstalled {
			t.Errorf("state = %s, want installed", state)
		}
		return nil
	}); err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
}

// 같은 (tenant, pack_key) 두 번 → ErrPackAlreadyInstalled.
func TestInstallPackRejectsDuplicate(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	pub, priv := newKey(t)

	tarGz := buildTarGz(t, map[string][]byte{
		"pack.yaml": []byte(validPackYAML),
	}, "cis-cis-ubuntu-2404-v1.0.0", priv)
	tenantCtx := storage.WithTenantID(context.Background(), testTenant)

	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.InstallPack(ctx, tx, testTenant, tarGz, pub, "k", "us_a")
		return e
	}); err != nil {
		t.Fatalf("first install: %v", err)
	}

	err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.InstallPack(ctx, tx, testTenant, tarGz, pub, "k", "us_a")
		return e
	})
	if !errors.Is(err, benchmark.ErrPackAlreadyInstalled) {
		t.Errorf("err = %v, want ErrPackAlreadyInstalled", err)
	}
}

// GetPackByKey + ListPacks 라운드트립.
func TestGetPackAndListPacks(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	pub, priv := newKey(t)

	tarGz := buildTarGz(t, map[string][]byte{
		"pack.yaml":               []byte(validPackYAML),
		"checks/CIS-1.1.1.1.yaml": []byte(validCheckYAML),
	}, "cis-cis-ubuntu-2404-v1.0.0", priv)
	tenantCtx := storage.WithTenantID(context.Background(), testTenant)

	var installed benchmark.Pack
	_ = store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		p, _ := repo.InstallPack(ctx, tx, testTenant, tarGz, pub, "k", "us_a")
		installed = p
		return nil
	})

	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		got, err := repo.GetPackByKey(ctx, tx, testTenant, installed.PackKey)
		if err != nil {
			return err
		}
		if got.ID != installed.ID {
			t.Errorf("ID mismatch: %q vs %q", got.ID, installed.ID)
		}
		if len(got.Checks) != 1 || got.Checks[0].CheckID != "CIS-1.1.1.1" {
			t.Errorf("Checks = %v", got.Checks)
		}
		return nil
	}); err != nil {
		t.Fatalf("GetPackByKey: %v", err)
	}

	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		list, err := repo.ListPacks(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		if len(list) != 1 {
			t.Errorf("ListPacks len = %d, want 1", len(list))
		}
		return nil
	}); err != nil {
		t.Fatalf("ListPacks: %v", err)
	}
}

// TransitionPack — installed → staged → active.
func TestTransitionPackHappy(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	pub, priv := newKey(t)

	tarGz := buildTarGz(t, map[string][]byte{
		"pack.yaml": []byte(validPackYAML),
	}, "cis-cis-ubuntu-2404-v1.0.0", priv)
	tenantCtx := storage.WithTenantID(context.Background(), testTenant)

	var installed benchmark.Pack
	_ = store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		p, _ := repo.InstallPack(ctx, tx, testTenant, tarGz, pub, "k", "us_a")
		installed = p
		return nil
	})

	for _, to := range []benchmark.State{benchmark.StateStaged, benchmark.StateActive} {
		if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
			return repo.TransitionPack(ctx, tx, installed.ID, to, "us_a", "test transition")
		}); err != nil {
			t.Fatalf("TransitionPack to %s: %v", to, err)
		}
	}

	// CurrentState = active.
	_ = store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		state, _ := repo.CurrentState(ctx, tx, installed.ID)
		if state != benchmark.StateActive {
			t.Errorf("state = %s, want active", state)
		}
		return nil
	})
}

// 불법 전이는 거부.
func TestTransitionPackRejectsIllegal(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	pub, priv := newKey(t)

	tarGz := buildTarGz(t, map[string][]byte{
		"pack.yaml": []byte(validPackYAML),
	}, "cis-cis-ubuntu-2404-v1.0.0", priv)
	tenantCtx := storage.WithTenantID(context.Background(), testTenant)

	var installed benchmark.Pack
	_ = store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		p, _ := repo.InstallPack(ctx, tx, testTenant, tarGz, pub, "k", "us_a")
		installed = p
		return nil
	})

	// installed → active 직접 시도 (Staged 거치지 않음) → ErrIllegalTransition.
	err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		return repo.TransitionPack(ctx, tx, installed.ID, benchmark.StateActive, "us_a", "")
	})
	if !errors.Is(err, benchmark.ErrIllegalTransition) {
		t.Errorf("err = %v, want ErrIllegalTransition", err)
	}
}
