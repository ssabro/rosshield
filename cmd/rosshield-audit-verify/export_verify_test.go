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

// v3FixtureEntryInput 은 v3 entry 의 input — leaderEpoch 노출.
type v3FixtureEntryInput struct {
	action      string
	keyEpoch    *int64
	leaderEpoch *int64
}

// buildV3FixtureEntries 는 v3 hash 분기 (v1 ↔ v3) 를 일관 적용해 entries 를 만듭니다.
// sqliterepo.Repo.recomputeHashForSeq 와 byte-identical — e.Seq <= transitionSeq → v1 hash,
// 그 외 → v3 hash. 외부 fg-verify v3 가 같은 분기로 hash recompute.
func buildV3FixtureEntries(t *testing.T, tenant string, inputs []v3FixtureEntryInput, transitionSeq int64) []audit.Entry {
	t.Helper()
	out := make([]audit.Entry, 0, len(inputs))
	var prev audit.Hash
	for i, in := range inputs {
		seq := int64(i + 1)
		e := audit.Entry{
			TenantID:   storage.TenantID(tenant),
			Seq:        seq,
			OccurredAt: time.Date(2026, 5, 22, 0, 0, int(seq), 0, time.UTC),
			Actor: audit.Actor{
				Type: audit.ActorSystem,
				ID:   "scheduler",
			},
			Action:        in.action,
			Target:        audit.Target{Type: "chain", ID: "system"},
			Outcome:       audit.OutcomeSuccess,
			PayloadDigest: audit.ComputePayloadDigest([]byte(in.action)),
			PrevHash:      prev,
			KeyEpoch:      in.keyEpoch,
			LeaderEpoch:   in.leaderEpoch,
		}
		var (
			h   audit.Hash
			err error
		)
		if transitionSeq > 0 && seq > transitionSeq {
			h, err = audit.ComputeEntryHashV3(e.PrevHash, e.PayloadDigest, e)
		} else {
			h, err = audit.ComputeEntryHash(e.PrevHash, e.PayloadDigest, e)
		}
		if err != nil {
			t.Fatalf("compute hash seq=%d: %v", seq, err)
		}
		e.Hash = h
		prev = h
		out = append(out, e)
	}
	return out
}

// buildV3Bundle 은 v3 entries 를 MarshalEntryLineV3 로 직렬화 + signature line 에
// HashVersionTransitionAt 명시 + chainKeyEpochs 포함 + gzip wrap 합니다.
func buildV3Bundle(t *testing.T, entries []audit.Entry, signingKey fixtureKey, allKeys []fixtureKey,
	fromSeq, toSeq, transitionSeq int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		line, err := audit.MarshalEntryLineV3(e)
		if err != nil {
			t.Fatalf("MarshalEntryLineV3 seq=%d: %v", e.Seq, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	digest := sha256.Sum256(buf.Bytes())
	sig := ed25519.Sign(signingKey.priv, digest[:])

	sl := audit.ExportSignatureLine{
		BundleVersion:           audit.BundleVersionV3,
		From:                    fromSeq,
		HashVersionTransitionAt: transitionSeq,
		KeyID:                   signingKey.keyID,
		PublicKey:               hex.EncodeToString(signingKey.pub),
		SignedDigest:            hex.EncodeToString(digest[:]),
		Signature:               hex.EncodeToString(sig),
		To:                      toSeq,
	}
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
	sigBytes, err := audit.MarshalSignatureLine(sl)
	if err != nil {
		t.Fatalf("MarshalSignatureLine: %v", err)
	}

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(buf.Bytes()); err != nil {
		t.Fatalf("gz entries: %v", err)
	}
	if _, err := gz.Write(sigBytes); err != nil {
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
// fixture (v1 + v2 + v3) 가 모두 PASS — fixture wire 회귀 가드.
func TestExportVerify_TestdataFixtures_PASS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		path              string
		wantVersion       string
		wantEntries       int
		wantRotations     int
		wantEpochCount    int
		wantTransitionSeq int64
	}{
		{
			name:              "v1_legacy_default_epoch",
			path:              "testdata/v1_bundle.ndjson.gz",
			wantVersion:       "v1",
			wantEntries:       3,
			wantRotations:     0,
			wantEpochCount:    0,
			wantTransitionSeq: 0,
		},
		{
			name:              "v2_three_epoch_rotation_chain",
			path:              "testdata/v2_bundle.ndjson.gz",
			wantVersion:       "v2",
			wantEntries:       5,
			wantRotations:     2,
			wantEpochCount:    3,
			wantTransitionSeq: 0,
		},
		{
			name:              "v3_v1_v3_split_chain",
			path:              "testdata/v3_bundle.ndjson.gz",
			wantVersion:       "v3",
			wantEntries:       5,
			wantRotations:     1, // seq=4 audit.chain.key_rotated.
			wantEpochCount:    2,
			wantTransitionSeq: 3, // seq=3 audit.chain.hash_version_changed.
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
			if out.HashVersionTransitionAt != tc.wantTransitionSeq {
				t.Errorf("HashVersionTransitionAt=%d want %d",
					out.HashVersionTransitionAt, tc.wantTransitionSeq)
			}
		})
	}
}

// makeV3Fixture 는 v3 bundle 테스트 공용 fixture: v1 entries (seq 1~3) + transition entry
// (seq=3) + v3 entries (seq 4~5). 반환: (entries, signingKey, allKeys, transitionSeq).
//
// fixture 는 sqliterepo.Repo 의 hash 분기 (seq > transitionSeq → v3 hash) 와 byte-identical.
func makeV3Fixture(t *testing.T) ([]audit.Entry, fixtureKey, []fixtureKey, int64) {
	t.Helper()
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	k1 := buildFixtureKey(t, 1, now.AddDate(0, -3, 0))
	k2 := buildFixtureKey(t, 2, now)
	rev1 := k2.createdAt
	k1.revokedAt = &rev1

	ke1, ke2 := int64(1), int64(2)
	le1, le2 := int64(7), int64(8)
	transitionSeq := int64(3)
	entries := buildV3FixtureEntries(t, "system", []v3FixtureEntryInput{
		{action: "robot.create", keyEpoch: &ke1, leaderEpoch: nil},
		{action: "scan.execute", keyEpoch: &ke1, leaderEpoch: nil},
		{action: "audit.chain.hash_version_changed", keyEpoch: &ke1, leaderEpoch: nil},
		{action: "audit.chain.key_rotated", keyEpoch: &ke2, leaderEpoch: &le1},
		{action: "report.export", keyEpoch: &ke2, leaderEpoch: &le2},
	}, transitionSeq)
	return entries, k2, []fixtureKey{k1, k2}, transitionSeq
}

// TestExportVerify_V3Bundle_RoundTrip — v1 entries + transition marker + v3 entries 혼합
// chain 이 PASS. v1 hash + v3 hash 분기가 모두 정합.
func TestExportVerify_V3Bundle_RoundTrip(t *testing.T) {
	t.Parallel()
	entries, signing, all, transitionSeq := makeV3Fixture(t)
	bundle := buildV3Bundle(t, entries, signing, all, 1, 5, transitionSeq)
	path := writeBundle(t, "v3.ndjson.gz", bundle)

	out := verifyExportBundle(path)
	if !out.OK {
		t.Fatalf("v3 bundle FAIL: %s\nsteps=%+v", out.Reason, out.Steps)
	}
	if out.BundleVersion != "v3" {
		t.Errorf("BundleVersion=%q want v3", out.BundleVersion)
	}
	if out.EntryCount != 5 {
		t.Errorf("EntryCount=%d want 5", out.EntryCount)
	}
	if out.HashVersionTransitionAt != transitionSeq {
		t.Errorf("HashVersionTransitionAt=%d want %d", out.HashVersionTransitionAt, transitionSeq)
	}
	if out.RotationEntries != 1 {
		t.Errorf("RotationEntries=%d want 1", out.RotationEntries)
	}
	if out.EpochCount != 2 {
		t.Errorf("EpochCount=%d want 2", out.EpochCount)
	}
}

// TestExportVerify_V3Bundle_PostTransitionEntryTampered — transition 이후 v3 entry 의
// PayloadDigest 변조 시 chain step 에서 FAIL (v3 hash recompute mismatch).
func TestExportVerify_V3Bundle_PostTransitionEntryTampered(t *testing.T) {
	t.Parallel()
	entries, signing, all, transitionSeq := makeV3Fixture(t)
	// seq=5 (v3 hash 분기 안) 의 PayloadDigest 변조 — Hash 와 mismatch.
	entries[4].PayloadDigest = audit.Hash{0xde, 0xad, 0xbe, 0xef}
	// signature line 은 변조 후 entries 기준 재구성 (signature/digest PASS, chain step 만 FAIL).
	bundle := buildV3Bundle(t, entries, signing, all, 1, 5, transitionSeq)
	path := writeBundle(t, "v3-post-tampered.ndjson.gz", bundle)

	out := verifyExportBundle(path)
	if out.OK {
		t.Fatalf("expected FAIL, got PASS")
	}
	found := false
	for _, s := range out.Steps {
		if s.Name == "chain" && !s.OK {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("chain step not marked FAIL; steps=%+v reason=%s", out.Steps, out.Reason)
	}
}

// TestExportVerify_V3Bundle_BoundaryHashFunctionMismatch — bundle 전체를 v1 hash 로 계산했는데
// _bundleVersion="v3" + transitionAt=3 인 경우, seq>3 entries 가 v3 hash mismatch 로 FAIL.
//
// 본 test 는 v3 bundle 의 hash 분기 boundary 자체가 변조 (entries 전체를 v1 hash 로
// 계산 — Append 분기 누락 시뮬) 를 chain step 이 잡아내는지 확인합니다.
func TestExportVerify_V3Bundle_BoundaryHashFunctionMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	k1 := buildFixtureKey(t, 1, now.AddDate(0, -3, 0))
	k2 := buildFixtureKey(t, 2, now)
	rev1 := k2.createdAt
	k1.revokedAt = &rev1

	ke1, ke2 := int64(1), int64(2)
	le1 := int64(7)
	// transitionSeq 를 false 0 으로 buildV3FixtureEntries 호출 → 모든 entry 가 v1 hash.
	entries := buildV3FixtureEntries(t, "system", []v3FixtureEntryInput{
		{action: "robot.create", keyEpoch: &ke1, leaderEpoch: nil},
		{action: "scan.execute", keyEpoch: &ke1, leaderEpoch: nil},
		{action: "audit.chain.hash_version_changed", keyEpoch: &ke1, leaderEpoch: nil},
		// seq=4: leaderEpoch 비-nil → v1 hash 와 v3 hash 결과가 다름 (v1 함수는 leaderEpoch 무시).
		{action: "audit.chain.key_rotated", keyEpoch: &ke2, leaderEpoch: &le1},
		{action: "report.export", keyEpoch: &ke2, leaderEpoch: &le1},
	}, 0) // ← transitionSeq=0 → 모든 entry v1 hash 로 잘못 계산.
	// signature line 은 transitionAt=3 명시 → verifier 가 seq>3 entries 를 v3 hash 로 재계산 → mismatch.
	bundle := buildV3Bundle(t, entries, k2, []fixtureKey{k1, k2}, 1, 5, 3)
	path := writeBundle(t, "v3-boundary-mismatch.ndjson.gz", bundle)

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

// TestExportVerify_V3Bundle_TransitionAtTampered — _hashVersionTransitionAt 를 잘못된 seq
// (실제 transition entry seq 와 불일치) 로 변조하면 hashVersionTransition step 에서 FAIL.
func TestExportVerify_V3Bundle_TransitionAtTampered(t *testing.T) {
	t.Parallel()
	entries, signing, all, _ := makeV3Fixture(t)
	// 잘못된 transitionSeq 4 (실제 transition entry 는 seq=3) — signature line 만 변조.
	// entries 는 transitionSeq=3 기준 hash 계산 (정상) — chain PASS, transition marker FAIL.
	correctEntries, _, _, correctTS := makeV3Fixture(t)
	_ = entries
	_ = correctTS
	bundle := buildV3Bundle(t, correctEntries, signing, all, 1, 5, 4)
	path := writeBundle(t, "v3-transitionAt-tampered.ndjson.gz", bundle)

	out := verifyExportBundle(path)
	if out.OK {
		t.Fatalf("expected FAIL, got PASS")
	}
	// chain step 은 entries seq>4 → v3 hash 로 재계산하지만 실제 entries 는 seq>3 이미 v3 hash.
	// seq=4 entry 가 v1 hash (잘못된 transitionAt 분기) 로 재계산 → mismatch. chain step 에서 FAIL.
	// 혹은 hashVersionTransition step 이 먼저 도달할 수 있음 — 둘 중 어디든 FAIL 이면 OK.
	failed := false
	for _, s := range out.Steps {
		if (s.Name == "chain" || s.Name == "hashVersionTransition") && !s.OK {
			failed = true
			break
		}
	}
	if !failed {
		t.Errorf("expected chain or hashVersionTransition step FAIL; steps=%+v reason=%s",
			out.Steps, out.Reason)
	}
}

// TestExportVerify_V3Bundle_SignatureTampered — v3 bundle 의 _signature 변조 시 signature
// step 이 FAIL (v2 와 동일 경로 — v3 도 회귀 0 확인).
func TestExportVerify_V3Bundle_SignatureTampered(t *testing.T) {
	t.Parallel()
	entries, signing, all, transitionSeq := makeV3Fixture(t)
	bundle := buildV3Bundle(t, entries, signing, all, 1, 5, transitionSeq)
	tampered := tamperSignature(t, bundle)
	path := writeBundle(t, "v3-sig-tampered.ndjson.gz", tampered)

	out := verifyExportBundle(path)
	if out.OK {
		t.Fatalf("expected FAIL, got PASS")
	}
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
