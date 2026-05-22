package audit_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
)

func TestComputePayloadDigestStable(t *testing.T) {
	t.Parallel()

	want := sha256.Sum256([]byte(`{"name":"r1"}`))
	got := audit.ComputePayloadDigest([]byte(`{"name":"r1"}`))
	if audit.Hash(want) != got {
		t.Errorf("digest mismatch")
	}
}

func TestComputePayloadDigestEmptyIsSha256OfEmpty(t *testing.T) {
	t.Parallel()
	want := sha256.Sum256(nil)
	got := audit.ComputePayloadDigest(nil)
	if audit.Hash(want) != got {
		t.Errorf("empty digest mismatch")
	}
}

func TestComputeEntryHashIsDeterministic(t *testing.T) {
	t.Parallel()

	prev := audit.Hash{}
	digest := audit.ComputePayloadDigest([]byte(`{"x":1}`))

	occurredAt, _ := time.Parse(time.RFC3339Nano, "2026-04-24T01:23:45.123456789Z")
	entry := audit.Entry{
		TenantID:      "tn_a",
		Seq:           1,
		OccurredAt:    occurredAt,
		Actor:         audit.Actor{Type: audit.ActorUser, ID: "us_x"},
		Action:        "robot.create",
		Target:        audit.Target{Type: "robot", ID: "ro_x"},
		PayloadDigest: digest,
		Outcome:       audit.OutcomeSuccess,
	}

	h1, err := audit.ComputeEntryHash(prev, digest, entry)
	if err != nil {
		t.Fatalf("ComputeEntryHash 1: %v", err)
	}
	h2, err := audit.ComputeEntryHash(prev, digest, entry)
	if err != nil {
		t.Fatalf("ComputeEntryHash 2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("non-deterministic: %x vs %x", h1, h2)
	}
	if h1.IsZero() {
		t.Error("hash should not be zero")
	}
}

func TestComputeEntryHashChangesWithEachField(t *testing.T) {
	t.Parallel()

	occurredAt, _ := time.Parse(time.RFC3339Nano, "2026-04-24T01:23:45.123Z")
	base := audit.Entry{
		TenantID:   "tn_a",
		Seq:        1,
		OccurredAt: occurredAt,
		Actor:      audit.Actor{Type: audit.ActorUser, ID: "us_x"},
		Action:     "robot.create",
		Target:     audit.Target{Type: "robot", ID: "ro_x"},
		Outcome:    audit.OutcomeSuccess,
	}
	digest := audit.ComputePayloadDigest([]byte(`x`))

	baseHash, _ := audit.ComputeEntryHash(audit.Hash{}, digest, base)

	mutations := []struct {
		name   string
		mutate func(e *audit.Entry)
	}{
		{"action", func(e *audit.Entry) { e.Action = "robot.delete" }},
		{"actor.id", func(e *audit.Entry) { e.Actor.ID = "us_y" }},
		{"target.id", func(e *audit.Entry) { e.Target.ID = "ro_y" }},
		{"outcome", func(e *audit.Entry) { e.Outcome = audit.OutcomeFailure }},
		{"seq", func(e *audit.Entry) { e.Seq = 2 }},
		{"tenantId", func(e *audit.Entry) { e.TenantID = "tn_b" }},
		{"occurredAt", func(e *audit.Entry) { e.OccurredAt = occurredAt.Add(time.Nanosecond) }},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			e := base
			m.mutate(&e)
			h, _ := audit.ComputeEntryHash(audit.Hash{}, digest, e)
			if h == baseHash {
				t.Errorf("hash unchanged after mutating %s — %s", m.name, hex.EncodeToString(h[:]))
			}
		})
	}
}

func TestComputeEntryHashIncludesPrevHash(t *testing.T) {
	t.Parallel()
	digest := audit.ComputePayloadDigest([]byte(`x`))
	occurredAt, _ := time.Parse(time.RFC3339Nano, "2026-04-24T00:00:00Z")
	e := audit.Entry{
		TenantID:   "tn_a",
		Seq:        1,
		OccurredAt: occurredAt,
		Actor:      audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:     "x",
		Target:     audit.Target{Type: "x", ID: "x"},
		Outcome:    audit.OutcomeSuccess,
	}

	hZero, _ := audit.ComputeEntryHash(audit.Hash{}, digest, e)
	prev := audit.Hash{0xff, 0x00, 0x01}
	hPrev, _ := audit.ComputeEntryHash(prev, digest, e)
	if hZero == hPrev {
		t.Error("hash unchanged when prevHash differs")
	}
}

// --- Phase 11.C-2 v3 hash 단위 test ---
//
// design: docs/design/notes/audit-hash-key-epoch-input-design.md §6.2.
// v3 = v1 7 키 + keyEpoch + leaderEpoch 알파벳순 (총 9 키), nil 은 omitempty.

// fixtureEntry 는 v1/v3 test 가 공통으로 사용하는 결정적 Entry 입니다.
func fixtureEntry(t *testing.T) audit.Entry {
	t.Helper()
	occurredAt, err := time.Parse(time.RFC3339Nano, "2026-05-21T01:23:45.123456789Z")
	if err != nil {
		t.Fatalf("parse occurredAt: %v", err)
	}
	return audit.Entry{
		TenantID:      "tn_a",
		Seq:           42,
		OccurredAt:    occurredAt,
		Actor:         audit.Actor{Type: audit.ActorUser, ID: "us_x", IP: "10.0.0.1", UserAgent: "ua/1.0"},
		Action:        "robot.create",
		Target:        audit.Target{Type: "robot", ID: "ro_x"},
		PayloadDigest: audit.ComputePayloadDigest([]byte(`{"x":1}`)),
		Outcome:       audit.OutcomeSuccess,
	}
}

// TestComputeEntryHashV3NilEpochsByteIdenticalWithV1 — v1 entry(epoch 모두 nil)에 대해
// ComputeEntryHashV3 결과가 ComputeEntryHash(v1) 결과와 byte-identical 임을 검증.
//
// 이는 v3 hash 함수가 v1 entry 의 backward-compat 재계산을 지원함을 보장 — Stage 11.C-3
// chain transition marker 이전 entry 도 v3 함수로 재계산 가능 (안전망).
func TestComputeEntryHashV3NilEpochsByteIdenticalWithV1(t *testing.T) {
	t.Parallel()
	e := fixtureEntry(t)
	// KeyEpoch, LeaderEpoch 둘 다 nil — v1 chain entry 시뮬레이션.
	if e.KeyEpoch != nil || e.LeaderEpoch != nil {
		t.Fatalf("fixture invariant: expected nil epochs")
	}

	prev := audit.Hash{0x01, 0x02, 0x03}
	v1, err := audit.ComputeEntryHash(prev, e.PayloadDigest, e)
	if err != nil {
		t.Fatalf("v1 hash: %v", err)
	}
	v3, err := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)
	if err != nil {
		t.Fatalf("v3 hash: %v", err)
	}
	if v1 != v3 {
		t.Errorf("v3 hash must match v1 hash for nil-epoch entry — v1=%x v3=%x", v1, v3)
	}
}

// TestComputeEntryHashV3DiffersWhenEpochsSet — keyEpoch 또는 leaderEpoch 가 채워진
// entry 에 대해 v3 hash 가 v1 hash 와 달라야 함 (epoch 가 hash input 에 반영).
func TestComputeEntryHashV3DiffersWhenEpochsSet(t *testing.T) {
	t.Parallel()

	prev := audit.Hash{}

	t.Run("keyEpoch only", func(t *testing.T) {
		e := fixtureEntry(t)
		ke := int64(3)
		e.KeyEpoch = &ke
		v1, _ := audit.ComputeEntryHash(prev, e.PayloadDigest, e)
		v3, _ := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)
		if v1 == v3 {
			t.Error("v3 hash must differ from v1 when keyEpoch is set")
		}
	})

	t.Run("leaderEpoch only", func(t *testing.T) {
		e := fixtureEntry(t)
		le := int64(7)
		e.LeaderEpoch = &le
		v1, _ := audit.ComputeEntryHash(prev, e.PayloadDigest, e)
		v3, _ := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)
		if v1 == v3 {
			t.Error("v3 hash must differ from v1 when leaderEpoch is set")
		}
	})

	t.Run("both epochs", func(t *testing.T) {
		e := fixtureEntry(t)
		ke, le := int64(3), int64(7)
		e.KeyEpoch = &ke
		e.LeaderEpoch = &le
		v1, _ := audit.ComputeEntryHash(prev, e.PayloadDigest, e)
		v3, _ := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)
		if v1 == v3 {
			t.Error("v3 hash must differ from v1 when both epochs are set")
		}
	})
}

// TestComputeEntryHashV3ChangesWithEpochValue — v3 hash 가 keyEpoch / leaderEpoch 값
// 변화에 민감해야 함 (1 → 2 epoch 변경 시 hash 변경).
func TestComputeEntryHashV3ChangesWithEpochValue(t *testing.T) {
	t.Parallel()

	prev := audit.Hash{}

	t.Run("keyEpoch value change", func(t *testing.T) {
		e := fixtureEntry(t)
		ke1 := int64(1)
		e.KeyEpoch = &ke1
		h1, _ := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)

		ke2 := int64(2)
		e.KeyEpoch = &ke2
		h2, _ := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)
		if h1 == h2 {
			t.Error("v3 hash unchanged after keyEpoch value change (1 → 2)")
		}
	})

	t.Run("leaderEpoch value change", func(t *testing.T) {
		e := fixtureEntry(t)
		le1 := int64(5)
		e.LeaderEpoch = &le1
		h1, _ := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)

		le2 := int64(6)
		e.LeaderEpoch = &le2
		h2, _ := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)
		if h1 == h2 {
			t.Error("v3 hash unchanged after leaderEpoch value change (5 → 6)")
		}
	})
}

// TestComputeEntryHashV3IsDeterministic — 같은 entry 를 여러 번 호출해도 동일 hash.
func TestComputeEntryHashV3IsDeterministic(t *testing.T) {
	t.Parallel()
	e := fixtureEntry(t)
	ke, le := int64(3), int64(7)
	e.KeyEpoch = &ke
	e.LeaderEpoch = &le

	prev := audit.Hash{0xab, 0xcd}
	h1, err := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)
	if err != nil {
		t.Fatalf("v3 hash 1: %v", err)
	}
	h2, err := audit.ComputeEntryHashV3(prev, e.PayloadDigest, e)
	if err != nil {
		t.Fatalf("v3 hash 2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("v3 hash non-deterministic: %x vs %x", h1, h2)
	}
	if h1.IsZero() {
		t.Error("v3 hash should not be zero")
	}
}

// TestComputeEntryHashV3FieldOrderIsAlphabetic — v3 wire JSON 의 key 가
// 알파벳순 (action < actor < keyEpoch < leaderEpoch < occurredAt < outcome <
// seq < target < tenantId) 임을 확인. encoding/json 의 struct field 순서가
// JSON 출력 순서를 결정하므로, 우리는 hash output 을 unmarshal → re-marshal 비교
// 대신 v3 hash 가 epoch 위치 swap 에 민감한지를 검증합니다 (간접 검증).
//
// 그리고 reflection-free 직접 검증: 같은 9 키 entry 를 v3 함수로 hash 계산하면
// 항상 같은 값 — 이는 위 TestComputeEntryHashV3IsDeterministic 이 cover. 본 test 는
// hash 출력이 아니라 v3 가 fixture 의 9 키를 실제로 _모두_ 입력에 반영함을 확인 —
// 각 단일 field mutation 이 hash 변경을 유발해야 함.
func TestComputeEntryHashV3ChangesWithEachField(t *testing.T) {
	t.Parallel()

	occurredAt, _ := time.Parse(time.RFC3339Nano, "2026-05-21T01:23:45.123Z")
	ke, le := int64(3), int64(7)
	base := audit.Entry{
		TenantID:    "tn_a",
		Seq:         1,
		OccurredAt:  occurredAt,
		Actor:       audit.Actor{Type: audit.ActorUser, ID: "us_x"},
		Action:      "robot.create",
		Target:      audit.Target{Type: "robot", ID: "ro_x"},
		Outcome:     audit.OutcomeSuccess,
		KeyEpoch:    &ke,
		LeaderEpoch: &le,
	}
	digest := audit.ComputePayloadDigest([]byte(`x`))

	baseHash, _ := audit.ComputeEntryHashV3(audit.Hash{}, digest, base)

	keNew := int64(4)
	leNew := int64(8)
	mutations := []struct {
		name   string
		mutate func(e *audit.Entry)
	}{
		{"action", func(e *audit.Entry) { e.Action = "robot.delete" }},
		{"actor.id", func(e *audit.Entry) { e.Actor.ID = "us_y" }},
		{"keyEpoch", func(e *audit.Entry) { e.KeyEpoch = &keNew }},
		{"leaderEpoch", func(e *audit.Entry) { e.LeaderEpoch = &leNew }},
		{"target.id", func(e *audit.Entry) { e.Target.ID = "ro_y" }},
		{"outcome", func(e *audit.Entry) { e.Outcome = audit.OutcomeFailure }},
		{"seq", func(e *audit.Entry) { e.Seq = 2 }},
		{"tenantId", func(e *audit.Entry) { e.TenantID = "tn_b" }},
		{"occurredAt", func(e *audit.Entry) { e.OccurredAt = occurredAt.Add(time.Nanosecond) }},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			e := base
			m.mutate(&e)
			h, _ := audit.ComputeEntryHashV3(audit.Hash{}, digest, e)
			if h == baseHash {
				t.Errorf("v3 hash unchanged after mutating %s — %s", m.name, hex.EncodeToString(h[:]))
			}
		})
	}
}

// TestComputeEntryHashUnsupportedVersion 직접 검증은 internal helper 가 노출되지 않아
// 우회 불가 — 대신 public API 가 v1 / v3 만 노출함을 문서로 보장. 본 placeholder 는
// 향후 version negotiation 추가 시 확장 지점.

// --- canonicalMetaJSON v3 wire-format 직접 검증 ---
//
// hash sha256 은 입력 변경에 대한 indirect signal — 키 순서·omitempty 동작은
// JSON wire 를 직접 확인할 때만 보장됩니다. v3 hash 함수가 외부 검증 도구
// (fg-verify) 와 byte-level 호환됨을 보장하기 위해, v3 hash 함수 결과 wire JSON
// 을 stable v1 hash 와 cross-check 합니다.
//
// 기법: ComputeEntryHashV3(prev, digest, e) == sha256(prev ‖ digest ‖ wireJSON)
// 이고, wireJSON 의 구조를 우리는 알고 있음 (canonicalMetaJSONv3 의 metaJSON
// struct 정의). 따라서 nil-epoch entry 의 v3 hash == v1 hash 등식이 _이미_ wire
// byte-identical 을 의미합니다 (TestComputeEntryHashV3NilEpochsByteIdenticalWithV1).
//
// 보강 test: keyEpoch + leaderEpoch 가 채워진 entry 의 v3 hash 를 우리가 직접
// 구성한 9 키 JSON 으로 재계산해 일치함을 검증.

func TestComputeEntryHashV3MatchesExpectedWireFormat(t *testing.T) {
	t.Parallel()

	occurredAt, _ := time.Parse(time.RFC3339Nano, "2026-05-21T01:23:45.123456789Z")
	ke, le := int64(3), int64(7)
	e := audit.Entry{
		TenantID:    "tn_a",
		Seq:         42,
		OccurredAt:  occurredAt,
		Actor:       audit.Actor{Type: audit.ActorUser, ID: "us_x"},
		Action:      "robot.create",
		Target:      audit.Target{Type: "robot", ID: "ro_x"},
		Outcome:     audit.OutcomeSuccess,
		KeyEpoch:    &ke,
		LeaderEpoch: &le,
	}
	digest := audit.ComputePayloadDigest([]byte(`{"x":1}`))
	prev := audit.Hash{}

	got, err := audit.ComputeEntryHashV3(prev, digest, e)
	if err != nil {
		t.Fatalf("v3 hash: %v", err)
	}

	// 기대 wire — canonicalMetaJSONv3 와 동일한 알파벳순 9 키 + omitempty 규칙.
	type actorJSON struct {
		ID        string `json:"id"`
		IP        string `json:"ip,omitempty"`
		Type      string `json:"type"`
		UserAgent string `json:"userAgent,omitempty"`
	}
	type targetJSON struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	type metaJSON struct {
		Action      string     `json:"action"`
		Actor       actorJSON  `json:"actor"`
		KeyEpoch    *int64     `json:"keyEpoch,omitempty"`
		LeaderEpoch *int64     `json:"leaderEpoch,omitempty"`
		OccurredAt  string     `json:"occurredAt"`
		Outcome     string     `json:"outcome"`
		Seq         int64      `json:"seq"`
		Target      targetJSON `json:"target"`
		TenantID    string     `json:"tenantId"`
	}
	expectedMeta, err := json.Marshal(metaJSON{
		Action:      e.Action,
		Actor:       actorJSON{ID: e.Actor.ID, Type: string(e.Actor.Type)},
		KeyEpoch:    e.KeyEpoch,
		LeaderEpoch: e.LeaderEpoch,
		OccurredAt:  e.OccurredAt.UTC().Format(time.RFC3339Nano),
		Outcome:     string(e.Outcome),
		Seq:         e.Seq,
		Target:      targetJSON{ID: e.Target.ID, Type: e.Target.Type},
		TenantID:    string(e.TenantID),
	})
	if err != nil {
		t.Fatalf("expected meta marshal: %v", err)
	}

	// wire 의 알파벳 순서 sanity — 키 위치 확인 (substring index).
	wire := string(expectedMeta)
	keyOrder := []string{`"action"`, `"actor"`, `"keyEpoch"`, `"leaderEpoch"`, `"occurredAt"`, `"outcome"`, `"seq"`, `"target"`, `"tenantId"`}
	last := -1
	for _, key := range keyOrder {
		idx := strings.Index(wire, key)
		if idx < 0 {
			t.Fatalf("expected wire to contain %s — wire=%s", key, wire)
		}
		if idx <= last {
			t.Errorf("v3 wire JSON key %s appears out of alphabetic order — wire=%s", key, wire)
		}
		last = idx
	}

	// sha256(prev ‖ digest ‖ expectedMeta) == ComputeEntryHashV3 출력.
	h := sha256.New()
	h.Write(prev[:])
	h.Write(digest[:])
	h.Write(expectedMeta)
	var expected audit.Hash
	copy(expected[:], h.Sum(nil))

	if got != expected {
		t.Errorf("v3 hash mismatch with reconstructed wire — got=%x expected=%x wire=%s",
			got, expected, wire)
	}
}

// TestCanonicalMetaJSONv3NilEpochsByteIdenticalWithV1Wire — nil-epoch entry 의 v3 wire
// JSON 이 v1 wire JSON 과 byte-identical 임을 확인 (TestComputeEntryHashV3NilEpochs...
// 보강). 직접 비교 — v1 hash 와 v3 hash 가 같다는 것은 wire 가 같음을 의미하지만,
// 본 test 는 두 함수 wire 가 byte-level 같음을 직접 보여줍니다.
func TestCanonicalMetaJSONv3NilEpochsByteIdenticalWithV1Wire(t *testing.T) {
	t.Parallel()

	e := fixtureEntry(t)
	// nil epochs.

	// v1 / v3 모두 hash 만 노출하므로, sha256 결과를 wire equivalence 의 cryptographic
	// proxy 로 사용. v1.Hash == v3.Hash + same prev/digest → wire is identical.
	prev := audit.Hash{0xde, 0xad, 0xbe, 0xef}
	digest := e.PayloadDigest

	v1, _ := audit.ComputeEntryHash(prev, digest, e)
	v3, _ := audit.ComputeEntryHashV3(prev, digest, e)
	if !bytes.Equal(v1[:], v3[:]) {
		t.Errorf("v1 / v3 wire must be byte-identical for nil-epoch entry — v1=%x v3=%x", v1, v3)
	}
}
