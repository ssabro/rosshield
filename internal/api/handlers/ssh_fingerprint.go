package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"golang.org/x/crypto/ssh"
)

// sshFingerprintRequest는 POST /api/v1/utils/ssh-fingerprint 본문입니다.
//
// 평문 PEM — 메모리에서만 처리, 응답 후 GC. 영속화 없음.
type sshFingerprintRequest struct {
	PrivateKeyPem string `json:"privateKeyPem"`
	Passphrase    string `json:"passphrase,omitempty"`
}

// sshFingerprintResponse는 SHA256 표준 SSH fingerprint와 키 종류를 반환합니다.
type sshFingerprintResponse struct {
	Fingerprint string `json:"fingerprint"` // "SHA256:<base64-no-pad>"
	KeyType     string `json:"keyType"`     // "ssh-rsa" | "ssh-ed25519" | "ecdsa-sha2-nistp256" 등
}

// SSHFingerprint는 PEM private key를 받아 표준 OpenSSH SHA256 fingerprint를 반환합니다.
//
// admin 전용. credential rotate UI의 PEM textarea preview 용도. 본 endpoint는
// PEM을 영속하지 않고 즉시 fingerprint만 계산해 반환합니다 — 평문은 응답 직후
// GC. PEM 형식 무효 또는 passphrase 누락 시 400.
func (h *Handlers) SSHFingerprint(w http.ResponseWriter, r *http.Request) {
	var req sshFingerprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PrivateKeyPem == "" {
		writeError(w, http.StatusBadRequest, "missing privateKeyPem")
		return
	}

	pemBytes := []byte(req.PrivateKeyPem)
	var signer ssh.Signer
	var err error
	if req.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(req.Passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(pemBytes)
	}
	if err != nil {
		// 암호화된 키이지만 passphrase 없는 경우 명확한 메시지로.
		if errors.Is(err, &ssh.PassphraseMissingError{}) {
			writeError(w, http.StatusBadRequest, "private key is encrypted — passphrase required")
			return
		}
		// PassphraseMissingError는 errors.Is로 잘 안 잡힐 수 있음 — string match fallback.
		var pmErr *ssh.PassphraseMissingError
		if errors.As(err, &pmErr) {
			writeError(w, http.StatusBadRequest, "private key is encrypted — passphrase required")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to parse private key")
		return
	}

	pub := signer.PublicKey()
	writeJSON(w, http.StatusOK, sshFingerprintResponse{
		Fingerprint: ssh.FingerprintSHA256(pub),
		KeyType:     pub.Type(),
	})
}
