package rotation_test

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// 본 파일은 rotation 패키지의 cosign Signer 단위 test를 cover합니다 (D-AR-4 옵션 A 외부 CLI).
//
// 본 round 범위 (의도적으로 좁힘):
//   - LoadSignerConfigFromEnv: env → SignerConfig 매핑 + ENABLED parse fallback.
//   - CosignSigner.Enabled / Sign 비활성 경로.
//   - FakeSigner: signFunc 호출 + Enabled 신호.
//   - Rotator + FakeSigner 결선: cosign_bundle column 채움 + 비활성 시 NULL.
//   - 활성 signer 실패 시 Rotate가 Tx rollback (cosign 호출 실패 propagation).
//
// 본 round 미커버 (별 epic / build tag):
//   - 실 cosign CLI 통합 (build tag `cosign_e2e` 후속).
//   - Verify 경로 (rosshield-audit-verify CLI 책임).

// --- SignerConfig / LoadSignerConfigFromEnv ---

func TestLoadSignerConfigFromEnv_Defaults(t *testing.T) {
	// 본 test는 env mutate 필요 — t.Setenv는 default를 빈 값으로 둠.
	t.Setenv("ROSSHIELD_COSIGN_ENABLED", "")
	t.Setenv("ROSSHIELD_COSIGN_BINARY", "")
	t.Setenv("ROSSHIELD_COSIGN_IDENTITY", "")
	t.Setenv("ROSSHIELD_COSIGN_FULCIO_URL", "")
	t.Setenv("ROSSHIELD_COSIGN_REKOR_URL", "")

	cfg := rotation.LoadSignerConfigFromEnv()
	if cfg.Enabled {
		t.Errorf("Enabled = true, want false (default)")
	}
	if cfg.BinaryPath != "" {
		t.Errorf("BinaryPath = %q, want empty", cfg.BinaryPath)
	}
	if cfg.Identity != "" || cfg.FulcioURL != "" || cfg.RekorURL != "" {
		t.Errorf("expected empty Identity/Fulcio/Rekor, got %+v", cfg)
	}
}

func TestLoadSignerConfigFromEnv_AllSet(t *testing.T) {
	t.Setenv("ROSSHIELD_COSIGN_ENABLED", "true")
	t.Setenv("ROSSHIELD_COSIGN_BINARY", "/opt/cosign/bin/cosign")
	t.Setenv("ROSSHIELD_COSIGN_IDENTITY", "admin@example.com")
	t.Setenv("ROSSHIELD_COSIGN_FULCIO_URL", "https://fulcio.test/")
	t.Setenv("ROSSHIELD_COSIGN_REKOR_URL", "https://rekor.test/")

	cfg := rotation.LoadSignerConfigFromEnv()
	if !cfg.Enabled {
		t.Error("Enabled = false, want true")
	}
	if cfg.BinaryPath != "/opt/cosign/bin/cosign" {
		t.Errorf("BinaryPath = %q", cfg.BinaryPath)
	}
	if cfg.Identity != "admin@example.com" {
		t.Errorf("Identity = %q", cfg.Identity)
	}
	if cfg.FulcioURL != "https://fulcio.test/" {
		t.Errorf("FulcioURL = %q", cfg.FulcioURL)
	}
	if cfg.RekorURL != "https://rekor.test/" {
		t.Errorf("RekorURL = %q", cfg.RekorURL)
	}
}

func TestLoadSignerConfigFromEnv_InvalidEnabledFallsBackToFalse(t *testing.T) {
	t.Setenv("ROSSHIELD_COSIGN_ENABLED", "notabool")

	cfg := rotation.LoadSignerConfigFromEnv()
	if cfg.Enabled {
		t.Error("expected Enabled=false on invalid bool, got true")
	}
}

func TestLoadSignerConfigFromEnv_TrimsWhitespace(t *testing.T) {
	t.Setenv("ROSSHIELD_COSIGN_BINARY", "  /opt/cosign  ")
	t.Setenv("ROSSHIELD_COSIGN_IDENTITY", "  admin@example.com  ")

	cfg := rotation.LoadSignerConfigFromEnv()
	if cfg.BinaryPath != "/opt/cosign" {
		t.Errorf("BinaryPath = %q, want trimmed", cfg.BinaryPath)
	}
	if cfg.Identity != "admin@example.com" {
		t.Errorf("Identity = %q, want trimmed", cfg.Identity)
	}
}

// --- CosignSigner 비활성 경로 ---

func TestCosignSigner_DisabledReturnsNilNil(t *testing.T) {
	t.Parallel()

	s := rotation.NewCosignSigner(rotation.SignerConfig{Enabled: false})
	if s.Enabled() {
		t.Error("Enabled() = true, want false")
	}

	bundle, err := s.Sign(context.Background(), []byte("anything"))
	if err != nil {
		t.Errorf("Sign(disabled) err = %v, want nil", err)
	}
	if bundle != nil {
		t.Errorf("Sign(disabled) bundle = %v, want nil", bundle)
	}
}

func TestCosignSigner_NilReceiverEnabledFalse(t *testing.T) {
	t.Parallel()

	var s *rotation.CosignSigner
	if s.Enabled() {
		t.Error("nil receiver Enabled() = true, want false")
	}
}

func TestCosignSigner_IdentityAccessor(t *testing.T) {
	t.Parallel()

	s := rotation.NewCosignSigner(rotation.SignerConfig{Enabled: true, Identity: "ci@example.com"})
	if got := s.Identity(); got != "ci@example.com" {
		t.Errorf("Identity() = %q, want ci@example.com", got)
	}

	var nilS *rotation.CosignSigner
	if got := nilS.Identity(); got != "" {
		t.Errorf("nil Identity() = %q, want empty", got)
	}
}

func TestCosignSigner_EnabledMissingBinaryReturnsError(t *testing.T) {
	// 활성 signer + 존재하지 않는 binary → exec 실패 → error 반환.
	// PATH 오염 방지를 위해 절대 경로의 존재하지 않는 파일 지정.
	t.Parallel()

	missing := "/nonexistent/path/cosign-does-not-exist"
	if runtime.GOOS == "windows" {
		missing = `C:\nonexistent\cosign-does-not-exist.exe`
	}

	s := rotation.NewCosignSigner(rotation.SignerConfig{
		Enabled:    true,
		BinaryPath: missing,
	})
	if !s.Enabled() {
		t.Fatal("Enabled() = false, want true")
	}

	_, err := s.Sign(context.Background(), []byte("payload"))
	if err == nil {
		t.Fatal("expected error from missing cosign binary")
	}
	if !strings.Contains(err.Error(), "cosign sign-blob") {
		t.Errorf("error %q does not mention cosign sign-blob context", err.Error())
	}
}

func TestCosignSigner_EnabledEmptyArchiveError(t *testing.T) {
	t.Parallel()

	s := rotation.NewCosignSigner(rotation.SignerConfig{Enabled: true})
	_, err := s.Sign(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty archive")
	}
	if !strings.Contains(err.Error(), "empty archive") {
		t.Errorf("error %q does not mention 'empty archive'", err.Error())
	}
}

// --- FakeSigner ---

func TestFakeSigner_NilSignFuncDisabled(t *testing.T) {
	t.Parallel()

	s := rotation.NewFakeSigner(nil)
	if s.Enabled() {
		t.Error("Enabled() = true for nil signFunc, want false")
	}
	bundle, err := s.Sign(context.Background(), []byte("x"))
	if err != nil || bundle != nil {
		t.Errorf("Sign(nil func) = (%v, %v), want (nil, nil)", bundle, err)
	}
}

func TestFakeSigner_SignFuncInvoked(t *testing.T) {
	t.Parallel()

	want := []byte("fake-bundle-bytes")
	var gotInput []byte
	s := rotation.NewFakeSigner(func(archive []byte) ([]byte, error) {
		gotInput = append([]byte(nil), archive...)
		return want, nil
	})
	if !s.Enabled() {
		t.Fatal("Enabled() = false, want true")
	}

	bundle, err := s.Sign(context.Background(), []byte("payload"))
	if err != nil {
		t.Fatalf("Sign err = %v", err)
	}
	if !bytes.Equal(bundle, want) {
		t.Errorf("bundle = %x, want %x", bundle, want)
	}
	if string(gotInput) != "payload" {
		t.Errorf("signFunc input = %q, want payload", gotInput)
	}
}

func TestFakeSigner_PropagatesError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("fake sign failure")
	s := rotation.NewFakeSigner(func(_ []byte) ([]byte, error) {
		return nil, sentinel
	})
	_, err := s.Sign(context.Background(), []byte("x"))
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
}

// --- Rotator + Signer 결선 ---

// TestRotate_WithFakeSigner_FillsCosignBundle은 활성 FakeSigner가 cosign_bundle column을
// 채우는지 검증합니다 (D-AR-4 결선 contract).
func TestRotate_WithFakeSigner_FillsCosignBundle(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 3)

	be, _ := rotation.NewFileBackend(t.TempDir())

	wantBundle := []byte("FAKE-COSIGN-BUNDLE-V1")
	signer := rotation.NewFakeSigner(func(archive []byte) ([]byte, error) {
		if len(archive) == 0 {
			return nil, errors.New("got empty archive")
		}
		// archive bytes 도착 — bundle 리턴.
		return wantBundle, nil
	})

	rot, err := rotation.New(rotation.Deps{
		Clock:    clock.System(),
		Backend:  be,
		Appender: repo,
		Signer:   signer,
	})
	if err != nil {
		t.Fatalf("rotation.New: %v", err)
	}

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var rec *rotation.SegmentRecord
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := rot.Rotate(ctx, tx, testTenant, 1, 1, 3)
		rec = r
		return err
	}); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if !bytes.Equal(rec.CosignBundle, wantBundle) {
		t.Errorf("rec.CosignBundle = %q, want %q", rec.CosignBundle, wantBundle)
	}

	// DB round-trip — GetSegment에서도 같은 bundle을 읽는지.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		got, err := rotation.GetSegment(ctx, tx, testTenant, 1)
		if err != nil {
			return err
		}
		if !bytes.Equal(got.CosignBundle, wantBundle) {
			t.Errorf("GetSegment.CosignBundle = %q, want %q", got.CosignBundle, wantBundle)
		}
		return nil
	}); err != nil {
		t.Fatalf("GetSegment: %v", err)
	}
}

// TestRotate_WithDisabledSigner_BundleNil는 nil/비활성 signer일 때 cosign_bundle이 비어
// 저장되는지 검증합니다 (default behavior — 회귀 보호).
func TestRotate_WithDisabledSigner_BundleNil(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 2)

	be, _ := rotation.NewFileBackend(t.TempDir())

	// Signer 주입 안 함 — Deps.Signer = nil.
	rot, _ := rotation.New(rotation.Deps{
		Clock:    clock.System(),
		Backend:  be,
		Appender: repo,
	})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var rec *rotation.SegmentRecord
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := rot.Rotate(ctx, tx, testTenant, 1, 1, 2)
		rec = r
		return err
	}); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(rec.CosignBundle) != 0 {
		t.Errorf("rec.CosignBundle len = %d, want 0 (nil signer)", len(rec.CosignBundle))
	}

	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		got, err := rotation.GetSegment(ctx, tx, testTenant, 1)
		if err != nil {
			return err
		}
		if len(got.CosignBundle) != 0 {
			t.Errorf("GetSegment.CosignBundle len = %d, want 0", len(got.CosignBundle))
		}
		return nil
	}); err != nil {
		t.Fatalf("GetSegment: %v", err)
	}
}

// TestRotate_WithDisabledFakeSigner_BundleNil는 FakeSigner.Enabled=false (signFunc=nil)에서
// cosign_bundle이 비어있는지 — 활성 signer 객체지만 signFunc 미설정 경로.
func TestRotate_WithDisabledFakeSigner_BundleNil(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 2)

	be, _ := rotation.NewFileBackend(t.TempDir())

	// signFunc=nil → FakeSigner.Enabled()=false → Rotator가 Sign 자체 skip.
	signer := rotation.NewFakeSigner(nil)
	rot, _ := rotation.New(rotation.Deps{
		Clock:    clock.System(),
		Backend:  be,
		Appender: repo,
		Signer:   signer,
	})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var rec *rotation.SegmentRecord
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := rot.Rotate(ctx, tx, testTenant, 1, 1, 2)
		rec = r
		return err
	}); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(rec.CosignBundle) != 0 {
		t.Errorf("rec.CosignBundle len = %d, want 0 (disabled fake)", len(rec.CosignBundle))
	}
}

// TestRotate_SignerErrorAborts는 활성 signer가 에러 리턴 시 Rotate가 실패하고 segment row가
// 생성되지 않는지 (Tx rollback) 검증합니다.
func TestRotate_SignerErrorAborts(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 2)

	be, _ := rotation.NewFileBackend(t.TempDir())

	signErr := errors.New("simulated cosign failure")
	signer := rotation.NewFakeSigner(func(_ []byte) ([]byte, error) {
		return nil, signErr
	})
	rot, _ := rotation.New(rotation.Deps{
		Clock:    clock.System(),
		Backend:  be,
		Appender: repo,
		Signer:   signer,
	})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rot.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	})
	if err == nil {
		t.Fatal("expected Rotate error from signer failure")
	}
	if !errors.Is(err, signErr) {
		t.Errorf("err chain does not include signErr: %v", err)
	}

	// Tx rollback 확인 — segment 1 row 부재.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		latest, err := rotation.LatestSegmentNumber(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		if latest != 0 {
			t.Errorf("LatestSegmentNumber = %d, want 0 (rollback)", latest)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify rollback: %v", err)
	}
}
