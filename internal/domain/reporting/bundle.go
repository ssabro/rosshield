package reporting

// bundle.go — 외부 검증 가능한 tar.gz 번들 + 검증 (R10-2·R10-4, E8 Stage C).
//
// 결과 파일 구조:
//
//	report.pdf                 — Build 결과 본문
//	report.pdf.sig             — Ed25519 detached signature (raw 64B, minisign 호환 단순화)
//	audit-chain-head.json      — anchor JSON (R10-3)
//	public-key.pem             — ed25519 PublicKey (PKIX SubjectPublicKeyInfo)
//
// 외부 검증자는 GPG/SSH 패턴처럼 ed25519.Verify(pub, pdfBytes, sig)로 단독 검증 가능.
// audit-chain-head.json은 audit DB와 cross-check 옵션(`rosshield-server report verify --audit-export`).
//
// 도메인 결합 규칙(P5): stdlib + crypto/ed25519만 사용. 외부 의존 0.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"time"
)

// 번들 entry 파일 이름 — 변경 시 외부 검증 도구 호환 깨짐.
const (
	BundleFilePDF       = "report.pdf"
	BundleFileSignature = "report.pdf.sig"
	BundleFileAnchor    = "audit-chain-head.json"
	BundleFilePublicKey = "public-key.pem"
)

// AnchorPayload는 audit-chain-head.json의 와이어 형식 (R10-3 anchor).
//
// JSON canonical: 키 알파벳순, 공백 0 — 외부 도구가 byte-identical 직렬화 가능해야 함.
type AnchorPayload struct {
	ChainHeadHash string `json:"chainHeadHash"`
	ChainHeadSeq  int64  `json:"chainHeadSeq"`
	SignedAt      string `json:"signedAt"` // RFC3339Nano UTC
	SignerKeyID   string `json:"signerKeyId"`
	TenantID      string `json:"tenantId"`
}

// VerifyResult는 VerifyBundle의 출력입니다.
type VerifyResult struct {
	OK            bool   // 모든 검증 통과
	Reason        string // OK=false일 때 사람 읽기용 설명
	PDFSize       int64  // 추출된 PDF 크기
	PDFSHA256     string // 추출된 PDF의 sha256(hex)
	SignerKeyID   string // 추출된 signer keyId
	ChainHeadSeq  int64  // anchor의 seq
	ChainHeadHash string // anchor의 hash(hex)
}

// 에러 sentinel.
var (
	ErrBundleMissingPDF       = errors.New("reporting: bundle missing report.pdf")
	ErrBundleMissingSignature = errors.New("reporting: bundle missing report.pdf.sig")
	ErrBundleMissingAnchor    = errors.New("reporting: bundle missing audit-chain-head.json")
	ErrBundleMissingPubKey    = errors.New("reporting: bundle missing public-key.pem")
	ErrBundleSignatureSize    = errors.New("reporting: signature must be 64 bytes (Ed25519)")
	ErrBundleSignatureInvalid = errors.New("reporting: signature verify failed")
	ErrBundleAnchorMalformed  = errors.New("reporting: audit-chain-head.json malformed")
	ErrBundlePubKeyMalformed  = errors.New("reporting: public-key.pem malformed")
	ErrBundlePubKeyMismatch   = errors.New("reporting: bundle public key does not match expected")
)

// BuildBundle은 서명된 Report를 외부 검증 가능한 tar.gz 번들로 묶습니다.
//
// 입력 Report는 반드시 Sign 단계를 거쳐 Signature.Signature가 비-zero여야 합니다.
// publicKey는 raw 32B Ed25519 PublicKey — bootstrap이 signer.PublicKey()를 직접 전달.
//
// tar entry 순서·timestamp·gzip ModTime을 모두 결정적으로 고정 — 같은 입력 ⇒ byte-identical 번들.
func BuildBundle(report Report, publicKey ed25519.PublicKey) ([]byte, error) {
	if len(report.PDF) == 0 {
		return nil, fmt.Errorf("reporting: report.PDF is empty")
	}
	if report.Signature.IsZero() {
		return nil, fmt.Errorf("reporting: report not signed (call Sign first)")
	}
	if len(report.Signature.Signature) != ed25519SignatureSize {
		return nil, ErrBundleSignatureSize
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("reporting: publicKey size = %d, want %d",
			len(publicKey), ed25519.PublicKeySize)
	}

	anchor := AnchorPayload{
		TenantID:      string(report.TenantID),
		ChainHeadSeq:  report.Signature.ChainHeadSeq,
		ChainHeadHash: report.Signature.ChainHeadHash,
		SignedAt:      report.Signature.SignedAt.UTC().Format(time.RFC3339Nano),
		SignerKeyID:   report.Signature.SignerKeyID,
	}
	anchorBytes, err := json.Marshal(anchor)
	if err != nil {
		return nil, fmt.Errorf("reporting: marshal anchor: %w", err)
	}

	pemBytes, err := encodePublicKeyPEM(publicKey)
	if err != nil {
		return nil, err
	}

	// tar entry 순서: PDF → SIGNATURE → anchor → public-key.
	entries := []struct {
		name string
		body []byte
	}{
		{BundleFilePDF, report.PDF},
		{BundleFileSignature, report.Signature.Signature},
		{BundleFileAnchor, anchorBytes},
		{BundleFilePublicKey, pemBytes},
	}

	var gzBuf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("reporting: gzip writer: %w", err)
	}
	gz.ModTime = time.Time{}
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:    e.name,
			Mode:    0o644,
			Size:    int64(len(e.body)),
			ModTime: time.Time{},
			Format:  tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("reporting: tar header %q: %w", e.name, err)
		}
		if _, err := tw.Write(e.body); err != nil {
			return nil, fmt.Errorf("reporting: tar write %q: %w", e.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("reporting: tar close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("reporting: gzip close: %w", err)
	}
	return gzBuf.Bytes(), nil
}

// VerifyBundle은 tar.gz 번들을 풀어 모든 검증을 수행합니다.
//
// 검증 순서:
//  1. tar.gz 풀어 4 entry 모두 존재 확인
//  2. public-key.pem → PKIX 디코드 → ed25519.PublicKey
//  3. ed25519.Verify(publicKey, pdfBytes, sig) — 본 번들의 핵심 진위 검증
//  4. anchor JSON 파싱 — seq·hash·signerKeyId·signedAt 추출
//  5. anchor.SignerKeyID 검증은 외부 책임 (다른 안전 채널로 받은 expectedKeyID와 비교)
//
// expectedPublicKey가 nil이면 번들 안 public-key.pem을 신뢰. nil 아니면 mismatch는 ErrBundlePubKeyMismatch.
//
// audit DB와의 cross-check(seq·hash 일치 여부)는 본 함수 책임 X — caller가 audit.Service로 별도 수행.
func VerifyBundle(tarGz []byte, expectedPublicKey ed25519.PublicKey) (VerifyResult, error) {
	files, err := extractBundleTarGz(tarGz)
	if err != nil {
		return VerifyResult{}, err
	}
	pdfBytes, ok := files[BundleFilePDF]
	if !ok {
		return VerifyResult{}, ErrBundleMissingPDF
	}
	sigBytes, ok := files[BundleFileSignature]
	if !ok {
		return VerifyResult{}, ErrBundleMissingSignature
	}
	anchorBytes, ok := files[BundleFileAnchor]
	if !ok {
		return VerifyResult{}, ErrBundleMissingAnchor
	}
	pemBytes, ok := files[BundleFilePublicKey]
	if !ok {
		return VerifyResult{}, ErrBundleMissingPubKey
	}

	if len(sigBytes) != ed25519.SignatureSize {
		return VerifyResult{
			OK: false, Reason: fmt.Sprintf("signature size=%d", len(sigBytes)),
		}, ErrBundleSignatureSize
	}

	bundlePub, err := decodePublicKeyPEM(pemBytes)
	if err != nil {
		return VerifyResult{OK: false, Reason: err.Error()}, ErrBundlePubKeyMalformed
	}
	if expectedPublicKey != nil && !bytes.Equal(bundlePub, expectedPublicKey) {
		return VerifyResult{OK: false, Reason: "public key mismatch"}, ErrBundlePubKeyMismatch
	}

	if !ed25519.Verify(bundlePub, pdfBytes, sigBytes) {
		return VerifyResult{OK: false, Reason: "ed25519 verify failed"}, ErrBundleSignatureInvalid
	}

	var anchor AnchorPayload
	if err := json.Unmarshal(anchorBytes, &anchor); err != nil {
		return VerifyResult{OK: false, Reason: err.Error()}, ErrBundleAnchorMalformed
	}

	sum := sha256.Sum256(pdfBytes)
	return VerifyResult{
		OK:            true,
		PDFSize:       int64(len(pdfBytes)),
		PDFSHA256:     hex.EncodeToString(sum[:]),
		SignerKeyID:   anchor.SignerKeyID,
		ChainHeadSeq:  anchor.ChainHeadSeq,
		ChainHeadHash: anchor.ChainHeadHash,
	}, nil
}

// extractBundleTarGz는 tar.gz 바이트를 path → bytes로 풉니다 (memory).
//
// 본 함수는 reporting 도메인 한정 — benchmark/converter의 tar.gz extract와 별개로
// 도메인 격리 유지(P5). 한도(파일당 16MiB·총 256MiB)는 의도적으로 동일한 보안 limit 채택.
func extractBundleTarGz(data []byte) (map[string][]byte, error) {
	const maxFileSize = 16 * 1024 * 1024
	const maxTotal = 256 * 1024 * 1024

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("reporting: gzip open: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reporting: tar next: %w", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		if hdr.Size > maxFileSize {
			return nil, fmt.Errorf("reporting: bundle entry %q exceeds %d bytes", hdr.Name, maxFileSize)
		}
		if total+hdr.Size > maxTotal {
			return nil, fmt.Errorf("reporting: bundle exceeds %d bytes total", maxTotal)
		}
		body := make([]byte, 0, hdr.Size)
		buf := bytes.NewBuffer(body)
		n, err := io.CopyN(buf, tr, maxFileSize+1)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("reporting: read entry %q: %w", hdr.Name, err)
		}
		if n > maxFileSize {
			return nil, fmt.Errorf("reporting: entry %q exceeded during read", hdr.Name)
		}
		out[hdr.Name] = buf.Bytes()
		total += n
	}
	return out, nil
}

// encodePublicKeyPEM은 raw Ed25519 PublicKey를 PKIX SubjectPublicKeyInfo PEM으로 인코딩합니다.
//
// 표준 형식: `-----BEGIN PUBLIC KEY-----\n<base64>\n-----END PUBLIC KEY-----`
// `openssl pkey -pubin` 같은 외부 도구가 바로 읽을 수 있어 검증 도구 단순.
func encodePublicKeyPEM(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("reporting: marshal PKIX: %w", err)
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	return pem.EncodeToMemory(block), nil
}

// decodePublicKeyPEM은 PEM bytes에서 ed25519.PublicKey를 추출합니다.
func decodePublicKeyPEM(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("reporting: PEM decode failed")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("reporting: parse PKIX: %w", err)
	}
	edPub, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("reporting: PEM is not Ed25519 PublicKey")
	}
	return edPub, nil
}
