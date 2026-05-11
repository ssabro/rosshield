//go:build linux && tpm_integration

// store_linux_test.go — go-tpm-tools/simulator를 사용한 통합 테스트 (E34 Stage 2-B).
//
// 빌드 태그 `tpm_integration`은 일반 `go test ./...`에서 본 파일이 빌드되지 않게
// 합니다 (simulator는 cgo + ms-tpm-20-ref C 라이브러리 의존). CI의 별 job
// `tpm-integration`에서만 실행:
//
//	go test -tags=tpm_integration -count=1 ./internal/platform/keystore/tpm/...
//
// simulator.Get()은 in-process Microsoft TPM2 reference. sync.Mutex로 단일
// 인스턴스만 살아있게 보장 — 본 파일 내 테스트들은 t.Run sub-test 또는 순차
// 실행이 필요하며, t.Parallel은 사용하지 않습니다.
package tpm

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-tpm-tools/simulator"
	"github.com/google/go-tpm/legacy/tpm2"
	"github.com/google/go-tpm/tpmutil"
)

// debugPCR는 simulator에서 자유롭게 PCR_Extend로 변조 가능한 PCR입니다.
// PCR 16은 TPM 2.0 spec상 debug용으로 OS extend가 허용됩니다.
const debugPCR = 16

// pcrSelectionForTest — debug PCR(16)을 sealing 정책에 포함하여 변조 후 unseal
// 실패를 검증할 수 있게 합니다. 실 production은 [0,2,4,7] (defaultPCRSelection).
func pcrSelectionForTest() []int { return []int{0, debugPCR} }

// newSimulatorStore — simulator를 주입한 Store 생성 헬퍼.
// 주의: simulator.Get()은 global lock — 한 번에 하나의 simulator만 살아있어야 함.
// 호출자가 t.Cleanup으로 Close 보장.
func newSimulatorStore(t *testing.T, sealingDir string) (*Store, *simulator.Simulator) {
	t.Helper()
	sim, err := simulator.Get()
	if err != nil {
		t.Fatalf("simulator.Get: %v", err)
	}
	opener := func() (io.ReadWriteCloser, error) { return sim, nil }
	store, err := newWithOpener(Options{
		SealingDir:   sealingDir,
		PCRSelection: pcrSelectionForTest(),
	}, opener)
	if err != nil {
		_ = sim.Close()
		t.Fatalf("newWithOpener: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store, sim
}

// TestSealUnsealRoundTrip — 첫 LoadOrCreate에서 신규 키 생성·seal, 두 번째
// 호출에서 같은 키를 unseal 반환.
func TestSealUnsealRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sealDir := filepath.Join(dir, "keys", "tpm")

	store, _ := newSimulatorStore(t, sealDir)

	priv1, err := store.LoadOrCreatePrivateKey("platform")
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	if len(priv1) != ed25519.PrivateKeySize {
		t.Fatalf("priv1 size = %d, want %d", len(priv1), ed25519.PrivateKeySize)
	}

	// 두 번째 호출 — 같은 simulator·blob → 같은 키 unseal.
	priv2, err := store.LoadOrCreatePrivateKey("platform")
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	if !bytes.Equal(priv1, priv2) {
		t.Errorf("priv2 differs from priv1 (unseal not deterministic)")
	}

	// blob 파일이 0600 권한으로 존재.
	blobPath := filepath.Join(sealDir, "platform.sealed")
	info, err := os.Stat(blobPath)
	if err != nil {
		t.Fatalf("stat blob: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("blob mode = %o, want 0600", mode)
	}
}

// TestLoadOrCreateMultipleHandles — 같은 Store에서 다른 handle은 다른 키.
func TestLoadOrCreateMultipleHandles(t *testing.T) {
	dir := t.TempDir()
	sealDir := filepath.Join(dir, "keys", "tpm")

	store, _ := newSimulatorStore(t, sealDir)

	platformKey, err := store.LoadOrCreatePrivateKey("platform")
	if err != nil {
		t.Fatalf("platform LoadOrCreate: %v", err)
	}
	jwtKey, err := store.LoadOrCreatePrivateKey("jwt")
	if err != nil {
		t.Fatalf("jwt LoadOrCreate: %v", err)
	}
	if bytes.Equal(platformKey, jwtKey) {
		t.Errorf("platform == jwt (different handles must yield different keys)")
	}

	// 재로드 — handle별로 정확히 같은 키.
	platformKey2, err := store.LoadOrCreatePrivateKey("platform")
	if err != nil {
		t.Fatalf("platform re-LoadOrCreate: %v", err)
	}
	if !bytes.Equal(platformKey, platformKey2) {
		t.Errorf("platform key not stable across reloads")
	}
}

// TestSealUnsealFailsWhenPcrChanged — seal 후 PCR_Extend로 정책 PCR을 변조하면
// unseal이 ErrPcrMismatch (또는 wrap된 에러).
func TestSealUnsealFailsWhenPcrChanged(t *testing.T) {
	dir := t.TempDir()
	sealDir := filepath.Join(dir, "keys", "tpm")

	store, sim := newSimulatorStore(t, sealDir)

	if _, err := store.LoadOrCreatePrivateKey("platform"); err != nil {
		t.Fatalf("seal: %v", err)
	}

	// 정책에 포함된 PCR을 변조 (debug PCR 16).
	extension := bytes.Repeat([]byte{0xAA}, sha256.Size)
	if err := tpm2.PCRExtend(sim, tpmutil.Handle(debugPCR), tpm2.AlgSHA256, extension, ""); err != nil {
		t.Fatalf("PCRExtend: %v", err)
	}

	// 두 번째 LoadOrCreate는 unseal을 시도하므로 실패해야 함.
	_, err := store.LoadOrCreatePrivateKey("platform")
	if err == nil {
		t.Fatal("expected unseal failure after PCR_Extend, got nil")
	}
	if !errors.Is(err, ErrPcrMismatch) {
		t.Errorf("err = %v, want wrap of ErrPcrMismatch", err)
	}
}

// TestNewWithoutSealingDirReturnsError — newWithOpener도 SealingDir 비면 거부.
func TestNewWithoutSealingDirReturnsError(t *testing.T) {
	opener := func() (io.ReadWriteCloser, error) { return nil, nil }
	_, err := newWithOpener(Options{SealingDir: ""}, opener)
	if !errors.Is(err, ErrSealingDirRequired) {
		t.Errorf("err = %v, want ErrSealingDirRequired", err)
	}
}

// TestInvalidHandleRejected — path traversal 방어.
func TestInvalidHandleRejected(t *testing.T) {
	dir := t.TempDir()
	sealDir := filepath.Join(dir, "keys", "tpm")
	store, _ := newSimulatorStore(t, sealDir)

	cases := []string{"", "../../etc/passwd", "subdir/key", "back\\slash"}
	for _, h := range cases {
		_, err := store.LoadOrCreatePrivateKey(h)
		if err == nil {
			t.Errorf("LoadOrCreatePrivateKey(%q) = nil, want error", h)
		}
	}
}
