//go:build linux

// store_linux.go — Linux 전용 TPM 2.0 PCR-sealed Store 구현 (E34 Stage 2-B).
//
// 본 파일은 cgo·linux 환경에서 google/go-tpm-tools/client + go-tpm/legacy/tpm2를
// 사용하여 ed25519 raw private key를 PCR policy 정책에 묶어 sealed blob으로
// 보관합니다. simulator 의존부는 별 파일(`store_linux_test.go`)에서만 import.
package tpm

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/go-tpm-tools/client"
	pb "github.com/google/go-tpm-tools/proto/tpm"
	"github.com/google/go-tpm/legacy/tpm2"
	"google.golang.org/protobuf/proto"
)

// Store는 TPM 2.0 PCR-sealed KeyStore (Linux 구현)입니다.
//
// 동시성: TPM 디바이스는 한 번에 하나의 명령만 처리하므로 본 Store는 mutex로
// LoadOrCreatePrivateKey·Close를 직렬화합니다 (HA 환경에서 두 인스턴스가 같은
// /dev/tpm*을 동시 사용할 일은 없으나, 같은 process 안 다중 호출은 안전해야 함).
type Store struct {
	rwc          io.ReadWriteCloser
	pcrSelection []int
	sealingDir   string
	openTPM      tpmOpener // test seam — production 코드는 항상 openLinuxTPM
	mu           sync.Mutex
	closed       bool
}

// tpmOpener는 store_linux_test.go가 simulator를 주입하기 위한 hook입니다.
// nil이면 production 경로(/dev/tpmrm0 → /dev/tpm0).
type tpmOpener func() (io.ReadWriteCloser, error)

// New는 TPM Store를 생성합니다 (Linux).
//
// SealingDir 비어있으면 ErrSealingDirRequired.
// PCRSelection 비어있으면 [0, 2, 4, 7] 기본값.
// DevicePath:
//   - "" → tpm2.OpenTPM() (기본 /dev/tpmrm0 fallback /dev/tpm0)
//   - "<path>" → tpm2.OpenTPM(path)
//
// TPM 디바이스 부재·권한 부족·TPM 2.0 미장착이면 ErrTpmDeviceNotAvailable.
func New(opts Options) (*Store, error) {
	if opts.SealingDir == "" {
		return nil, ErrSealingDirRequired
	}
	if err := os.MkdirAll(opts.SealingDir, 0o700); err != nil {
		return nil, fmt.Errorf("keystore/tpm: mkdir SealingDir: %w", err)
	}
	pcr := resolvePCRSelection(opts)

	devicePath := opts.DevicePath
	rwc, err := openLinuxTPM(devicePath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTpmDeviceNotAvailable, err)
	}
	return &Store{
		rwc:          rwc,
		pcrSelection: pcr,
		sealingDir:   opts.SealingDir,
	}, nil
}

// newWithOpener는 test 전용 — simulator 같은 in-process TPM을 주입합니다.
// store_linux_test.go가 호출.
func newWithOpener(opts Options, opener tpmOpener) (*Store, error) {
	if opts.SealingDir == "" {
		return nil, ErrSealingDirRequired
	}
	if opener == nil {
		return nil, errors.New("keystore/tpm: nil opener (test seam)")
	}
	if err := os.MkdirAll(opts.SealingDir, 0o700); err != nil {
		return nil, fmt.Errorf("keystore/tpm: mkdir SealingDir: %w", err)
	}
	pcr := resolvePCRSelection(opts)
	rwc, err := opener()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTpmDeviceNotAvailable, err)
	}
	return &Store{
		rwc:          rwc,
		pcrSelection: pcr,
		sealingDir:   opts.SealingDir,
		openTPM:      opener,
	}, nil
}

// openLinuxTPM은 production 경로 — go-tpm legacy/tpm2.OpenTPM 위임.
func openLinuxTPM(devicePath string) (io.ReadWriteCloser, error) {
	if devicePath == "" {
		return tpm2.OpenTPM()
	}
	return tpm2.OpenTPM(devicePath)
}

// LoadOrCreatePrivateKey는 handle에 해당하는 sealed blob에서 ed25519 private key를
// 로드 또는 신규 생성·sealing합니다.
//
// handle: sealed blob filename prefix (예: "platform" → SealingDir/platform.sealed).
// 경로 구분자(`/`, `\`)·`..` 포함 시 거부 (디렉터리 traversal 방어).
//
// blob 존재 시:
//  1. blob 파일 read → proto SealedBytes Unmarshal
//  2. tpm2 client.Unseal (PCR policy 자동 검증) → ed25519 raw 64B
//  3. PCR 변조 시 ErrPcrMismatch 래핑
//
// blob 부재 시:
//  1. ed25519.GenerateKey
//  2. SRK ECC handle 생성
//  3. client.Key.Seal(raw 64B, PCRSelection)
//  4. proto Marshal → 0600으로 디스크 저장
func (s *Store) LoadOrCreatePrivateKey(handle string) (ed25519.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("keystore/tpm: Store closed")
	}
	if err := validateHandle(handle); err != nil {
		return nil, err
	}
	blobPath := filepath.Join(s.sealingDir, handle+".sealed")

	// 시도 1: blob 존재 시 unseal.
	if _, err := os.Stat(blobPath); err == nil {
		key, err := s.unsealKey(blobPath)
		if err != nil {
			return nil, err
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("keystore/tpm: stat sealed blob: %w", err)
	}

	// 시도 2: 신규 생성·seal.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("keystore/tpm: generate ed25519: %w", err)
	}
	if err := s.sealKey(blobPath, priv); err != nil {
		return nil, err
	}
	return priv, nil
}

// validateHandle은 path traversal 방어 — handle은 단순 식별자만 허용.
func validateHandle(handle string) error {
	if handle == "" {
		return errors.New("keystore/tpm: handle is empty")
	}
	if strings.ContainsAny(handle, `/\`) || strings.Contains(handle, "..") {
		return fmt.Errorf("keystore/tpm: invalid handle %q (path separators or '..' not allowed)", handle)
	}
	return nil
}

// sealKey는 ed25519 private key를 PCR-sealed blob으로 디스크에 저장합니다.
func (s *Store) sealKey(blobPath string, priv ed25519.PrivateKey) error {
	srk, err := client.StorageRootKeyECC(s.rwc)
	if err != nil {
		return fmt.Errorf("keystore/tpm: load SRK ECC: %w", err)
	}
	defer srk.Close()

	sel := tpm2.PCRSelection{Hash: tpm2.AlgSHA256, PCRs: s.pcrSelection}
	sealed, err := srk.Seal([]byte(priv), client.SealOpts{Current: sel})
	if err != nil {
		return fmt.Errorf("keystore/tpm: seal: %w", err)
	}
	blob, err := proto.Marshal(sealed)
	if err != nil {
		return fmt.Errorf("keystore/tpm: marshal sealed blob: %w", err)
	}
	if err := writeBlobFile(blobPath, blob); err != nil {
		return fmt.Errorf("keystore/tpm: write sealed blob: %w", err)
	}
	return nil
}

// unsealKey는 디스크의 sealed blob을 unseal하여 ed25519 private key를 반환합니다.
// PCR 변조 시 ErrPcrMismatch.
func (s *Store) unsealKey(blobPath string) (ed25519.PrivateKey, error) {
	blob, err := os.ReadFile(blobPath) // #nosec G304 — blobPath는 SealingDir 하위, validateHandle로 traversal 방어
	if err != nil {
		return nil, fmt.Errorf("keystore/tpm: read sealed blob: %w", err)
	}
	var sealed pb.SealedBytes
	if err := proto.Unmarshal(blob, &sealed); err != nil {
		return nil, fmt.Errorf("keystore/tpm: unmarshal sealed blob: %w", err)
	}
	srk, err := client.StorageRootKeyECC(s.rwc)
	if err != nil {
		return nil, fmt.Errorf("keystore/tpm: load SRK ECC: %w", err)
	}
	defer srk.Close()

	sel := tpm2.PCRSelection{Hash: tpm2.AlgSHA256, PCRs: s.pcrSelection}
	raw, err := srk.Unseal(&sealed, client.UnsealOpts{CertifyCurrent: sel})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPcrMismatch, err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("keystore/tpm: unsealed key size = %d, want %d", len(raw), ed25519.PrivateKeySize)
	}
	priv := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	copy(priv, raw)
	return priv, nil
}

// writeBlobFile은 blob을 0600 권한으로 atomic 저장합니다 (rename via temp file).
func writeBlobFile(blobPath string, data []byte) error {
	tmp := blobPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, blobPath)
}

// Close는 TPM 디바이스를 해제합니다 (idempotent).
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.rwc == nil {
		return nil
	}
	return s.rwc.Close()
}
