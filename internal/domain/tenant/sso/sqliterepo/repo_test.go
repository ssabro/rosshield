package sqliterepo_test

// repo_test.go — E20-A SSO sqliterepo 단위 테스트.
//
// 검증 포인트:
//
//	Provider CRUD + tenant 격리.
//	StartLogin이 OIDC vs SAML에 따라 PKCE/Nonce vs RelayState를 채움.
//	CompleteLogin이 만료·재사용·잘못된 state를 거부.
//	UpsertExternalIdentity가 INSERT 후 호출 시 last_seen_at만 갱신(first_seen_at 보존).
//	Audit emit 횟수.
//	Cross-tenant lookup이 ErrProviderNotFound로 마스킹.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === fakes ===

type fakeAuditEmitter struct {
	mu             sync.Mutex
	providerEvents []string // "created"|"updated"|"deleted"
	loginStarted   int
	loginCompleted int
	loginOK        []bool
}

func (a *fakeAuditEmitter) EmitProviderChanged(_ context.Context, _ storage.Tx, _ sso.Provider, action string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.providerEvents = append(a.providerEvents, action)
	return nil
}
func (a *fakeAuditEmitter) EmitLoginStarted(_ context.Context, _ storage.Tx, _ sso.LoginAttempt) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.loginStarted++
	return nil
}
func (a *fakeAuditEmitter) EmitLoginCompleted(_ context.Context, _ storage.Tx, _ sso.LoginAttempt, _ sso.ExternalIdentity, ok bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.loginCompleted++
	a.loginOK = append(a.loginOK, ok)
	return nil
}

// === fake clock ===

type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func newStepClock(start time.Time) *stepClock { return &stepClock{now: start} }
func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// === harness ===

const (
	testTenant    = "tn_E20A"
	otherTenant   = "tn_OTHER"
	testUser      = "us_E20A"
	otherTestUser = "us_OTHER"
)

type harness struct {
	repo    *sqliterepo.Repo
	emitter *fakeAuditEmitter
	store   storage.Storage
	clock   *stepClock
}

func newTestHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: filepath.Join(dir, "sso.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		// 두 tenant + user 시드 — cross-tenant 격리 검증용.
		for _, tn := range []struct{ id string }{{testTenant}, {otherTenant}} {
			if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, 'desktop_free', ?)`,
				tn.id, "tenant-"+tn.id, now); e != nil {
				return e
			}
		}
		users := []struct{ id, tid string }{{testUser, testTenant}, {otherTestUser, otherTenant}}
		for _, u := range users {
			if _, e := tx.Exec(ctx, `INSERT INTO users (id, tenant_id, email, display_name, auth_provider, status, created_at, updated_at)
VALUES (?, ?, ?, 'U', 'oidc', 'active', ?, ?)`, u.id, u.tid, u.id+"@x.test", now, now); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	em := &fakeAuditEmitter{}
	clk := newStepClock(time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC))
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock: clk,
		IDGen: idgen.NewULID(),
		Audit: em,
	})
	return &harness{repo: repo, emitter: em, store: store, clock: clk}
}

func tenantCtx(tid string) context.Context {
	return storage.WithTenantID(context.Background(), storage.TenantID(tid))
}

// === Provider CRUD + tenant isolation ===

func TestCreateProviderInsertsAndAuditsAndIsTenantScoped(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	var created sso.Provider
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, err := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			TenantID: testTenant,
			Type:     sso.TypeOIDC,
			Name:     "Google Workspace",
			Enabled:  true,
			Config:   json.RawMessage(`{"issuer":"https://accounts.google.com","clientId":"abc","redirectUri":"https://app/callback"}`),
		})
		created = p
		return err
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if created.ID == "" || !startsWith(created.ID, "ssop_") {
		t.Errorf("ID = %q, want ssop_ prefix", created.ID)
	}
	if !created.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if string(created.Type) != "oidc" {
		t.Errorf("Type = %q, want oidc", created.Type)
	}
	if got := h.emitter.providerEvents; len(got) != 1 || got[0] != "created" {
		t.Errorf("provider audit events = %v, want [created]", got)
	}

	// cross-tenant lookup → ErrProviderNotFound
	if err := h.store.Tx(tenantCtx(otherTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.GetProvider(ctx, tx, created.ID)
		return e
	}); !errors.Is(err, sso.ErrProviderNotFound) {
		t.Errorf("cross-tenant Get = %v, want ErrProviderNotFound", err)
	}

	// 같은 tenant + 같은 name → ErrProviderNameExists
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type:    sso.TypeSAML,
			Name:    "Google Workspace", // duplicate
			Enabled: true,
			Config:  json.RawMessage(`{"metadataUrl":"https://x"}`),
		})
		return e
	}); !errors.Is(err, sso.ErrProviderNameExists) {
		t.Errorf("duplicate Name = %v, want ErrProviderNameExists", err)
	}
}

func TestUpdateAndDeleteProviderEmitAuditAndRespectTenant(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	var pid string
	_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, _ := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeOIDC, Name: "Okta", Enabled: true,
			Config: json.RawMessage(`{"issuer":"https://okta.com"}`),
		})
		pid = p.ID
		return nil
	})

	// 부분 갱신 — Enabled false로
	enabled := false
	newName := "Okta - Engineering"
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.UpdateProvider(ctx, tx, sso.UpdateProviderRequest{
			ID: pid, TenantID: testTenant,
			Name: &newName, Enabled: &enabled,
		})
		return e
	}); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}

	// cross-tenant Update → ErrProviderNotFound
	if err := h.store.Tx(tenantCtx(otherTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.UpdateProvider(ctx, tx, sso.UpdateProviderRequest{
			ID: pid, TenantID: otherTenant,
			Enabled: &enabled,
		})
		return e
	}); !errors.Is(err, sso.ErrProviderNotFound) {
		t.Errorf("cross-tenant Update = %v, want ErrProviderNotFound", err)
	}

	// Delete (tenant scope OK)
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		return h.repo.DeleteProvider(ctx, tx, pid)
	}); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	// 두 번 Delete → 두 번째는 ErrProviderNotFound
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		return h.repo.DeleteProvider(ctx, tx, pid)
	}); !errors.Is(err, sso.ErrProviderNotFound) {
		t.Errorf("second Delete = %v, want ErrProviderNotFound", err)
	}

	// audit events: created + updated + deleted (다른 tenant 시도는 audit 없이 거부)
	got := h.emitter.providerEvents
	if len(got) != 3 || got[0] != "created" || got[1] != "updated" || got[2] != "deleted" {
		t.Errorf("provider events = %v, want [created updated deleted]", got)
	}
}

// === StartLogin/CompleteLogin ===

func TestStartLoginOIDCPersistsPKCEAndNonce(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	var pid string
	_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, _ := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeOIDC, Name: "Google", Enabled: true,
			Config: json.RawMessage(`{"issuer":"https://accounts.google.com","clientId":"abc","redirectUri":"https://app/callback"}`),
		})
		pid = p.ID
		return nil
	})

	var result sso.StartLoginResult
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		r, e := h.repo.StartLogin(ctx, tx, sso.StartLoginRequest{ProviderID: pid})
		result = r
		return e
	}); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if result.State == "" {
		t.Errorf("State empty")
	}
	if result.Attempt.PKCEVerifier == "" {
		t.Errorf("PKCEVerifier empty for OIDC")
	}
	if result.Attempt.Nonce == "" {
		t.Errorf("Nonce empty for OIDC")
	}
	if result.Attempt.RelayState != "" {
		t.Errorf("RelayState should be empty for OIDC, got %q", result.Attempt.RelayState)
	}
	if h.emitter.loginStarted != 1 {
		t.Errorf("loginStarted = %d, want 1", h.emitter.loginStarted)
	}

	// CompleteLogin — 성공
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{
			State: result.State,
			Code:  "fake_authorization_code",
		})
		return e
	}); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}

	// 두 번째 CompleteLogin (state 재사용) → ErrInvalidState
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{State: result.State, Code: "x"})
		return e
	}); !errors.Is(err, sso.ErrInvalidState) {
		t.Errorf("reused state = %v, want ErrInvalidState", err)
	}
}

func TestStartLoginSAMLPersistsRelayState(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	var pid string
	_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, _ := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeSAML, Name: "Okta SAML", Enabled: true,
			Config: json.RawMessage(`{"metadataUrl":"https://okta/metadata"}`),
		})
		pid = p.ID
		return nil
	})

	var result sso.StartLoginResult
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		r, e := h.repo.StartLogin(ctx, tx, sso.StartLoginRequest{
			ProviderID:    pid,
			RedirectAfter: "/dashboard",
		})
		result = r
		return e
	}); err != nil {
		t.Fatalf("StartLogin SAML: %v", err)
	}
	if result.Attempt.RelayState != "/dashboard" {
		t.Errorf("RelayState = %q, want /dashboard", result.Attempt.RelayState)
	}
	if result.Attempt.PKCEVerifier != "" || result.Attempt.Nonce != "" {
		t.Errorf("SAML attempt should not have PKCE/Nonce")
	}
}

func TestStartLoginRejectsDisabledProvider(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	var pid string
	_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, _ := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeOIDC, Name: "Disabled", Enabled: false,
			Config: json.RawMessage(`{"issuer":"https://x"}`),
		})
		pid = p.ID
		return nil
	})

	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.StartLogin(ctx, tx, sso.StartLoginRequest{ProviderID: pid})
		return e
	}); !errors.Is(err, sso.ErrProviderDisabled) {
		t.Errorf("disabled provider = %v, want ErrProviderDisabled", err)
	}
}

func TestCompleteLoginRejectsExpiredAndUnknownState(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	var pid string
	_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, _ := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeOIDC, Name: "Test", Enabled: true,
			Config: json.RawMessage(`{"issuer":"https://x"}`),
		})
		pid = p.ID
		return nil
	})

	// Unknown state
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{State: "nonexistent_state_xyz"})
		return e
	}); !errors.Is(err, sso.ErrInvalidState) {
		t.Errorf("unknown state = %v, want ErrInvalidState", err)
	}

	// Empty state
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{State: ""})
		return e
	}); !errors.Is(err, sso.ErrEmptyState) {
		t.Errorf("empty state = %v, want ErrEmptyState", err)
	}

	// 만료 후 호출
	var state string
	_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		r, _ := h.repo.StartLogin(ctx, tx, sso.StartLoginRequest{ProviderID: pid})
		state = r.State
		return nil
	})
	h.clock.Advance(sso.DefaultAttemptTTL + time.Second)

	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{State: state, Code: "x"})
		return e
	}); !errors.Is(err, sso.ErrStateExpired) {
		t.Errorf("expired state = %v, want ErrStateExpired", err)
	}
}

// === ExternalIdentity upsert ===

func TestUpsertExternalIdentityInsertsAndUpdatesLastSeen(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	var pid string
	_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, _ := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeOIDC, Name: "Google", Enabled: true,
			Config: json.RawMessage(`{"issuer":"https://accounts.google.com"}`),
		})
		pid = p.ID
		return nil
	})

	// 1차 INSERT
	var first sso.ExternalIdentity
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.UpsertExternalIdentity(ctx, tx, sso.ExternalIdentity{
			ProviderID:      pid,
			ExternalSubject: "google-sub-12345",
			UserID:          testUser,
			Email:           "alice@x.test",
		})
		first = out
		return e
	}); err != nil {
		t.Fatalf("UpsertExternalIdentity 1st: %v", err)
	}
	if first.FirstSeenAt.IsZero() {
		t.Errorf("FirstSeenAt zero after INSERT")
	}
	if !first.FirstSeenAt.Equal(first.LastSeenAt) {
		t.Errorf("FirstSeenAt and LastSeenAt should match on first INSERT")
	}

	// clock 진행 후 2차 호출 — last_seen_at만 갱신
	h.clock.Advance(time.Hour)
	var second sso.ExternalIdentity
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.UpsertExternalIdentity(ctx, tx, sso.ExternalIdentity{
			ProviderID:      pid,
			ExternalSubject: "google-sub-12345",
			UserID:          testUser,
			Email:           "alice-renamed@x.test",
		})
		second = out
		return e
	}); err != nil {
		t.Fatalf("UpsertExternalIdentity 2nd: %v", err)
	}
	if !second.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Errorf("FirstSeenAt changed: %v vs %v (should be preserved)", first.FirstSeenAt, second.FirstSeenAt)
	}
	if !second.LastSeenAt.After(first.LastSeenAt) {
		t.Errorf("LastSeenAt should advance: first=%v second=%v", first.LastSeenAt, second.LastSeenAt)
	}
	if second.Email != "alice-renamed@x.test" {
		t.Errorf("Email = %q, want renamed", second.Email)
	}

	// 빈 subject
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.UpsertExternalIdentity(ctx, tx, sso.ExternalIdentity{
			ProviderID: pid, UserID: testUser,
		})
		return e
	}); !errors.Is(err, sso.ErrEmptySubject) {
		t.Errorf("empty subject = %v, want ErrEmptySubject", err)
	}
}

// === Validation ===

func TestCreateProviderValidatesInput(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	cases := []struct {
		name string
		req  sso.CreateProviderRequest
		want error
	}{
		{"empty name", sso.CreateProviderRequest{Type: sso.TypeOIDC, Config: json.RawMessage(`{}`)}, sso.ErrEmptyName},
		{"empty config", sso.CreateProviderRequest{Type: sso.TypeOIDC, Name: "x"}, sso.ErrEmptyConfig},
		{"invalid type", sso.CreateProviderRequest{Type: sso.Type("ldap"), Name: "x", Config: json.RawMessage(`{}`)}, sso.ErrUnsupportedType},
		{"malformed config", sso.CreateProviderRequest{Type: sso.TypeOIDC, Name: "x", Config: json.RawMessage(`{not-json}`)}, sso.ErrEmptyConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
				_, e := h.repo.CreateProvider(ctx, tx, tc.req)
				return e
			})
			if !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestListProvidersReturnsTenantScoped(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	// testTenant에 2개, otherTenant에 1개
	for _, n := range []string{"Google", "Okta"} {
		_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
			_, e := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
				Type: sso.TypeOIDC, Name: n, Enabled: true,
				Config: json.RawMessage(`{"issuer":"https://x"}`),
			})
			return e
		})
	}
	_ = h.store.Tx(tenantCtx(otherTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeSAML, Name: "Other-Okta", Enabled: true,
			Config: json.RawMessage(`{"metadataUrl":"https://x"}`),
		})
		return e
	})

	var list []sso.Provider
	_ = h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.ListProviders(ctx, tx)
		list = out
		return e
	})
	if len(list) != 2 {
		t.Errorf("List(testTenant) = %d items, want 2", len(list))
	}
	for _, p := range list {
		if p.TenantID != testTenant {
			t.Errorf("got provider with TenantID = %q, want %q", p.TenantID, testTenant)
		}
	}
}

// startsWith는 strings.HasPrefix 단축 (test 파일 격리).
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
