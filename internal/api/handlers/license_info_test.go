package handlers_test

// license_info_test.go — GET /api/v1/license 통합 테스트 (E24-C 후속).
//
// 시나리오:
//   1. License 미설정(community) → 200 + edition=community + quotas 0/0/0.
//   2. 인증 없음 → 401.
//
// enterprise edition 분기(features·만료·quotas 노출)는 license platform 단위 테스트
// (license_test.go)에서 이미 검증 — 본 통합 테스트는 fixture가 License nil인 community
// 응답·Auth 게이트만.

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestGetLicenseInfo_Community_NoLicense(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	resp := f.doRequest(t, "GET", "/api/v1/license", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Edition  string   `json:"edition"`
		Expired  bool     `json:"expired"`
		Features []string `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Edition != "community" {
		t.Errorf("edition=%q, want community", out.Edition)
	}
	if out.Expired {
		t.Error("Expired=true for community (no license)")
	}
	if len(out.Features) != 0 {
		t.Errorf("Features=%v, want empty", out.Features)
	}
}

func TestGetLicenseInfo_RequiresAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	resp := f.doRequest(t, "GET", "/api/v1/license", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}
