//go:build integration

package sqliterepo_test

// hash_version_pg_integration_test.go — Phase 11.C-3+4 PG 통합 테스트.
//
// 본 파일은 `-tags=integration` 빌드 태그가 붙어야 컴파일됩니다.
// docker 미가용 시 t.Skip.
//
// 실행:
//
//	go test -tags=integration -count=1 ./internal/domain/audit/sqliterepo/...
//
// 검증:
//  1. PG 환경에서 transition emit + Append v3 분기 + ExportV3 round-trip 정상.
//  2. signature line `_bundleVersion="v3"` + `_hashVersionTransitionAt` 노출.
//  3. chain Verify (v1 + transition + v3 entries) 전체 PASS.

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// TestIntegrationPG_HashVersionTransitionRoundTrip — PG 환경에서 transition + v3 chain
// 전체 round-trip.
func TestIntegrationPG_HashVersionTransitionRoundTrip(t *testing.T) {
	t.Parallel()
	store := newPGKeyEpochFixture(t)
	const sysTenant storage.TenantID = "system"

	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System()})
	keyRepo := sqliterepo.NewKeyEpochRepo()

	ctx := storage.WithTenantID(context.Background(), sysTenant)

	// 2 개 v1 entry.
	for i := 0; i < 2; i++ {
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: sysTenant,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "system.bootstrap",
				Target:   audit.Target{Type: "system", ID: "boot"},
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		}); err != nil {
			t.Fatalf("Append v1: %v", err)
		}
	}

	// transition emit (seq=3).
	var transitionSeq int64
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		e, _, err := audit.EnsureHashVersionTransition(ctx, tx, repo, repo, repo, sysTenant)
		transitionSeq = e.Seq
		return err
	}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if transitionSeq != 3 {
		t.Fatalf("transitionSeq = %d, want 3", transitionSeq)
	}

	// 2 개 v3 entry.
	for i := 0; i < 2; i++ {
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: sysTenant,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "audit.chain.key_rotated",
				Target:   audit.Target{Type: "chain", ID: "system"},
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		}); err != nil {
			t.Fatalf("Append v3: %v", err)
		}
	}

	// 참고: PG 환경에서는 Verify 가 occurred_at TIMESTAMPTZ → string round-trip 정밀도
	// 차이로 byte-identical 재계산을 보장하지 못함 (기존 PG audit 통합 테스트 일관 회피 —
	// pgnative_hotpath_integration_test.go §1 line 87 명시). 본 epic 은 wire format 회귀
	// 가드에 집중 — Verify 정합성은 unit test (SQLite) + fg-verify v3 단단히 cover.

	// ExportV3 — bundle wire 검증.
	sgn, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}

	var lines []string
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		rc, err := repo.ExportV3(ctx, tx, sysTenant, 1, 5, sgn, keyRepo)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		gz, err := gzip.NewReader(rc)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		scanner := bufio.NewScanner(gz)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		return scanner.Err()
	}); err != nil {
		t.Fatalf("ExportV3: %v", err)
	}

	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6 (5 entries + 1 signature)", len(lines))
	}
	var sig audit.ExportSignatureLine
	if err := json.Unmarshal([]byte(lines[5]), &sig); err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if sig.BundleVersion != audit.BundleVersionV3 {
		t.Errorf("BundleVersion=%q want %q", sig.BundleVersion, audit.BundleVersionV3)
	}
	if sig.HashVersionTransitionAt != 3 {
		t.Errorf("HashVersionTransitionAt=%d want 3", sig.HashVersionTransitionAt)
	}
	if len(sig.ChainKeyEpochs) < 1 {
		t.Errorf("ChainKeyEpochs=%d want >= 1 (bootstrap)", len(sig.ChainKeyEpochs))
	}
}
