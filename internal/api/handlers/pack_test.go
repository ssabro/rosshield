package handlers_test

// pack_test.go — GET /api/v1/packs 회귀.
//
// 호출자 tenant에 builtin pack을 설치 → GET /api/v1/packs로 응답 확인.
// IsBuiltin=true 검증은 system tenant FK seed 필요(별 epic) — 본 테스트는 응답
// 형식·인증·empty 케이스만.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	builtinpacks "github.com/ssabro/rosshield/internal/builtin/packs"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// installSamplePack은 호출자 tenant에 builtin pack 한 개를 설치합니다.
//
// embed _archives 비어있으면 t.Skip — make pack-archive 미실행.
func installSamplePack(t *testing.T, f *testFixture) {
	t.Helper()
	seeded, err := builtinpacks.Builtins()
	if err != nil {
		t.Skipf("no built-in packs embedded: %v", err)
	}
	if len(seeded) == 0 {
		t.Skip("Builtins() returned empty")
	}
	p := seeded[0]
	tenantCtx := storage.WithTenantID(context.Background(), f.tenantID)
	err = f.storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		// 첫 trust 키(dev signer)로 install — dev 빌드 archive와 매칭.
		te := p.TrustBundle[0]
		_, e := f.bench.InstallPack(ctx, tx, f.tenantID, p.TarGz, te.PublicKey, te.SignerKeyID, "test-actor")
		return e
	})
	if err != nil {
		t.Fatalf("InstallPack: %v", err)
	}
}

func TestListPacksReturnsInstalledPack(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	installSamplePack(t, f)
	token := f.loginAndGetToken(t)

	req, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/packs: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Packs []struct {
			ID        string `json:"id"`
			TenantID  string `json:"tenantId"`
			PackKey   string `json:"packKey"`
			Name      string `json:"name"`
			Vendor    string `json:"vendor"`
			Version   string `json:"version"`
			IsBuiltin bool   `json:"isBuiltin"`
		} `json:"packs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Packs) != 1 {
		t.Fatalf("got %d packs, want 1", len(out.Packs))
	}
	got := out.Packs[0]
	if got.TenantID != string(f.tenantID) {
		t.Errorf("TenantID = %q, want %q", got.TenantID, f.tenantID)
	}
	if got.IsBuiltin {
		t.Errorf("IsBuiltin = true, want false (installed to caller tenant, not system)")
	}
	if got.Vendor == "" {
		t.Error("Vendor empty")
	}
	if got.PackKey == "" {
		t.Error("PackKey empty")
	}
}

func TestListPacksRequiresAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	resp, err := http.Get(f.server.URL + "/api/v1/packs")
	if err != nil {
		t.Fatalf("GET unauth: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", resp.StatusCode)
	}
}

func TestListPacksReturnsEmptyWhenNoneSeeded(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	req, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/packs: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200 (empty list)", resp.StatusCode)
	}

	var out struct {
		Packs []json.RawMessage `json:"packs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Packs) != 0 {
		t.Errorf("got %d packs, want 0 (no seed)", len(out.Packs))
	}
}
