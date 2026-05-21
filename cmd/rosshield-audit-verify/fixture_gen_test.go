//go:build fixturegen
// +build fixturegen

package main

// fixture_gen_test.go — testdata fixture 재생성 헬퍼 (`-tags=fixturegen`).
//
// 본 파일은 deterministic seed (chacha8) 로 ed25519 key 를 생성하여 byte-identical
// fixture 를 `cmd/rosshield-audit-verify/testdata/` 에 작성합니다. 재생성 절차는
// testdata/README.md 참조.
//
// build tag 분리 이유: 일반 `go test` run 에서는 fixture 가 변경되지 않아야 합니다 —
// fixture 는 commit 시점에 결정되며, 본 generator 는 명시적 round (예: chain key 와이어
// 변경) 시에만 실행합니다.

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"io"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// detRand 는 deterministic key 생성용 chacha8 wrapped io.Reader.
type detRand struct{ r *mathrand.ChaCha8 }

func (d *detRand) Read(p []byte) (int, error) {
	for i := range p {
		// ChaCha8 는 Uint64 단위 — 1 byte 씩 채워도 충분 (성능 무관).
		buf := d.r.Uint64()
		p[i] = byte(buf)
	}
	return len(p), nil
}

func newDetRand(seedHi, seedLo uint64) *detRand {
	var seed [32]byte
	for i := 0; i < 8; i++ {
		seed[i] = byte(seedHi >> (8 * i))
		seed[8+i] = byte(seedLo >> (8 * i))
	}
	return &detRand{r: mathrand.NewChaCha8(seed)}
}

func detKey(t *testing.T, seedHi, seedLo uint64) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	rd := newDetRand(seedHi, seedLo)
	pub, priv, err := ed25519.GenerateKey(rd)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func detKeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "key_e" + hex.EncodeToString(sum[:4])
}

// TestGenerateFixtures 는 v1 + v2 fixture 를 testdata 에 작성합니다.
//
// run: `go test -tags=fixturegen -run TestGenerateFixtures ./cmd/rosshield-audit-verify/...`
func TestGenerateFixtures(t *testing.T) {
	dir := filepath.Join("testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// === v1 fixture ===
	v1Pub, v1Priv := detKey(t, 0xC0FFEE0001, 0xDEADBEEF0001)
	v1KeyID := detKeyID(v1Pub)

	v1Entries := buildDetEntries(t, "system", []detEntryInput{
		{action: "robot.create", keyEpoch: nil},
		{action: "scan.execute", keyEpoch: nil},
		{action: "report.export", keyEpoch: nil},
	}, time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC))
	v1Bundle := assembleBundle(t, v1Entries, "", v1KeyID, v1Pub, v1Priv, nil, 1, 3)
	v1Path := filepath.Join(dir, "v1_bundle.ndjson.gz")
	if err := os.WriteFile(v1Path, v1Bundle, 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	t.Logf("wrote %s (%d bytes)", v1Path, len(v1Bundle))

	// === v2 fixture: epoch 1→2→3 transition ===
	k1Pub, _ := detKey(t, 0xA1A1A100001, 0xB1B1B100001)
	k2Pub, _ := detKey(t, 0xA2A2A200002, 0xB2B2B200002)
	k3Pub, k3Priv := detKey(t, 0xA3A3A300003, 0xB3B3B300003)
	k1ID, k2ID, k3ID := detKeyID(k1Pub), detKeyID(k2Pub), detKeyID(k3Pub)

	t0 := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	rev1 := t0.AddDate(0, 0, 90)
	rev2 := t0.AddDate(0, 0, 180)

	e1, e2, e3 := int64(1), int64(2), int64(3)
	v2Entries := buildDetEntries(t, "system", []detEntryInput{
		{action: "robot.create", keyEpoch: &e1},
		{action: "audit.chain.key_rotated", keyEpoch: &e2},
		{action: "scan.execute", keyEpoch: &e2},
		{action: "audit.chain.key_rotated", keyEpoch: &e3},
		{action: "report.export", keyEpoch: &e3},
	}, t0)

	chainKeys := []audit.ExportChainKeyEpoch{
		{
			CreatedAt:    t0.UTC().Format(time.RFC3339Nano),
			Epoch:        1,
			KeyID:        k1ID,
			PublicKeyHex: hex.EncodeToString(k1Pub),
			RevokedAt:    rev1.UTC().Format(time.RFC3339Nano),
		},
		{
			CreatedAt:    rev1.UTC().Format(time.RFC3339Nano),
			Epoch:        2,
			KeyID:        k2ID,
			PublicKeyHex: hex.EncodeToString(k2Pub),
			RevokedAt:    rev2.UTC().Format(time.RFC3339Nano),
		},
		{
			CreatedAt:    rev2.UTC().Format(time.RFC3339Nano),
			Epoch:        3,
			KeyID:        k3ID,
			PublicKeyHex: hex.EncodeToString(k3Pub),
		},
	}
	v2Bundle := assembleBundleWithChainKeys(t, v2Entries, audit.BundleVersionV2,
		k3ID, k3Pub, k3Priv, chainKeys, 1, 5)
	v2Path := filepath.Join(dir, "v2_bundle.ndjson.gz")
	if err := os.WriteFile(v2Path, v2Bundle, 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	t.Logf("wrote %s (%d bytes)", v2Path, len(v2Bundle))
}

type detEntryInput struct {
	action   string
	keyEpoch *int64
}

func buildDetEntries(t *testing.T, tenant string, inputs []detEntryInput, base time.Time) []audit.Entry {
	t.Helper()
	out := make([]audit.Entry, 0, len(inputs))
	var prev audit.Hash
	for i, in := range inputs {
		seq := int64(i + 1)
		e := audit.Entry{
			TenantID:   storage.TenantID(tenant),
			Seq:        seq,
			OccurredAt: base.Add(time.Duration(seq) * time.Second),
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
		}
		h, err := audit.ComputeEntryHash(e.PrevHash, e.PayloadDigest, e)
		if err != nil {
			t.Fatalf("ComputeEntryHash seq=%d: %v", seq, err)
		}
		e.Hash = h
		prev = h
		out = append(out, e)
	}
	return out
}

func assembleBundle(t *testing.T, entries []audit.Entry, version, keyID string,
	pub ed25519.PublicKey, priv ed25519.PrivateKey, chainKeys []audit.ExportChainKeyEpoch,
	fromSeq, toSeq int64) []byte {
	return assembleBundleWithChainKeys(t, entries, version, keyID, pub, priv, chainKeys, fromSeq, toSeq)
}

func assembleBundleWithChainKeys(t *testing.T, entries []audit.Entry, version, keyID string,
	pub ed25519.PublicKey, priv ed25519.PrivateKey, chainKeys []audit.ExportChainKeyEpoch,
	fromSeq, toSeq int64) []byte {
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
	sig := ed25519.Sign(priv, digest[:])

	sl := audit.ExportSignatureLine{
		BundleVersion:  version,
		ChainKeyEpochs: chainKeys,
		From:           fromSeq,
		KeyID:          keyID,
		PublicKey:      hex.EncodeToString(pub),
		SignedDigest:   hex.EncodeToString(digest[:]),
		Signature:      hex.EncodeToString(sig),
		To:             toSeq,
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
	_ = io.EOF // satisfy import (io used elsewhere only via go fmt — keep stable).
	return gzBuf.Bytes()
}
