package handlers_test

// ssh_fingerprint_test.go — POST /api/v1/utils/ssh-fingerprint 통합 테스트.
//
// 시나리오:
//   - 유효 ed25519 PEM → 200 + "SHA256:..." + keyType
//   - 잘못된 PEM → 400
//   - auth 없음 → 401

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"testing"

	"golang.org/x/crypto/ssh"
)

// generateEd25519PEM은 OpenSSH 형식 PKCS8 ed25519 private key PEM을 생성합니다.
func generateEd25519PEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	return pem.EncodeToMemory(block)
}

func TestSSHFingerprintReturnsSHA256(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	pem := generateEd25519PEM(t)
	body, _ := json.Marshal(map[string]any{"privateKeyPem": string(pem)})
	resp := f.doRequest(t, "POST", "/api/v1/utils/ssh-fingerprint", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var got struct {
		Fingerprint string `json:"fingerprint"`
		KeyType     string `json:"keyType"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.KeyType != "ssh-ed25519" {
		t.Errorf("KeyType = %q, want ssh-ed25519", got.KeyType)
	}
	if len(got.Fingerprint) < 8 || got.Fingerprint[:7] != "SHA256:" {
		t.Errorf("Fingerprint = %q, want SHA256: prefix", got.Fingerprint)
	}
}

func TestSSHFingerprintRejectsInvalidPEM(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{"privateKeyPem": "not a pem"})
	resp := f.doRequest(t, "POST", "/api/v1/utils/ssh-fingerprint", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 400", resp.StatusCode, string(raw))
	}
}

func TestSSHFingerprintRequires401(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()
	body, _ := json.Marshal(map[string]any{"privateKeyPem": "-----BEGIN-----"})
	resp := f.doRequest(t, "POST", "/api/v1/utils/ssh-fingerprint", "", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}
