package replication_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/replication"
	replicationrepo "github.com/ssabro/rosshield/internal/platform/replication/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === Config (env override) ===

func TestLoadConfigFromEnvDefault(t *testing.T) {
	t.Setenv("ROSSHIELD_REPLICATION_ENABLED", "")
	t.Setenv("ROSSHIELD_REPLICATION_REGION", "")
	t.Setenv("ROSSHIELD_REPLICATION_ROLE", "")
	t.Setenv("ROSSHIELD_REPLICATION_PRIMARY_ENDPOINT", "")

	cfg, err := replication.LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("Enabled = true, want false (default)")
	}
	if cfg.Region != "default" {
		t.Errorf("Region = %q, want \"default\"", cfg.Region)
	}
	if cfg.Role != replication.RolePrimary {
		t.Errorf("Role = %q, want primary", cfg.Role)
	}
	if cfg.IsStandby() {
		t.Errorf("IsStandby = true, want false (default config)")
	}
}

func TestLoadConfigFromEnvStandby(t *testing.T) {
	t.Setenv("ROSSHIELD_REPLICATION_ENABLED", "true")
	t.Setenv("ROSSHIELD_REPLICATION_REGION", "ap-northeast-1")
	t.Setenv("ROSSHIELD_REPLICATION_ROLE", "standby")
	t.Setenv("ROSSHIELD_REPLICATION_PRIMARY_ENDPOINT", "https://api.lodestar.example")

	cfg, err := replication.LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if !cfg.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if cfg.Role != replication.RoleStandby {
		t.Errorf("Role = %q, want standby", cfg.Role)
	}
	if !cfg.IsStandby() {
		t.Errorf("IsStandby = false, want true")
	}
	if cfg.PrimaryEndpoint != "https://api.lodestar.example" {
		t.Errorf("PrimaryEndpoint = %q", cfg.PrimaryEndpoint)
	}
}

func TestLoadConfigFromEnvInvalidRole(t *testing.T) {
	t.Setenv("ROSSHIELD_REPLICATION_ROLE", "bogus")

	_, err := replication.LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid role, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("err = %v, want contains 'invalid'", err)
	}
}

func TestLoadConfigFromEnvInvalidEnabled(t *testing.T) {
	t.Setenv("ROSSHIELD_REPLICATION_ENABLED", "maybe")

	_, err := replication.LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid bool, got nil")
	}
}

// === Repository CRUD ===

func newTestStorage(t *testing.T) storage.Storage {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "replication.db")
	s, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

func TestRegisterReplicaAndGet(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	repo := replicationrepo.New()

	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	var registered replication.Replica
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		row, e := repo.RegisterReplica(ctx, tx, replication.RegisterReplicaRequest{
			Region:   "ap-northeast-2",
			Role:     replication.RolePrimary,
			Endpoint: "https://primary.example",
		}, now)
		if e != nil {
			return e
		}
		registered = row
		return nil
	}); err != nil {
		t.Fatalf("RegisterReplica: %v", err)
	}
	if registered.Region != "ap-northeast-2" {
		t.Errorf("Region = %q", registered.Region)
	}
	if registered.Role != replication.RolePrimary {
		t.Errorf("Role = %q", registered.Role)
	}
	if !registered.Enabled {
		t.Errorf("Enabled = false, want true (default)")
	}

	// GetReplica.
	var fetched replication.Replica
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		got, e := repo.GetReplica(ctx, tx, "ap-northeast-2")
		if e != nil {
			return e
		}
		fetched = got
		return nil
	}); err != nil {
		t.Fatalf("GetReplica: %v", err)
	}
	if fetched.Endpoint != "https://primary.example" {
		t.Errorf("Endpoint = %q", fetched.Endpoint)
	}
}

func TestRegisterReplicaDuplicateRegion(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	repo := replicationrepo.New()

	now := time.Now().UTC()
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.RegisterReplica(ctx, tx, replication.RegisterReplicaRequest{
			Region: "us-west-2", Role: replication.RolePrimary, Endpoint: "https://a",
		}, now)
		return e
	}); err != nil {
		t.Fatalf("first RegisterReplica: %v", err)
	}
	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.RegisterReplica(ctx, tx, replication.RegisterReplicaRequest{
			Region: "us-west-2", Role: replication.RoleStandby, Endpoint: "https://b",
		}, now)
		return e
	})
	if err == nil {
		t.Fatal("expected ErrReplicaExists, got nil")
	}
	if !strings.Contains(err.Error(), "exists") {
		t.Errorf("err = %v, want contains 'exists'", err)
	}
}

func TestUpdateHeartbeatAndListReplicas(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	repo := replicationrepo.New()

	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.RegisterReplica(ctx, tx, replication.RegisterReplicaRequest{
			Region: "ap-northeast-2", Role: replication.RolePrimary, Endpoint: "https://primary",
		}, now)
		if e != nil {
			return e
		}
		_, e = repo.RegisterReplica(ctx, tx, replication.RegisterReplicaRequest{
			Region: "ap-northeast-1", Role: replication.RoleStandby, Endpoint: "https://standby",
		}, now)
		return e
	}); err != nil {
		t.Fatalf("RegisterReplica x2: %v", err)
	}

	heartbeat := now.Add(30 * time.Second)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		return repo.UpdateHeartbeat(ctx, tx, replication.HeartbeatRequest{
			Region:        "ap-northeast-1",
			LastReplayLSN: "0/19000060",
			Now:           heartbeat,
		})
	}); err != nil {
		t.Fatalf("UpdateHeartbeat: %v", err)
	}

	var list []replication.Replica
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		rs, e := repo.ListReplicas(ctx, tx)
		if e != nil {
			return e
		}
		list = rs
		return nil
	}); err != nil {
		t.Fatalf("ListReplicas: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
	// region ASC.
	if list[0].Region != "ap-northeast-1" || list[1].Region != "ap-northeast-2" {
		t.Errorf("ordering broken: %q, %q", list[0].Region, list[1].Region)
	}
	// heartbeat 반영 검증.
	standby := list[0]
	if standby.LastReplayLSN != "0/19000060" {
		t.Errorf("LastReplayLSN = %q", standby.LastReplayLSN)
	}
	if standby.LastReplayAt.IsZero() {
		t.Errorf("LastReplayAt is zero")
	}
}

func TestUpdateHeartbeatUnknownRegionReturnsNotFound(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	repo := replicationrepo.New()

	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		return repo.UpdateHeartbeat(ctx, tx, replication.HeartbeatRequest{
			Region: "missing-region", Now: time.Now().UTC(),
		})
	})
	if err == nil {
		t.Fatal("expected ErrReplicaNotFound, got nil")
	}
}

func TestSetRoleAndRecordFailover(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	repo := replicationrepo.New()

	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.RegisterReplica(ctx, tx, replication.RegisterReplicaRequest{
			Region: "ap-northeast-2", Role: replication.RolePrimary, Endpoint: "https://primary",
		}, now)
		if e != nil {
			return e
		}
		_, e = repo.RegisterReplica(ctx, tx, replication.RegisterReplicaRequest{
			Region: "ap-northeast-1", Role: replication.RoleStandby, Endpoint: "https://standby",
		}, now)
		return e
	}); err != nil {
		t.Fatalf("RegisterReplica: %v", err)
	}

	// failover swap (안에서 SetRole 두 번 + Record + Link).
	swapAt := now.Add(time.Hour)
	var failoverID int64
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if e := repo.SetRole(ctx, tx, "ap-northeast-2", replication.RoleStandby); e != nil {
			return e
		}
		if e := repo.SetRole(ctx, tx, "ap-northeast-1", replication.RolePrimary); e != nil {
			return e
		}
		row, e := repo.RecordFailover(ctx, tx, replication.FailoverRequest{
			FromRegion:      "ap-northeast-2",
			ToRegion:        "ap-northeast-1",
			InitiatedByUser: "us_admin1",
			Reason:          "primary region outage",
			Now:             swapAt,
		})
		if e != nil {
			return e
		}
		failoverID = row.ID
		return repo.LinkFailoverAudit(ctx, tx, row.ID, 9876, swapAt.Add(time.Second))
	}); err != nil {
		t.Fatalf("failover sequence: %v", err)
	}
	if failoverID == 0 {
		t.Error("failoverID = 0, want > 0 (sqlite AUTOINCREMENT)")
	}

	// 검증: 두 replica role swap 됨.
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		oldPrimary, e := repo.GetReplica(ctx, tx, "ap-northeast-2")
		if e != nil {
			return e
		}
		if oldPrimary.Role != replication.RoleStandby {
			t.Errorf("old primary role = %q, want standby", oldPrimary.Role)
		}
		newPrimary, e := repo.GetReplica(ctx, tx, "ap-northeast-1")
		if e != nil {
			return e
		}
		if newPrimary.Role != replication.RolePrimary {
			t.Errorf("new primary role = %q, want primary", newPrimary.Role)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify swap: %v", err)
	}
}

func TestRecordFailoverSameRegionRejected(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	repo := replicationrepo.New()

	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.RecordFailover(ctx, tx, replication.FailoverRequest{
			FromRegion: "same", ToRegion: "same", Now: time.Now().UTC(),
		})
		return e
	})
	if err == nil {
		t.Fatal("expected ErrSameRegionFailover, got nil")
	}
}

// === standby-mode middleware ===

func TestStandbyMiddlewarePassThroughWhenDisabled(t *testing.T) {
	t.Parallel()
	cfg := replication.DefaultConfig() // Enabled=false
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := replication.StandbyReadOnlyMiddleware(cfg)
	srv := httptest.NewServer(mw(next))
	defer srv.Close()

	// write request should pass through.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/robots", strings.NewReader(`{}`))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if !called {
		t.Error("next handler not called")
	}
}

func TestStandbyMiddlewareBlocksWriteWhenStandby(t *testing.T) {
	t.Parallel()
	cfg := replication.Config{
		Enabled:         true,
		Region:          "ap-northeast-1",
		Role:            replication.RoleStandby,
		PrimaryEndpoint: "https://primary.example",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should NOT be called for standby write")
		w.WriteHeader(http.StatusOK)
	})
	mw := replication.StandbyReadOnlyMiddleware(cfg)
	srv := httptest.NewServer(mw(next))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/robots", strings.NewReader(`{}`))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Rosshield-Replica-Role"); got != "standby" {
		t.Errorf("X-Rosshield-Replica-Role = %q, want standby", got)
	}
	if got := resp.Header.Get("X-Rosshield-Primary-Endpoint"); got != "https://primary.example" {
		t.Errorf("X-Rosshield-Primary-Endpoint = %q", got)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "standby_read_only" {
		t.Errorf("body[error] = %q", body["error"])
	}
	if body["primary_endpoint"] != "https://primary.example" {
		t.Errorf("body[primary_endpoint] = %q", body["primary_endpoint"])
	}
}

func TestStandbyMiddlewareAllowsReadWhenStandby(t *testing.T) {
	t.Parallel()
	cfg := replication.Config{
		Enabled: true, Role: replication.RoleStandby, Region: "r",
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := replication.StandbyReadOnlyMiddleware(cfg)
	srv := httptest.NewServer(mw(next))
	defer srv.Close()

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		called = false
		req, _ := http.NewRequest(method, srv.URL+"/api/v1/robots", nil)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("Do %s: %v", method, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200", method, resp.StatusCode)
		}
		if !called {
			t.Errorf("%s: next not called", method)
		}
	}
}

func TestStandbyMiddlewareAllowsExemptPaths(t *testing.T) {
	t.Parallel()
	cfg := replication.Config{
		Enabled: true, Role: replication.RoleStandby, Region: "r",
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := replication.StandbyReadOnlyMiddleware(cfg)
	srv := httptest.NewServer(mw(next))
	defer srv.Close()

	// 모든 exempt path가 POST에도 통과해야 함 (heartbeat, failover).
	for _, path := range []string{
		"/api/v1/replication/heartbeat",
		"/api/v1/replication/failover",
		"/healthz",
	} {
		called = false
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(`{}`))
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("Do %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200 (exempt)", path, resp.StatusCode)
		}
		if !called {
			t.Errorf("%s: next not called (exempt should pass)", path)
		}
	}
}

func TestStandbyMiddlewareAllowsAllWhenPrimary(t *testing.T) {
	t.Parallel()
	cfg := replication.Config{
		Enabled: true, Role: replication.RolePrimary, Region: "r",
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := replication.StandbyReadOnlyMiddleware(cfg)
	srv := httptest.NewServer(mw(next))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/robots/x", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (primary pass)", resp.StatusCode)
	}
	if !called {
		t.Error("next not called (primary should pass)")
	}
}

// === Validation ===

func TestValidateRegisterRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		req     replication.RegisterReplicaRequest
		wantErr error
	}{
		{"empty region", replication.RegisterReplicaRequest{Role: replication.RolePrimary, Endpoint: "x"}, replication.ErrEmptyRegion},
		{"empty endpoint", replication.RegisterReplicaRequest{Region: "r", Role: replication.RolePrimary}, replication.ErrEmptyEndpoint},
		{"invalid role", replication.RegisterReplicaRequest{Region: "r", Role: "bogus", Endpoint: "x"}, replication.ErrInvalidRole},
		{"ok", replication.RegisterReplicaRequest{Region: "r", Role: replication.RolePrimary, Endpoint: "x"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := replication.ValidateRegisterRequest(c.req)
			if err != c.wantErr {
				t.Errorf("err = %v, want %v", err, c.wantErr)
			}
		})
	}
}

func TestValidateFailoverRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		req     replication.FailoverRequest
		wantErr error
	}{
		{"empty from", replication.FailoverRequest{ToRegion: "b"}, replication.ErrEmptyRegion},
		{"same region", replication.FailoverRequest{FromRegion: "a", ToRegion: "a"}, replication.ErrSameRegionFailover},
		{"ok", replication.FailoverRequest{FromRegion: "a", ToRegion: "b"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := replication.ValidateFailoverRequest(c.req)
			if err != c.wantErr {
				t.Errorf("err = %v, want %v", err, c.wantErr)
			}
		})
	}
}

// === env-leak guard ===

func TestMain(m *testing.M) {
	// 다른 패키지의 env override가 본 테스트에 leak되지 않도록 명시 clear.
	for _, k := range []string{
		"ROSSHIELD_REPLICATION_ENABLED",
		"ROSSHIELD_REPLICATION_REGION",
		"ROSSHIELD_REPLICATION_ROLE",
		"ROSSHIELD_REPLICATION_PRIMARY_ENDPOINT",
	} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
