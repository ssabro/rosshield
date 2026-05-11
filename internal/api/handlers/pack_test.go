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

func TestGetPackReturnsChecks(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	installSamplePack(t, f)
	token := f.loginAndGetToken(t)

	// 먼저 ListPacks로 packKey 확인.
	listReq, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET /api/v1/packs: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	var list struct {
		Packs []struct {
			PackKey string `json:"packKey"`
		} `json:"packs"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Packs) == 0 {
		t.Fatal("ListPacks returned 0 packs")
	}
	packKey := list.Packs[0].PackKey

	// GET /api/v1/packs/{packKey}
	req, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs/"+packKey, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/packs/%s: %v", packKey, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body: %s", resp.StatusCode, string(body))
	}

	var detail struct {
		PackKey string `json:"packKey"`
		Vendor  string `json:"vendor"`
		Checks  []struct {
			ID       string `json:"id"`
			CheckID  string `json:"checkId"`
			Title    string `json:"title"`
			Severity string `json:"severity"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.PackKey != packKey {
		t.Errorf("PackKey = %q, want %q", detail.PackKey, packKey)
	}
	if len(detail.Checks) == 0 {
		t.Fatal("Checks empty (built-in pack should have many checks)")
	}
	for _, c := range detail.Checks {
		if c.ID == "" || c.CheckID == "" || c.Title == "" {
			t.Errorf("check has empty fields: %+v", c)
			break
		}
	}
	// 결정성: CheckID 알파벳 정렬 검증
	for i := 1; i < len(detail.Checks); i++ {
		if detail.Checks[i-1].CheckID > detail.Checks[i].CheckID {
			t.Errorf("checks not sorted: %q > %q", detail.Checks[i-1].CheckID, detail.Checks[i].CheckID)
			break
		}
	}
}

func TestGetCheckReturnsDetail(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	installSamplePack(t, f)
	token := f.loginAndGetToken(t)

	// pack의 첫 check id 추출.
	listReq, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp, _ := http.DefaultClient.Do(listReq)
	defer func() { _ = listResp.Body.Close() }()
	var list struct {
		Packs []struct {
			PackKey string `json:"packKey"`
		} `json:"packs"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	packKey := list.Packs[0].PackKey

	detailReq, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs/"+packKey, nil)
	detailReq.Header.Set("Authorization", "Bearer "+token)
	detailResp, _ := http.DefaultClient.Do(detailReq)
	defer func() { _ = detailResp.Body.Close() }()
	var detail struct {
		Checks []struct {
			CheckID string `json:"checkId"`
		} `json:"checks"`
	}
	_ = json.NewDecoder(detailResp.Body).Decode(&detail)
	if len(detail.Checks) == 0 {
		t.Fatal("pack has no checks")
	}
	checkID := detail.Checks[0].CheckID

	// GET /api/v1/packs/{packKey}/checks/{checkId}
	req, _ := http.NewRequest(http.MethodGet,
		f.server.URL+"/api/v1/packs/"+packKey+"/checks/"+checkID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET check detail: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body: %s", resp.StatusCode, string(body))
	}

	var cd struct {
		ID             string          `json:"id"`
		CheckID        string          `json:"checkId"`
		PackKey        string          `json:"packKey"`
		Title          string          `json:"title"`
		Severity       string          `json:"severity"`
		AuditCommand   string          `json:"auditCommand"`
		EvaluationRule json.RawMessage `json:"evaluationRule"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cd.CheckID != checkID {
		t.Errorf("CheckID = %q, want %q", cd.CheckID, checkID)
	}
	if cd.PackKey != packKey {
		t.Errorf("PackKey = %q, want %q", cd.PackKey, packKey)
	}
	if cd.AuditCommand == "" {
		t.Error("AuditCommand empty")
	}
	if len(cd.EvaluationRule) == 0 {
		t.Error("EvaluationRule empty")
	}
}

func TestGetCheckNotFound(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	installSamplePack(t, f)
	token := f.loginAndGetToken(t)

	// 실 packKey 추출.
	listReq, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp, _ := http.DefaultClient.Do(listReq)
	defer func() { _ = listResp.Body.Close() }()
	var list struct {
		Packs []struct {
			PackKey string `json:"packKey"`
		} `json:"packs"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	packKey := list.Packs[0].PackKey

	req, _ := http.NewRequest(http.MethodGet,
		f.server.URL+"/api/v1/packs/"+packKey+"/checks/nonexistent-check-9999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

func TestGetCheckSelftestReturnsCases(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	installSamplePack(t, f)
	token := f.loginAndGetToken(t)

	// 첫 builtin pack의 packKey는 "rosshield-<name>-<version>" 패턴 — installSamplePack이
	// builtin tarGz를 caller tenant에 install했으므로 packKey는 동일.
	listReq, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp, _ := http.DefaultClient.Do(listReq)
	defer func() { _ = listResp.Body.Close() }()
	var list struct {
		Packs []struct {
			PackKey string `json:"packKey"`
		} `json:"packs"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	if len(list.Packs) == 0 {
		t.Skip("no packs installed")
	}
	packKey := list.Packs[0].PackKey

	// CIS pack의 1.5.1은 selftest 있음.
	url := f.server.URL + "/api/v1/packs/" + packKey + "/checks/1.5.1/selftest"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET selftest: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// installSamplePack이 ROS2 pack을 install한 경우 1.5.1은 없음. skip 대신 다른 check 시도.
		t.Skipf("selftest 1.5.1 not in this pack (got %d) — alphabet 첫 pack 의존", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body: %s", resp.StatusCode, string(body))
	}

	var cd struct {
		CheckID string                   `json:"checkId"`
		PackKey string                   `json:"packKey"`
		Cases   []map[string]interface{} `json:"cases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cd.CheckID != "1.5.1" {
		t.Errorf("CheckID = %q, want 1.5.1", cd.CheckID)
	}
	if len(cd.Cases) == 0 {
		t.Error("Cases empty (selftest fixture should have at least 1 case)")
	}
}

func TestGetCheckSelftestNotFoundForNonBuiltin(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	url := f.server.URL + "/api/v1/packs/some-tenant-pack-1.0/checks/CHECK-1/selftest"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404 (non-builtin packKey)", resp.StatusCode)
	}
}

func TestGetPackNotFound(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	req, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/v1/packs/nonexistent-key-123", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
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
