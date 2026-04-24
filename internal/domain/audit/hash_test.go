package audit_test

import (
	"crypto/sha256"
	"encoding/hex"
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
