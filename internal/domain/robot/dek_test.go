package robot_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func newKEK(t *testing.T) *robot.KEK {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credential.kek")
	kek, err := robot.LoadOrCreateKEK(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKEK: %v", err)
	}
	return kek
}

func samplePasswordMaterial() robot.CredentialMaterial {
	return robot.CredentialMaterial{
		Type:     robot.CredentialTypePassword,
		Username: "rosshield-runner",
		Password: "very-secret-password-do-not-leak",
	}
}

func samplePrivateKeyMaterial() robot.CredentialMaterial {
	return robot.CredentialMaterial{
		Type:                 robot.CredentialTypePrivateKey,
		Username:             "rosshield-runner",
		PrivateKeyPEM:        "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----\n",
		PrivateKeyPassphrase: "passphrase-for-key",
	}
}

func TestWrapUnwrapRoundtripPassword(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	mat := samplePasswordMaterial()

	cipher, meta, err := robot.WrapMaterial(kek, "tn_TEST", "cr_001", mat, time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}
	if len(cipher) == 0 {
		t.Error("cipher is empty")
	}
	if meta.KEKKeyID != kek.KeyID() {
		t.Errorf("meta KEKKeyID = %q, want %q", meta.KEKKeyID, kek.KeyID())
	}
	if meta.Version != robot.EncryptionVersion {
		t.Errorf("meta Version = %d, want %d", meta.Version, robot.EncryptionVersion)
	}
	if meta.Algorithm != robot.EncryptionAlgorithm {
		t.Errorf("meta Algorithm = %q, want %q", meta.Algorithm, robot.EncryptionAlgorithm)
	}

	got, err := robot.UnwrapMaterial(kek, cipher, meta)
	if err != nil {
		t.Fatalf("UnwrapMaterial: %v", err)
	}
	if got != mat {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", got, mat)
	}
}

func TestWrapUnwrapRoundtripPrivateKey(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	mat := samplePrivateKeyMaterial()

	cipher, meta, err := robot.WrapMaterial(kek, "tn_TEST", "cr_002", mat, time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}
	got, err := robot.UnwrapMaterial(kek, cipher, meta)
	if err != nil {
		t.Fatalf("UnwrapMaterial: %v", err)
	}
	if got != mat {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", got, mat)
	}
}

func TestWrapDoesNotLeakPlaintextInCipher(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	mat := samplePasswordMaterial()

	cipher, meta, err := robot.WrapMaterial(kek, "tn_TEST", "cr_003", mat, time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}
	// 평문 marker가 ciphertext나 wrappedDEK에 등장하면 누출.
	for _, marker := range []string{
		mat.Username,
		mat.Password,
	} {
		if bytes.Contains(cipher, []byte(marker)) {
			t.Errorf("ciphertext leaks plaintext marker %q", marker)
		}
		if bytes.Contains(meta.WrappedDEK, []byte(marker)) {
			t.Errorf("WrappedDEK leaks plaintext marker %q", marker)
		}
	}
}

func TestUnwrapRejectsTamperedCiphertext(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	cipher, meta, err := robot.WrapMaterial(kek, "tn_TEST", "cr_004", samplePasswordMaterial(), time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}
	cipher[0] ^= 0xFF // flip first byte — GCM tag should detect.

	_, err = robot.UnwrapMaterial(kek, cipher, meta)
	if !errors.Is(err, robot.ErrCredentialDecrypt) {
		t.Errorf("err = %v, want ErrCredentialDecrypt", err)
	}
}

func TestUnwrapRejectsTamperedWrappedDEK(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	cipher, meta, err := robot.WrapMaterial(kek, "tn_TEST", "cr_005", samplePasswordMaterial(), time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}
	meta.WrappedDEK[0] ^= 0xFF

	_, err = robot.UnwrapMaterial(kek, cipher, meta)
	if !errors.Is(err, robot.ErrCredentialDecrypt) {
		t.Errorf("err = %v, want ErrCredentialDecrypt", err)
	}
}

func TestUnwrapRejectsTamperedAAD(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	cipher, meta, err := robot.WrapMaterial(kek, "tn_TEST", "cr_006", samplePasswordMaterial(), time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}
	// AAD를 다른 tenant·credential ID로 위조.
	meta.AAD = "t=tn_OTHER;c=cr_006;v=1"

	_, err = robot.UnwrapMaterial(kek, cipher, meta)
	if !errors.Is(err, robot.ErrCredentialDecrypt) {
		t.Errorf("err = %v, want ErrCredentialDecrypt", err)
	}
}

func TestUnwrapRejectsDifferentKEK(t *testing.T) {
	t.Parallel()
	kek1 := newKEK(t)
	cipher, meta, err := robot.WrapMaterial(kek1, "tn_TEST", "cr_007", samplePasswordMaterial(), time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}

	kek2 := newKEK(t)
	if kek1.KeyID() == kek2.KeyID() {
		t.Fatal("two random KEKs collide — entropy bug")
	}

	_, err = robot.UnwrapMaterial(kek2, cipher, meta)
	if !errors.Is(err, robot.ErrCredentialDecrypt) {
		t.Errorf("err = %v, want ErrCredentialDecrypt for KEK mismatch", err)
	}
}

func TestUnwrapRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	cipher, meta, err := robot.WrapMaterial(kek, "tn_TEST", "cr_008", samplePasswordMaterial(), time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}
	meta.Version = 999

	_, err = robot.UnwrapMaterial(kek, cipher, meta)
	if !errors.Is(err, robot.ErrCredentialMetaVersion) {
		t.Errorf("err = %v, want ErrCredentialMetaVersion", err)
	}
}

func TestWrapDifferentCallsProduceDifferentCiphertext(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	mat := samplePasswordMaterial()

	c1, m1, err := robot.WrapMaterial(kek, "tn_TEST", "cr_009", mat, time.Now())
	if err != nil {
		t.Fatalf("first wrap: %v", err)
	}
	c2, m2, err := robot.WrapMaterial(kek, "tn_TEST", "cr_009", mat, time.Now())
	if err != nil {
		t.Fatalf("second wrap: %v", err)
	}
	if bytes.Equal(c1, c2) {
		t.Error("two wraps produced identical ciphertext — nonce reuse")
	}
	if bytes.Equal(m1.PayloadNonce, m2.PayloadNonce) {
		t.Error("two wraps produced identical PayloadNonce")
	}
	if bytes.Equal(m1.WrappedDEK, m2.WrappedDEK) {
		t.Error("two wraps produced identical WrappedDEK")
	}
}

func TestWrapRejectsEmptyTenant(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	_, _, err := robot.WrapMaterial(kek, "", "cr_010", samplePasswordMaterial(), time.Now())
	if !errors.Is(err, storage.ErrTenantMissing) {
		t.Errorf("err = %v, want ErrTenantMissing", err)
	}
}

func TestWrapRejectsEmptyCredentialID(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	_, _, err := robot.WrapMaterial(kek, "tn_TEST", "", samplePasswordMaterial(), time.Now())
	if err == nil {
		t.Error("err = nil, want non-nil for empty credentialID")
	}
}

func TestWrapRejectsInvalidMaterialType(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	mat := robot.CredentialMaterial{
		Type:     "ssh-agent", // 미지원
		Username: "u",
	}
	_, _, err := robot.WrapMaterial(kek, "tn_TEST", "cr_011", mat, time.Now())
	if !errors.Is(err, robot.ErrCredentialUnknownType) {
		t.Errorf("err = %v, want ErrCredentialUnknownType", err)
	}
}

func TestWrapRejectsEmptyUsername(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	mat := robot.CredentialMaterial{
		Type:     robot.CredentialTypePassword,
		Username: "   ",
		Password: "x",
	}
	_, _, err := robot.WrapMaterial(kek, "tn_TEST", "cr_012", mat, time.Now())
	if !errors.Is(err, robot.ErrCredentialEmptyUser) {
		t.Errorf("err = %v, want ErrCredentialEmptyUser", err)
	}
}

func TestEncryptionMetaAADFormat(t *testing.T) {
	t.Parallel()
	kek := newKEK(t)
	_, meta, err := robot.WrapMaterial(kek, "tn_ABC", "cr_XYZ", samplePasswordMaterial(), time.Now())
	if err != nil {
		t.Fatalf("WrapMaterial: %v", err)
	}
	want := "t=tn_ABC;c=cr_XYZ;v=1"
	if meta.AAD != want {
		t.Errorf("AAD = %q, want %q", meta.AAD, want)
	}
	if !strings.Contains(meta.AAD, "tn_ABC") {
		t.Error("AAD must contain tenant ID for cross-credential isolation")
	}
}
