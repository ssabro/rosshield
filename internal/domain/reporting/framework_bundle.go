package reporting

// framework_bundle.go — E18 후속 (Phase 2 Exit) — Framework 리포트 외부 검증 가능 번들.
//
// session 리포트 bundle.go와 동일 패턴이지만 별도 파일·별도 anchor schema:
//   - PDF entry 이름은 framework-report.pdf (session과 헷갈리지 않게)
//   - anchor JSON에 profileId/snapshotId/framework 추가 (외부 검증자가 어떤 framework·snapshot에서 나온 PDF인지 식별)
//
// 검증 흐름은 session bundle과 동일 — ed25519.Verify(pubKey, pdfBytes, sig).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Framework 번들 entry 파일 이름 — 외부 검증 도구 호환.
const (
	FrameworkBundleFilePDF       = "framework-report.pdf"
	FrameworkBundleFileSignature = "framework-report.pdf.sig"
	FrameworkBundleFileAnchor    = "framework-anchor.json"
	FrameworkBundleFilePublicKey = "public-key.pem"
)

// FrameworkAnchorPayload는 framework-anchor.json의 와이어 형식입니다.
//
// session AnchorPayload + framework 메타 (profileId·snapshotId·framework·frameworkVersion).
// JSON canonical: 키 알파벳순.
type FrameworkAnchorPayload struct {
	ChainHeadHash    string `json:"chainHeadHash"`
	ChainHeadSeq     int64  `json:"chainHeadSeq"`
	Framework        string `json:"framework"`
	FrameworkVersion string `json:"frameworkVersion"`
	ProfileID        string `json:"profileId"`
	SignedAt         string `json:"signedAt"`
	SignerKeyID      string `json:"signerKeyId"`
	SnapshotID       string `json:"snapshotId"`
	TenantID         string `json:"tenantId"`
}

// FrameworkVerifyResult는 VerifyFrameworkBundle의 출력입니다.
type FrameworkVerifyResult struct {
	OK               bool
	Reason           string
	PDFSize          int64
	PDFSHA256        string
	SignerKeyID      string
	ChainHeadSeq     int64
	ChainHeadHash    string
	ProfileID        string
	SnapshotID       string
	Framework        string
	FrameworkVersion string
}

// BuildFrameworkBundle은 서명된 FrameworkReport를 외부 검증 가능한 tar.gz 번들로 묶습니다.
//
// FrameworkReport는 Sign 단계를 거쳐 Signature.Signature가 비-zero여야 합니다.
// publicKey는 raw 32B Ed25519 PublicKey.
//
// framework·frameworkVersion은 호출자가 별도로 제공 — FrameworkReport에는 없음(profile에서 lookup해야 함).
// caller(bootstrap)가 ComplianceReader로 회수해 전달.
func BuildFrameworkBundle(report FrameworkReport, framework, frameworkVersion string, publicKey ed25519.PublicKey) ([]byte, error) {
	if len(report.PDF) == 0 {
		return nil, fmt.Errorf("reporting: framework report.PDF is empty")
	}
	if report.Signature.IsZero() {
		return nil, fmt.Errorf("reporting: framework report not signed (call SignFramework first)")
	}
	if len(report.Signature.Signature) != ed25519SignatureSize {
		return nil, ErrBundleSignatureSize
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("reporting: publicKey size = %d, want %d",
			len(publicKey), ed25519.PublicKeySize)
	}

	anchor := FrameworkAnchorPayload{
		TenantID:         string(report.TenantID),
		ProfileID:        report.ProfileID,
		SnapshotID:       report.SnapshotID,
		Framework:        framework,
		FrameworkVersion: frameworkVersion,
		ChainHeadSeq:     report.Signature.ChainHeadSeq,
		ChainHeadHash:    report.Signature.ChainHeadHash,
		SignedAt:         report.Signature.SignedAt.UTC().Format(time.RFC3339Nano),
		SignerKeyID:      report.Signature.SignerKeyID,
	}
	anchorBytes, err := json.Marshal(anchor)
	if err != nil {
		return nil, fmt.Errorf("reporting: marshal framework anchor: %w", err)
	}

	pemBytes, err := encodePublicKeyPEM(publicKey)
	if err != nil {
		return nil, err
	}

	entries := []struct {
		name string
		body []byte
	}{
		{FrameworkBundleFilePDF, report.PDF},
		{FrameworkBundleFileSignature, report.Signature.Signature},
		{FrameworkBundleFileAnchor, anchorBytes},
		{FrameworkBundleFilePublicKey, pemBytes},
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

// VerifyFrameworkBundle은 framework tar.gz 번들의 서명·anchor를 검증합니다.
//
// 검증 순서는 session VerifyBundle과 동일 — 단, anchor schema가 다름.
func VerifyFrameworkBundle(tarGz []byte, expectedPublicKey ed25519.PublicKey) (FrameworkVerifyResult, error) {
	files, err := extractBundleTarGz(tarGz)
	if err != nil {
		return FrameworkVerifyResult{}, err
	}
	pdfBytes, ok := files[FrameworkBundleFilePDF]
	if !ok {
		return FrameworkVerifyResult{}, ErrBundleMissingPDF
	}
	sigBytes, ok := files[FrameworkBundleFileSignature]
	if !ok {
		return FrameworkVerifyResult{}, ErrBundleMissingSignature
	}
	anchorBytes, ok := files[FrameworkBundleFileAnchor]
	if !ok {
		return FrameworkVerifyResult{}, ErrBundleMissingAnchor
	}
	pemBytes, ok := files[FrameworkBundleFilePublicKey]
	if !ok {
		return FrameworkVerifyResult{}, ErrBundleMissingPubKey
	}

	if len(sigBytes) != ed25519.SignatureSize {
		return FrameworkVerifyResult{
			OK: false, Reason: fmt.Sprintf("signature size=%d", len(sigBytes)),
		}, ErrBundleSignatureSize
	}

	bundlePub, err := decodePublicKeyPEM(pemBytes)
	if err != nil {
		return FrameworkVerifyResult{OK: false, Reason: err.Error()}, ErrBundlePubKeyMalformed
	}
	if expectedPublicKey != nil && !bytes.Equal(bundlePub, expectedPublicKey) {
		return FrameworkVerifyResult{OK: false, Reason: "public key mismatch"}, ErrBundlePubKeyMismatch
	}

	if !ed25519.Verify(bundlePub, pdfBytes, sigBytes) {
		return FrameworkVerifyResult{OK: false, Reason: "ed25519 verify failed"}, ErrBundleSignatureInvalid
	}

	var anchor FrameworkAnchorPayload
	if err := json.Unmarshal(anchorBytes, &anchor); err != nil {
		return FrameworkVerifyResult{OK: false, Reason: err.Error()}, ErrBundleAnchorMalformed
	}

	sum := sha256.Sum256(pdfBytes)
	return FrameworkVerifyResult{
		OK:               true,
		PDFSize:          int64(len(pdfBytes)),
		PDFSHA256:        hex.EncodeToString(sum[:]),
		SignerKeyID:      anchor.SignerKeyID,
		ChainHeadSeq:     anchor.ChainHeadSeq,
		ChainHeadHash:    anchor.ChainHeadHash,
		ProfileID:        anchor.ProfileID,
		SnapshotID:       anchor.SnapshotID,
		Framework:        anchor.Framework,
		FrameworkVersion: anchor.FrameworkVersion,
	}, nil
}
