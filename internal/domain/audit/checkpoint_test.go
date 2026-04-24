package audit_test

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

func TestSerializeCheckpointPayloadFormat(t *testing.T) {
	t.Parallel()

	hash := audit.Hash{}
	for i := range hash {
		hash[i] = byte(i)
	}
	payload := audit.SerializeCheckpointPayload("tn_a", 42, hash)

	if len(payload) != audit.HashSize+8+len("tn_a") {
		t.Fatalf("payload length = %d, want %d", len(payload), audit.HashSize+8+len("tn_a"))
	}

	// hash[0..32] == hash bytes
	for i := 0; i < audit.HashSize; i++ {
		if payload[i] != byte(i) {
			t.Errorf("payload[%d] = %x, want %x", i, payload[i], byte(i))
		}
	}
	// payload[32..40] == BigEndian seq
	gotSeq := int64(binary.BigEndian.Uint64(payload[audit.HashSize : audit.HashSize+8]))
	if gotSeq != 42 {
		t.Errorf("seq = %d, want 42", gotSeq)
	}
	// 끝부분 == tenantId
	if string(payload[audit.HashSize+8:]) != "tn_a" {
		t.Errorf("tenantId tail = %q, want tn_a", string(payload[audit.HashSize+8:]))
	}
}

// E2.T8 — Scheduler 등록 통합 테스트.
// `@every 1s` 잡 등록 → 2.5초 대기 → audit_checkpoints에 적어도 1건 존재.
func TestRegisterCheckpointJobFiresAndPersists(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "ckpt.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System()})

	// entry 1개 만들어 checkpoint 가능 상태.
	const tn storage.TenantID = "tn_ckpt"
	ctx := storage.WithTenantID(context.Background(), tn)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.Append(ctx, tx, audit.AppendRequest{
			TenantID: tn,
			Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
			Action:   "tenant.created",
			Target:   audit.Target{Type: "tenant", ID: string(tn)},
			Payload:  []byte(`{}`),
			Outcome:  audit.OutcomeSuccess,
		})
		return e
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	sgn, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	sch := cronsched.New(cronsched.Deps{Logger: logger})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sch.Close(ctx)
	})

	if err := audit.RegisterCheckpointJob(sch, store, repo, logger,
		"audit-checkpoint", "@every 1s", tn, sgn); err != nil {
		t.Fatalf("RegisterCheckpointJob: %v", err)
	}

	// 발화 대기 — 첫 fire는 ~1s, 두 번째는 ~2s. 같은 head로 두 번째는 ErrCheckpointExists로 noop.
	deadline := time.Now().Add(3500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var found atomic.Bool
		_ = store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			cp, err := repo.LatestCheckpoint(ctx, tx, tn)
			if err == nil && cp.Seq == 1 {
				found.Store(true)
			}
			return nil
		})
		if found.Load() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("checkpoint not written within 3.5s — RegisterCheckpointJob did not fire")
}

// E2.T8 보조: 잘못된 인자 — 명시적 에러.
func TestRegisterCheckpointJobValidatesArgs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sch := cronsched.New(cronsched.Deps{Logger: logger})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = sch.Close(ctx)
	})
	sgn, _ := soft.New()
	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System()})

	// 빈 tenantID.
	err := audit.RegisterCheckpointJob(sch, nil, repo, logger, "j", "@every 1m", "", sgn)
	if err == nil {
		t.Error("expected error for nil store")
	}
}
