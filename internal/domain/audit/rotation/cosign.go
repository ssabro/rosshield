package rotation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Signer는 archive bytes를 받아 외부 검증 가능한 signature bundle을 생성합니다.
//
// 본 interface는 rotation 패키지가 cosign 구현에 직접 의존하지 않게 하기 위한
// seam — Rotator는 Sign·Enabled 두 method만 호출하고, 실제 구현은 옵션 A(CosignSigner,
// 외부 cosign CLI) 또는 test 용 FakeSigner로 주입.
//
// 반환 bundle은 audit_rotation_segments.cosign_bundle BYTEA column에 저장됨
// (마이그레이션 0032에서 reserve). nil/빈 bundle은 "서명 skip" 의미 — bundle 길이로
// 활성 여부를 판단하면 안 됨 (Enabled() 별도 호출).
type Signer interface {
	// Sign은 archive bytes를 서명합니다.
	//
	// 비활성 signer (Enabled()=false)는 nil, nil 리턴 — Rotator는 cosign_bundle을 빈 채로
	// segment를 INSERT. 활성인데 cosign CLI 실패 시 error 반환 → rotation Tx 자체 rollback.
	Sign(ctx context.Context, archive []byte) ([]byte, error)

	// Enabled는 본 signer가 실제 서명을 수행하는지 리턴합니다.
	// 비활성(예: env 미설정) 신호로 활용 — Rotator는 비활성 signer에 대해 Sign 호출 자체를 skip.
	Enabled() bool
}

// SignerConfig는 cosign keyless signer의 env 기반 config입니다.
//
// 환경 변수 매핑 (main.go에서 결선):
//   - ROSSHIELD_COSIGN_ENABLED=true|false  → Enabled
//   - ROSSHIELD_COSIGN_BINARY=/usr/bin/cosign → BinaryPath ("" = "cosign" PATH lookup)
//   - ROSSHIELD_COSIGN_IDENTITY=admin@example.com → Identity (OIDC sub claim 기대치)
//   - ROSSHIELD_COSIGN_FULCIO_URL=https://...  → FulcioURL ("" = Sigstore public)
//   - ROSSHIELD_COSIGN_REKOR_URL=https://...   → RekorURL ("" = Sigstore public)
//
// 에어갭 customer는 Enabled=false로 비활성 (cosign 의존 0) — 추후 plain ed25519
// alternative (D-AR-5)로 carryover 가능.
type SignerConfig struct {
	Enabled    bool
	BinaryPath string
	Identity   string
	FulcioURL  string
	RekorURL   string
}

// LoadSignerConfigFromEnv는 ROSSHIELD_COSIGN_* env로 SignerConfig를 만듭니다.
//
// 빈 값/미설정 env는 default — Enabled=false (서명 skip), BinaryPath=""(="cosign").
// 잘못된 ENABLED 값(yes/1 외)은 false로 처리 — env strconv 실패 시 silent disable.
func LoadSignerConfigFromEnv() SignerConfig {
	cfg := SignerConfig{
		BinaryPath: strings.TrimSpace(os.Getenv("ROSSHIELD_COSIGN_BINARY")),
		Identity:   strings.TrimSpace(os.Getenv("ROSSHIELD_COSIGN_IDENTITY")),
		FulcioURL:  strings.TrimSpace(os.Getenv("ROSSHIELD_COSIGN_FULCIO_URL")),
		RekorURL:   strings.TrimSpace(os.Getenv("ROSSHIELD_COSIGN_REKOR_URL")),
	}
	if s := strings.TrimSpace(os.Getenv("ROSSHIELD_COSIGN_ENABLED")); s != "" {
		if b, err := strconv.ParseBool(s); err == nil {
			cfg.Enabled = b
		}
	}
	return cfg
}

// CosignSigner는 외부 cosign CLI를 호출해 archive blob을 keyless 서명합니다 (옵션 A).
//
// 결정 근거 (design doc D-AR-4):
//   - 옵션 A(외부 CLI) 채택 — cosign 표준 동작 100% 일치, 빌드 단순.
//   - 옵션 B(sigstore-go SDK)는 binary 의존 0이지만 Fulcio CA + Rekor 통합 복잡성·SDK
//     크기 trade-off로 후속 (별 epic).
//   - 에어갭 customer는 Enabled=false + 운영 doc로 cosign binary 별도 배포 안내.
//
// 동작:
//   - Sign 호출 시 `cosign sign-blob --bundle=- --yes [--fulcio-url=...] [--rekor-url=...] -`
//     execution. archive를 stdin으로 전달, bundle을 stdout으로 수신.
//   - cosign exit code != 0이면 stderr 캡처해 error 반환 → rotation Tx rollback.
//
// 한계 (본 round 미해결, 별 epic):
//   - Verify는 verify CLI (rosshield-audit-verify) 단독 책임 — 본 struct의 Verify는 stub.
//   - e2e test는 실 cosign CLI 의존이라 build tag `cosign_e2e` (CI default skip).
type CosignSigner struct {
	binaryPath string
	identity   string
	fulcioURL  string
	rekorURL   string
	enabled    bool
}

// NewCosignSigner는 config 기반으로 signer를 만듭니다.
//
// cfg.Enabled=false면 signer.Sign은 (nil, nil) — Rotator는 cosign_bundle 빈 채로 진행.
// cfg.BinaryPath=""면 "cosign"로 fallback (PATH lookup).
func NewCosignSigner(cfg SignerConfig) *CosignSigner {
	return &CosignSigner{
		binaryPath: cfg.BinaryPath,
		identity:   cfg.Identity,
		fulcioURL:  cfg.FulcioURL,
		rekorURL:   cfg.RekorURL,
		enabled:    cfg.Enabled,
	}
}

// Enabled는 signer 활성 여부를 리턴합니다 (nil safe).
func (s *CosignSigner) Enabled() bool {
	return s != nil && s.enabled
}

// Sign은 archive bytes를 cosign keyless로 서명하고 bundle bytes를 리턴합니다.
//
// 비활성(Enabled=false) 시 (nil, nil) — Rotator가 cosign_bundle 컬럼에 NULL 저장.
//
// 활성인데 cosign 실행 실패(binary 부재·OIDC 실패·Fulcio 거부 등) 시 stderr 캡처한
// error 반환 — Rotator가 Tx rollback → 부분 archive 잔존 방지.
//
// ctx 만료는 exec.CommandContext가 직접 전파 — 장기 행위 차단.
func (s *CosignSigner) Sign(ctx context.Context, archive []byte) ([]byte, error) {
	if !s.Enabled() {
		return nil, nil
	}
	if len(archive) == 0 {
		return nil, errors.New("rotation: cosign sign: empty archive")
	}

	binary := s.binaryPath
	if binary == "" {
		binary = "cosign"
	}

	// cosign 2.x: `--bundle=-` (stdout) 옵션은 신뢰할 수 없음 — stdout에 base64 signature
	// (`MEUC...` ECDSA DER) + bundle JSON이 혼재될 수 있어 verify-blob의 JSON parser가
	// "invalid character 'M'" 에러. 표준 패턴: 임시 파일에 bundle 받은 후 ReadFile.
	bundleFile, err := os.CreateTemp("", "rosshield-cosign-bundle-*.json")
	if err != nil {
		return nil, fmt.Errorf("rotation: cosign sign-blob: tmp file: %w", err)
	}
	bundlePath := bundleFile.Name()
	_ = bundleFile.Close() // cosign이 직접 write — 우리는 path만 전달.
	defer func() { _ = os.Remove(bundlePath) }()

	// `cosign sign-blob --bundle <tmpfile> --yes [--fulcio-url=...] [--rekor-url=...] -`
	// --yes — interactive confirmation 우회 (server 배포에서 필수)
	// --bundle <file> — bundle JSON을 별 파일에 작성
	// trailing `-` — stdin에서 blob 읽기
	args := []string{"sign-blob", "--yes", "--bundle=" + bundlePath}
	if s.fulcioURL != "" {
		args = append(args, "--fulcio-url="+s.fulcioURL)
	}
	if s.rekorURL != "" {
		args = append(args, "--rekor-url="+s.rekorURL)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = bytes.NewReader(archive)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout // 의도적으로 capture — base64 signature가 들어가지만 본 함수는 무시.
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rotation: cosign sign-blob: %w (stderr: %s)",
			err, strings.TrimSpace(stderr.String()))
	}

	bundle, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("rotation: cosign sign-blob: read bundle file: %w", err)
	}
	if len(bundle) == 0 {
		return nil, errors.New("rotation: cosign sign-blob: empty bundle file")
	}
	return bundle, nil
}

// Identity는 signer에 설정된 OIDC identity를 리턴합니다 (운영 doc · log 용).
//
// 본 값은 cosign verify-blob --certificate-identity=... 검증 측에서 사용 — server는
// 서명 시 직접 강제하지 않음 (cosign이 ambient OIDC token 또는 interactive flow 사용).
func (s *CosignSigner) Identity() string {
	if s == nil {
		return ""
	}
	return s.identity
}

// --- test helper ---

// FakeSigner는 unit test 용 in-process Signer입니다.
//
// 외부 cosign binary 없이 Rotator 결선·DB column 채움을 검증하기 위해 cosign_test.go +
// rotation_test.go 등에서 사용. signFunc=nil이면 비활성 signer (Enabled=false).
type FakeSigner struct {
	signFunc func(archive []byte) ([]byte, error)
}

// NewFakeSigner는 주어진 signFunc로 FakeSigner를 만듭니다.
// signFunc=nil → 비활성 (Sign이 nil, nil 리턴 + Enabled=false).
func NewFakeSigner(signFunc func([]byte) ([]byte, error)) *FakeSigner {
	return &FakeSigner{signFunc: signFunc}
}

// Sign은 signFunc를 호출합니다. ctx 만료 check는 호출자 책임.
func (f *FakeSigner) Sign(_ context.Context, archive []byte) ([]byte, error) {
	if f == nil || f.signFunc == nil {
		return nil, nil
	}
	return f.signFunc(archive)
}

// Enabled는 signFunc 설정 여부를 리턴합니다.
func (f *FakeSigner) Enabled() bool {
	return f != nil && f.signFunc != nil
}
