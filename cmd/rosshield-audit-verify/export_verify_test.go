package main

// export_verify_test.go — Phase 10.D-5: fg-verify export 서브커맨드 단위 테스트.
//
// 본 테스트는 v0.9.0 호환 (v1) bundle 과 v0.10.0+ (v2) bundle 양쪽이 PASS 하는지,
// 그리고 변조된 fixture (서명 변조 · epoch 누락 · public key tamper) 가 FAIL 하는지
// 확인합니다. testdata 디렉터리에 정상 v1/v2 fixture 를 함께 보존.

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

type fixtureKey struct {
	epoch     int64
	keyID     string
	pub       ed25519.PublicKey
	priv      ed25519.PrivateKey
	createdAt time.Time
	revokedAt *time.Time
}

// buildFixtureKey 는 새 ed25519 key 와 fixture metadata 를 생성합니다.
func buildFixtureKey(t *testing.T, epoch int64, createdAt time.Time) fixtureKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey epoch=%d: %v", epoch, err)
	}
	sum := sha256.Sum256(pub)
	return fixtureKey{
		epoch:     epoch,
		keyID:     "key_e" + strings.ToLower(hex.EncodeToString(sum[:4])),
		pub:       pub,
		priv:      priv,
		createdAt: createdAt,
	}
}

// fixtureEntry 는 audit.Entry slice 를 hash chain link 와 함께 만듭니다.
//
// entries 의 각 element 는 (action, keyEpoch) 만 명시 — 나머지 필드는 deterministic
// default 로 채움.
type fixtureEntryInput struct {
	action   string
	keyEpoch *int64
}

func buildFixtureEntries(t *testing.T, tenant string, inputs []fixtureEntryInput) []audit.Entry {
	t.Helper()
	out := make([]audit.Entry, 0, len(inputs))
	var prev audit.Hash // zero for first.
	for i, in := range inputs {
		seq := int64(i + 1)
		e := audit.Entry{
			TenantID:   storage.TenantID(tenant),
			Seq:        seq,
			OccurredAt: time.Date(2026, 5, 21, 0, 0, int(seq), 0, time.UTC),
			Actor: audit.Actor{
				Type: audit.ActorSystem,
				ID:   "scheduler",
			},
			Action: in.action,
			Target: audit.Target{
				Type: "chain",
				ID:   "system",
			},
			Outcome:       audit.OutcomeSuccess,
			PayloadDigest: audit.ComputePayloadDigest([]byte(in.action)),
			PrevHash:      prev,
			KeyEpoch:      in.keyEpoch,
		}
		hash, err := audit.ComputeEntryHash(e.PrevHash, e.PayloadDigest, e)
		if err != nil {
			t.Fatalf("ComputeEntryHash seq=%d: %v", seq, err)
		}
		e.Hash = hash
		prev = hash
		out = append(out, e)
	}
	return out
}

// buildBundle 은 entries + signature line 을 NDJSON+gzip 으로 묶어 byte slice 를 반환합니다.
//
// version="" 이면 v1 (BundleVersion 미설정), "v2" 면 v2 (chainKeyEpochs 포함).
// signingKey 는 SignedDigest 를 서명할 key (보통 마지막 epoch 의 key).
func buildBundle(t *testing.T, entries []audit.Entry, version string, signingKey fixtureKey, allKeys []fixtureKey, fromSeq, toSeq int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		line, err := audit.MarshalEntryLine(e)
		if err != nil {
			t.Fatalf("MarshalEntryLine seq=%d: %v", e.Seq, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	digest := sha256.Sum256(buf.Bytes())
	sig := ed25519.Sign(signingKey.priv, digest[:])

	sl := audit.ExportSignatureLine{
		BundleVersion: version,
		From:          fromSeq,
		KeyID:         signingKey.keyID,
		PublicKey:     hex.EncodeToString(signingKey.pub),
		SignedDigest:  hex.EncodeToString(digest[:]),
		Signature:     hex.EncodeToString(sig),
		To:            toSeq,
	}
	if version == audit.BundleVersionV2 {
		for _, k := range allKeys {
			row := audit.ExportChainKeyEpoch{
				CreatedAt:    k.createdAt.UTC().Format(time.RFC3339Nano),
				Epoch:        k.epoch,
				KeyID:        k.keyID,
				PublicKeyHex: hex.EncodeToString(k.pub),
			}
			if k.revokedAt != nil {
				row.RevokedAt = k.revokedAt.UTC().Format(time.RFC3339Nano)
			}
			sl.ChainKeyEpochs = append(sl.ChainKeyEpochs, row)
		}
	}
	sigBytes, err := audit.MarshalSignatureLine(sl)
	if err != nil {
		t.Fatalf("MarshalSignatureLine: %v", err)
	}

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(buf.Bytes()); err != nil {
		t.Fatalf("gz write entries: %v", err)
	}
	if _, err := gz.Write(sigBytes); err != nil {
		t.Fatalf("gz write sig: %v", err)
	}
	if _, err := gz.Write([]byte{'\n'}); err != nil {
		t.Fatalf("gz write newline: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	return gzBuf.Bytes()
}

// writeBundle 은 bundle bytes 를 temp 파일로 작성하고 경로를 반환합니다.
func writeBundle(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// TestExportVerify_V1Bundle — v0.9.0 호환 (epoch=1 default) bundle 이 PASS.
func TestExportVerify_V1Bundle(t *testing.T) {
	t.Parallel()
	k := buildFixtureKey(t, 1, time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC))
	// v1 entries: KeyEpoch 는 nil (legacy) — _bundleVersion 부재 → epoch=1 default.
	entries := buildFixtureEntries(t, "system", []fixtureEntryInput{
		{action: "robot.create", keyEpoch: nil},
		{action: "scan.execute", keyEpoch: nil},
		{action: "report.export", keyEpoch: nil},
	})
	bundle := buildBundle(t, entries, "", k, nil, 1, 3)
	path := writeBundle(t, "v1.ndjson.gz", bundle)

	out := verifyExportBundle(path)
	if !out.OK {
		t.Fatalf("v1 bundle FAIL: %s\nsteps=%+v", out.Reason, out.Steps)
	}
	if out.BundleVersion != "v1" {
		t.Errorf("BundleVersion=%q want v1", out.BundleVersion)
	}
	if out.EntryCount != 3 {
		t.Errorf("EntryCount=%d want 3", out.EntryCount)
	}
	if out.RotationEntries != 0 {
		t.Errorf("RotationEntries=%d want 0", out.RotationEntries)
	}
}

// TestExportVerify_V2Bundle_EpochTransition — v0.10.0 bundle: epoch 1→2→3 rotation
// entry 와 함께 모든 검증 단계 PASS.
func TestExportVerify_V2Bundle_EpochTransition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	k1 := buildFixtureKey(t, 1, now.AddDate(0, -6, 0))
	k2 := buildFixtureKey(t, 2, now.AddDate(0, -3, 0))
	k3 := buildFixtureKey(t, 3, now)
	rev1 := k2.createdAt
	rev2 := k3.createdAt
	k1.revokedAt = &rev1
	k2.revokedAt = &rev2

	e1, e2, e3 := int64(1), int64(2), int64(3)
	entries := buildFixtureEntries(t, "system", []fixtureEntryInput{
		{action: "robot.create", keyEpoch: &e1},
		{action: "audit.chain.key_rotated", keyEpoch: &e2}, // epoch 1→2 transition.
		{action: "scan.execute", keyEpoch: &e2},
		{action: "audit.chain.key_rotated", keyEpoch: &e3}, // epoch 2→3 transition.
		{action: "report.export", keyEpoch: &e3},
	})
	// signing key = 가장 최근 활성 epoch (k3).
	bundle := buildBundle(t, entries, audit.BundleVersionV2, k3, []fixtureKey{k1, k2, k3}, 1, 5)
	path := writeBundle(t, "v2.ndjson.gz", bundle)

	out := verifyExportBundle(path)
	if !out.OK {
		t.Fatalf("v2 bundle FAIL: %s\nsteps=%+v", out.Reason, out.Steps)
	}
	if out.BundleVersion != "v2" {
		t.Errorf("BundleVersion=%q want v2", out.BundleVersion)
	}
	if out.EntryCount != 5 {
		t.Errorf("EntryCount=%d want 5", out.EntryCount)
	}
	if out.EpochCount != 3 {
		t.Errorf("EpochCount=%d want 3", out.EpochCount)
	}
	if out.RotationEntries != 2 {
		t.Errorf("RotationEntries=%d want 2", out.RotationEntries)
	}
	if out.SigningKeyID != k3.keyID {
		t.Errorf("SigningKeyID=%s want %s", out.SigningKeyID, k3.keyID)
	}
}

// TestExportVerify_V2Bundle_SignatureTampered — v2 bundle 의 signature line _signature
// 를 변조하면 FAIL.
func TestExportVerify_V2Bundle_SignatureTampered(t *testing.T) {
	t.Parallel()
	k := buildFixtureKey(t, 1, time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC))
	ke := int64(1)
	entries := buildFixtureEntries(t, "system", []fixtureEntryInput{
		{action: "robot.create", keyEpoch: &ke},
	})
	bundle := buildBundle(t, entries, audit.BundleVersionV2, k, []fixtureKey{k}, 1, 1)
	tampered := tamperSignature(t, bundle)
	path := writeBundle(t, "tampered.ndjson.gz", tampered)

	out := verifyExportBundle(path)
	if out.OK {
		t.Fatalf("expected FAIL, got PASS")
	}
	// signature step 이 실패해야 함.
	found := false
	for _, s := range out.Steps {
		if s.Name == "signature" && !s.OK {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("signature step not marked FAIL; steps=%+v", out.Steps)
	}
}

// TestExportVerify_V2Bundle_EpochMissing — v2 bundle 의 chainKeyEpochs 에서 epoch=2 를
// 제거하면, rotation entry seq=2 (epoch=2) 가 lookup 실패로 FAIL.
func TestExportVerify_V2Bundle_EpochMissing(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	k1 := buildFixtureKey(t, 1, now.AddDate(0, -3, 0))
	k2 := buildFixtureKey(t, 2, now)
	rev1 := k2.createdAt
	k1.revokedAt = &rev1

	e1, e2 := int64(1), int64(2)
	entries := buildFixtureEntries(t, "system", []fixtureEntryInput{
		{action: "robot.create", keyEpoch: &e1},
		{action: "audit.chain.key_rotated", keyEpoch: &e2},
	})
	// chainKeyEpochs 에서 k2 를 의도적으로 빼면 — signing key (k2) lookup 실패.
	bundle := buildBundle(t, entries, audit.BundleVersionV2, k2, []fixtureKey{k1}, 1, 2)
	path := writeBundle(t, "epoch-missing.ndjson.gz", bundle)

	out := verifyExportBundle(path)
	if out.OK {
		t.Fatalf("expected FAIL, got PASS (reason=%s)", out.Reason)
	}
	if !strings.Contains(out.Reason, "not found in _chainKeyEpochs") &&
		!strings.Contains(out.Reason, "chainKeyEpochs") {
		t.Errorf("Reason=%q does not mention chainKeyEpochs lookup; steps=%+v", out.Reason, out.Steps)
	}
}

// TestExportVerify_V2Bundle_PublicKeyTampered — chainKeyEpochs[k].publicKeyHex 를 다른
// key 의 public 으로 변경하면 signature verify FAIL.
func TestExportVerify_V2Bundle_PublicKeyTampered(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	k1 := buildFixtureKey(t, 1, now)
	imposter, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("imposter GenerateKey: %v", err)
	}
	ke := int64(1)
	entries := buildFixtureEntries(t, "system", []fixtureEntryInput{
		{action: "robot.create", keyEpoch: &ke},
	})
	bundle := buildBundle(t, entries, audit.BundleVersionV2, k1, []fixtureKey{k1}, 1, 1)
	tampered := tamperChainKeyPublicKey(t, bundle, k1.keyID, imposter)
	path := writeBundle(t, "pubkey-tampered.ndjson.gz", tampered)

	out := verifyExportBundle(path)
	if out.OK {
		t.Fatalf("expected FAIL, got PASS")
	}
	signatureFailed := false
	for _, s := range out.Steps {
		if s.Name == "signature" && !s.OK {
			signatureFailed = true
			break
		}
	}
	if !signatureFailed {
		t.Errorf("signature step not marked FAIL; steps=%+v", out.Steps)
	}
}

// TestExportVerify_V2Bundle_EpochRegression — rotation entry 의 keyEpoch 가 이전 entry
// 와 동일 (regression) 이면 FAIL.
func TestExportVerify_V2Bundle_EpochRegression(t *testing.T) {
	t.Parallel()
	k1 := buildFixtureKey(t, 1, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	e1 := int64(1)
	// rotation entry 의 keyEpoch 가 동일 epoch (1) — regression.
	entries := buildFixtureEntries(t, "system", []fixtureEntryInput{
		{action: "robot.create", keyEpoch: &e1},
		{action: "audit.chain.key_rotated", keyEpoch: &e1},
	})
	bundle := buildBundle(t, entries, audit.BundleVersionV2, k1, []fixtureKey{k1}, 1, 2)
	path := writeBundle(t, "epoch-regression.ndjson.gz", bundle)

	out := verifyExportBundle(path)
	if out.OK {
		t.Fatalf("expected FAIL, got PASS")
	}
	if !strings.Contains(out.Reason, "exceed prev epoch") {
		t.Errorf("Reason=%q should mention epoch regression", out.Reason)
	}
}

// TestExportVerify_HashChainTampered — entry 의 PayloadDigest 변조 시 chain step 에서 FAIL.
func TestExportVerify_HashChainTampered(t *testing.T) {
	t.Parallel()
	k := buildFixtureKey(t, 1, time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC))
	ke := int64(1)
	entries := buildFixtureEntries(t, "system", []fixtureEntryInput{
		{action: "robot.create", keyEpoch: &ke},
		{action: "scan.execute", keyEpoch: &ke},
	})
	// 두 번째 entry 의 PayloadDigest 를 변조 (re-hash 안 함) — Hash 와 mismatch.
	entries[1].PayloadDigest = audit.Hash{0xff, 0xee, 0xdd}
	// signature line 은 변조 후 entries 기준으로 다시 만듦 (signature 자체는 PASS, chain step 만 FAIL).
	bundle := buildBundle(t, entries, audit.BundleVersionV2, k, []fixtureKey{k}, 1, 2)
	path := writeBundle(t, "chain-tampered.ndjson.gz", bundle)

	out := verifyExportBundle(path)
	if out.OK {
		t.Fatalf("expected FAIL, got PASS")
	}
	chainFailed := false
	for _, s := range out.Steps {
		if s.Name == "chain" && !s.OK {
			chainFailed = true
			break
		}
	}
	if !chainFailed {
		t.Errorf("chain step not marked FAIL; steps=%+v reason=%s", out.Steps, out.Reason)
	}
}

// TestExportVerify_TestdataFixtures_PASS — testdata 디렉터리에 보존된 deterministic
// fixture 두 개 (v1 + v2) 가 모두 PASS — fixture wire 회귀 가드.
func TestExportVerify_TestdataFixtures_PASS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		path           string
		wantVersion    string
		wantEntries    int
		wantRotations  int
		wantEpochCount int
	}{
		{
			name:           "v1_legacy_default_epoch",
			path:           "testdata/v1_bundle.ndjson.gz",
			wantVersion:    "v1",
			wantEntries:    3,
			wantRotations:  0,
			wantEpochCount: 0,
		},
		{
			name:           "v2_three_epoch_rotation_chain",
			path:           "testdata/v2_bundle.ndjson.gz",
			wantVersion:    "v2",
			wantEntries:    5,
			wantRotations:  2,
			wantEpochCount: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := os.Stat(tc.path); err != nil {
				t.Skipf("fixture missing (run -tags=fixturegen TestGenerateFixtures): %v", err)
			}
			out := verifyExportBundle(tc.path)
			if !out.OK {
				t.Fatalf("FAIL: %s\nsteps=%+v", out.Reason, out.Steps)
			}
			if out.BundleVersion != tc.wantVersion {
				t.Errorf("BundleVersion=%q want %q", out.BundleVersion, tc.wantVersion)
			}
			if out.EntryCount != tc.wantEntries {
				t.Errorf("EntryCount=%d want %d", out.EntryCount, tc.wantEntries)
			}
			if out.RotationEntries != tc.wantRotations {
				t.Errorf("RotationEntries=%d want %d", out.RotationEntries, tc.wantRotations)
			}
			if out.EpochCount != tc.wantEpochCount {
				t.Errorf("EpochCount=%d want %d", out.EpochCount, tc.wantEpochCount)
			}
		})
	}
}

// TestExportVerify_RunExport_V1Bundle_ExitZero — runExport CLI entry-point 이 v1 bundle 에
// 대해 exit code 0 을 반환.
func TestExportVerify_RunExport_V1Bundle_ExitZero(t *testing.T) {
	t.Parallel()
	k := buildFixtureKey(t, 1, time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC))
	entries := buildFixtureEntries(t, "system", []fixtureEntryInput{
		{action: "robot.create", keyEpoch: nil},
	})
	bundle := buildBundle(t, entries, "", k, nil, 1, 1)
	path := writeBundle(t, "v1.ndjson.gz", bundle)

	var stdout, stderr string
	stdout, stderr = captureStdio(t, func() {
		code := runExport([]string{"--bundle", path, "--format", "json"})
		if code != 0 {
			t.Errorf("runExport exit=%d want 0", code)
		}
	})
	if !strings.Contains(stdout, `"result": "PASS"`) {
		t.Errorf("stdout missing PASS: %s\nstderr=%s", stdout, stderr)
	}
}

// tamperSignature 는 gzip bundle 안 signature line 의 _signature hex 첫 byte 를 flip.
func tamperSignature(t *testing.T, gzBundle []byte) []byte {
	t.Helper()
	ndjson, err := gunzipExport(gzBundle)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	entriesBytes, sigLine, err := splitExportLines(ndjson)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var sl audit.ExportSignatureLine
	if err := json.Unmarshal(sigLine, &sl); err != nil {
		t.Fatalf("unmarshal sig: %v", err)
	}
	// signature hex 첫 byte 의 nibble 을 flip (00 → 11 보장).
	raw, err := hex.DecodeString(sl.Signature)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	raw[0] ^= 0xFF
	sl.Signature = hex.EncodeToString(raw)

	newSig, err := audit.MarshalSignatureLine(sl)
	if err != nil {
		t.Fatalf("re-marshal sig: %v", err)
	}
	return regzipBundle(t, entriesBytes, newSig)
}

// tamperChainKeyPublicKey 는 chainKeyEpochs 안 keyID 매칭 row 의 publicKeyHex 를 imposter
// public key 로 교체합니다.
func tamperChainKeyPublicKey(t *testing.T, gzBundle []byte, targetKeyID string, imposter ed25519.PublicKey) []byte {
	t.Helper()
	ndjson, err := gunzipExport(gzBundle)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	entriesBytes, sigLine, err := splitExportLines(ndjson)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var sl audit.ExportSignatureLine
	if err := json.Unmarshal(sigLine, &sl); err != nil {
		t.Fatalf("unmarshal sig: %v", err)
	}
	for i := range sl.ChainKeyEpochs {
		if sl.ChainKeyEpochs[i].KeyID == targetKeyID {
			sl.ChainKeyEpochs[i].PublicKeyHex = hex.EncodeToString(imposter)
		}
	}
	newSig, err := audit.MarshalSignatureLine(sl)
	if err != nil {
		t.Fatalf("re-marshal sig: %v", err)
	}
	return regzipBundle(t, entriesBytes, newSig)
}

// regzipBundle 은 entries stream + new signature line 으로 gzip bundle 을 재구성합니다.
func regzipBundle(t *testing.T, entriesStream, sigLine []byte) []byte {
	t.Helper()
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(entriesStream); err != nil {
		t.Fatalf("gz entries: %v", err)
	}
	if _, err := gz.Write(sigLine); err != nil {
		t.Fatalf("gz sig: %v", err)
	}
	if _, err := gz.Write([]byte{'\n'}); err != nil {
		t.Fatalf("gz newline: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	return gzBuf.Bytes()
}
