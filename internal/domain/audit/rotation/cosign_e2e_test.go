//go:build cosign_e2e

// cosign_e2e_test.go — 실 cosign binary 통합 검증 (D-AR-4 옵션 A wire 호환성).
//
// 실행:
//
//	go test -tags=cosign_e2e -count=1 -timeout=8m ./internal/domain/audit/rotation/...
//
// 본 파일은 build tag `cosign_e2e`가 켜진 빌드에서만 컴파일됩니다. CI 환경에서:
//   - `sigstore/cosign-installer@v3`로 cosign binary 설치
//   - `permissions: id-token: write`로 GitHub Actions OIDC token 활성
//   - cosign이 GitHub Actions 환경 변수(ACTIONS_ID_TOKEN_REQUEST_TOKEN/_URL)를
//     자동 감지하여 Fulcio에 keyless 인증서 발급 요청 + Rekor 등록
//
// 로컬에서는 cosign binary 부재 또는 OIDC token 없음 → t.Skip.
//
// 검증 항목:
//   - CosignSigner.Sign으로 실 bundle 생성 (Fulcio 인증서 + Rekor SET)
//   - `cosign verify-blob --bundle=... --certificate-identity=... -` 외부 호출이 OK
//   - bundle 1 byte 변조 → verify-blob non-zero exit
//
// 본 테스트는 매 실행 시 Sigstore public Fulcio/Rekor에 새 인증서·entry를 만들므로
// 외부 API 부하/quota 부담을 고려해 호출 최소화 (sign 1회 + verify OK/FAIL 2회).

package rotation_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
)

// skipIfCosignUnavailable는 cosign binary와 GitHub Actions OIDC env가 모두 있을 때만
// 진입을 허용합니다. 그 외 환경(로컬 dev, OIDC 없는 CI)은 t.Skip.
//
// cosign이 keyless 모드에서 ambient OIDC token 자동 감지: GitHub Actions는
// ACTIONS_ID_TOKEN_REQUEST_TOKEN + _URL이 set되면 cosign이 직접 fetch.
func skipIfCosignUnavailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("cosign"); err != nil {
		t.Skipf("cosign binary not in PATH: %v", err)
	}
	if os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN") == "" || os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") == "" {
		t.Skip("GitHub Actions OIDC env not set (ACTIONS_ID_TOKEN_REQUEST_TOKEN/_URL) — keyless sign 불가")
	}
}

// TestCosignE2E_SignAndVerifyRoundTrip — 실 cosign keyless 서명 → bundle → verify OK.
//
// GitHub Actions OIDC issuer = https://token.actions.githubusercontent.com.
// identity는 workflow path + ref 형식 (예: https://github.com/ssabro/rosshield/.github/workflows/ci.yml@refs/heads/main).
//
// 본 테스트는 identity regex로 verify — workflow 정확한 path는 환경에 따라 다르지만 host 부분은
// token.actions.githubusercontent.com OIDC issuer로 고정.
func TestCosignE2E_SignAndVerifyRoundTrip(t *testing.T) {
	skipIfCosignUnavailable(t)

	// 1. CosignSigner로 archive 서명.
	signer := rotation.NewCosignSigner(rotation.SignerConfig{Enabled: true})
	archive := []byte("rosshield-audit-rotation-e2e-payload")

	ctx, cancel := contextWithTimeout(t, 120) // 120s — Fulcio + Rekor 호출 지연 여유.
	defer cancel()

	bundle, err := signer.Sign(ctx, archive)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(bundle) == 0 {
		t.Fatal("Sign returned empty bundle")
	}

	// 2. bundle을 임시 파일로 작성 후 cosign verify-blob 외부 호출.
	bundlePath := filepath.Join(t.TempDir(), "seg.cosign.bundle")
	if err := os.WriteFile(bundlePath, bundle, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	// GitHub Actions OIDC identity는 workflow 경로 + ref. CI repository_owner를 식별자로 사용.
	identityRegex := "^https://github\\.com/" + githubRepoOwnerOrAny() + "/"

	stdout, stderr, err := runCosignVerify(ctx, bundlePath, archive, identityRegex,
		"https://token.actions.githubusercontent.com")
	if err != nil {
		t.Fatalf("cosign verify-blob OK case fail: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "Verified OK") {
		t.Errorf("verify output does not contain 'Verified OK' — stdout=%s, stderr=%s", stdout, stderr)
	}

	// 3. bundle 1 byte 변조 → verify 실패.
	tampered := make([]byte, len(bundle))
	copy(tampered, bundle)
	tampered[len(tampered)/2] ^= 0xff
	tamperedPath := filepath.Join(t.TempDir(), "tampered.bundle")
	if err := os.WriteFile(tamperedPath, tampered, 0o644); err != nil {
		t.Fatalf("write tampered: %v", err)
	}
	_, _, err = runCosignVerify(ctx, tamperedPath, archive, identityRegex,
		"https://token.actions.githubusercontent.com")
	if err == nil {
		t.Error("verify-blob succeeded on tampered bundle — want error")
	}
}

// runCosignVerify는 cosign verify-blob을 외부 호출합니다 (audit-verify CLI와 같은 옵션 A).
func runCosignVerify(ctx context.Context, bundlePath string, archive []byte, identityRegex, oidcIssuer string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "cosign",
		"verify-blob",
		"--bundle="+bundlePath,
		"--certificate-identity-regexp="+identityRegex,
		"--certificate-oidc-issuer="+oidcIssuer,
		"-")
	cmd.Stdin = bytes.NewReader(archive)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// githubRepoOwnerOrAny는 GitHub Actions 환경의 repository owner를 반환합니다 (예: ssabro).
// CI 외 환경(t.Skip으로 이미 차단되지만 안전)에서는 ".*"로 fallback.
func githubRepoOwnerOrAny() string {
	if owner := os.Getenv("GITHUB_REPOSITORY_OWNER"); owner != "" {
		return owner
	}
	return ".*"
}

// contextWithTimeout는 testing.T 친화 context.WithTimeout helper.
func contextWithTimeout(t *testing.T, sec int) (context.Context, context.CancelFunc) {
	t.Helper()
	if sec <= 0 {
		sec = 60
	}
	return context.WithTimeout(context.Background(), time.Duration(sec)*time.Second)
}
