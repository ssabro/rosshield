package sqliterepo

// repo_hash_version.go — Phase 11.C-3+4 audit chain hash version transition + v3 bundle.
//
// 본 파일은 Repo 의 hash version (v1 / v3) 분기 캐시 + ExportV3 wire 직렬화를 담당합니다.
// repo.go 가 이미 700+ line 이라 CLAUDE.md 정책(파일 ≤ 400 줄 권장 / ≤ 800 줄 최대) 일관
// 으로 본 epic 의 신규 표면을 별 파일로 격리.

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// SetHashVersionTransitionSeq 는 Phase 11.C-3 transition marker seq 를 캐시합니다.
//
// bootstrap.ensureHashVersionTransition 이 emit 직후 또는 기존 transition entry 발견 직후
// 1회 호출. seq <= 0 은 noop (unset 유지 — 모든 Append v1 hash 분기).
//
// 동시성: bootstrap 단일 thread 에서 1회 호출. atomic.Store 로 후속 Append goroutine 들이
// race-free 읽음.
//
// 본 메서드 호출 후 새 Append 는 entry.Seq > seq 일 때 v3 hash, 그 외 v1 hash 사용.
func (r *Repo) SetHashVersionTransitionSeq(seq int64) {
	if seq <= 0 {
		return
	}
	r.transitionSeq.Store(seq)
}

// HashVersionTransitionSeq 는 캐시된 transition seq 를 반환합니다 (0 = unset).
//
// 외부 도구 (예: ExportV3) 가 signature line 의 `_hashVersionTransitionAt` 필드에 노출할
// 때 사용. atomic.Load 라 race-free.
func (r *Repo) HashVersionTransitionSeq() int64 {
	return r.transitionSeq.Load()
}

// FindHashVersionTransitionSeq 는 audit_entries 에서 transition marker entry 의 seq 를 조회합니다.
//
// audit_entries.action = audit.ActionHashVersionChanged 인 row 가 있으면 그 seq 반환 (ok=true).
// 없으면 (0, false, nil). storage 에러는 (0, false, err).
//
// bootstrap.ensureHashVersionTransition 이 호출 — idempotent emit 판정.
//
// tenant scope: tx.TenantID() 와 tenantID 일치 검증 (호출자 보장). audit_chain 은 tenant
// 별 분리이므로 시스템 tenant 가 transition emit + 다른 tenant 와 무관.
func (r *Repo) FindHashVersionTransitionSeq(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (int64, bool, error) {
	row := tx.QueryRow(ctx, `
SELECT seq FROM audit_entries
 WHERE tenant_id = ? AND action = ?
 ORDER BY seq ASC
 LIMIT 1`,
		string(tenantID), audit.ActionHashVersionChanged)

	var seq int64
	err := row.Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("audit: find hash version transition: %w", err)
	}
	return seq, true, nil
}

// recomputeHashForSeq 는 Verify / 외부 검증 시뮬레이션 시 entry.Seq 에 맞춰 v1 / v3 hash
// 함수를 선택 호출합니다.
//
// transitionSeq == 0 (unset) → v1.
// transitionSeq > 0 + e.Seq > transitionSeq → v3.
// 그 외 → v1.
func (r *Repo) recomputeHashForSeq(e audit.Entry) (audit.Hash, error) {
	transition := r.transitionSeq.Load()
	if transition > 0 && e.Seq > transition {
		return audit.ComputeEntryHashV3(e.PrevHash, e.PayloadDigest, e)
	}
	return audit.ComputeEntryHash(e.PrevHash, e.PayloadDigest, e)
}

// ExportV3 는 Phase 11.C-4 v3 bundle 을 내보냅니다.
//
// v2 super-set:
//   - signature line `_bundleVersion: "v3"`.
//   - 각 entry line 이 LeaderEpoch (둘 다 nil 이면 omit) 노출 (MarshalEntryLineV3).
//   - signature line `_hashVersionTransitionAt` 가 bundle 범위 안의 transition entry seq 노출
//     (transitionSeq 미 cache + bundle 범위 안 entry 도 없으면 0/omit).
//   - keyRepo 가 비-nil 이면 chainKeyEpochs 포함 (v2 와 동일).
//
// chain hash 자체는 INSERT 시 결정 — Repo.transitionSeq 분기로 entry 별 v1 / v3 hash 가
// 이미 저장됨. ExportV3 는 wire 직렬화만 v3 형식.
func (r *Repo) ExportV3(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64, sgn signer.Signer, keyRepo audit.ChainKeyRepository) (io.ReadCloser, error) {
	if sgn == nil {
		return nil, fmt.Errorf("audit: ExportV3 requires non-nil signer")
	}
	if fromSeq <= 0 {
		fromSeq = 1
	}
	if toSeq <= 0 || toSeq < fromSeq {
		head, err := readHead(ctx, tx, tenantID)
		if err != nil {
			return nil, err
		}
		toSeq = head.Seq
	}

	entriesBuf, err := r.collectV3EntryLines(ctx, tx, tenantID, fromSeq, toSeq)
	if err != nil {
		return nil, err
	}

	// chainKeyEpochs lookup (v2 super-set).
	var chainKeys []audit.ExportChainKeyEpoch
	if keyRepo != nil {
		epochs, err := keyRepo.ListChainKeyEpochs(ctx, tx, tenantID)
		if err != nil {
			return nil, fmt.Errorf("audit: exportV3 list epochs: %w", err)
		}
		chainKeys = audit.ToExportChainKeyEpochs(epochs)
	}

	// hash version transition seq — bundle 범위 안에 transition entry 가 포함되면 그 seq.
	transitionAt := r.resolveTransitionAt(ctx, tx, tenantID, fromSeq, toSeq)

	digest := audit.SignedDigest(entriesBuf.Bytes())
	sig, keyID, err := sgn.Sign(digest[:])
	if err != nil {
		return nil, fmt.Errorf("audit: sign exportV3: %w", err)
	}

	sigLine, err := audit.MarshalSignatureLine(audit.ExportSignatureLine{
		BundleVersion:           audit.BundleVersionV3,
		ChainKeyEpochs:          chainKeys,
		From:                    fromSeq,
		HashVersionTransitionAt: transitionAt,
		KeyID:                   keyID,
		PublicKey:               hex.EncodeToString(sgn.PublicKey()),
		SignedDigest:            hex.EncodeToString(digest[:]),
		Signature:               hex.EncodeToString(sig),
		To:                      toSeq,
	})
	if err != nil {
		return nil, err
	}

	return writeV3GzipStream(entriesBuf.Bytes(), sigLine)
}

// collectV3EntryLines 는 audit_entries 의 row 들을 v3 entry line NDJSON byte buffer 로
// 누적합니다. 비-fromSeq~toSeq 또는 빈 chain 시 빈 buffer.
func (r *Repo) collectV3EntryLines(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64) (bytes.Buffer, error) {
	var buf bytes.Buffer
	if toSeq < fromSeq {
		return buf, nil
	}
	rows, err := tx.Query(ctx, `
SELECT seq, occurred_at, actor_type, actor_id, actor_ip, actor_ua,
       action, target_type, target_id,
       payload_digest, outcome, error_code, error_message,
       prev_hash, hash, leader_epoch, key_epoch
  FROM audit_entries
 WHERE tenant_id = ? AND seq BETWEEN ? AND ?
 ORDER BY seq ASC`,
		string(tenantID), fromSeq, toSeq)
	if err != nil {
		return buf, fmt.Errorf("audit: exportV3 query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		e, err := scanEntry(rows, tenantID)
		if err != nil {
			return buf, err
		}
		line, err := audit.MarshalEntryLineV3(e)
		if err != nil {
			return buf, fmt.Errorf("audit: marshal v3 entry seq=%d: %w", e.Seq, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		return buf, fmt.Errorf("audit: exportV3 rows: %w", err)
	}
	return buf, nil
}

// resolveTransitionAt 는 transitionSeq cache 또는 DB 조회로 transition seq 를 결정합니다.
// bundle 범위 (fromSeq~toSeq) 밖이면 0 (omit) — 외부 도구는 bundle 안의 seq 만 활용.
func (r *Repo) resolveTransitionAt(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64) int64 {
	transitionAt := r.transitionSeq.Load()
	if transitionAt == 0 {
		if seq, ok, err := r.FindHashVersionTransitionSeq(ctx, tx, tenantID); err == nil && ok {
			transitionAt = seq
		}
	}
	if transitionAt < fromSeq || transitionAt > toSeq {
		return 0
	}
	return transitionAt
}

// writeV3GzipStream 은 entries buffer + signature line + newline 을 gzip 으로 wrap 합니다.
func writeV3GzipStream(entries, sigLine []byte) (io.ReadCloser, error) {
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(entries); err != nil {
		return nil, fmt.Errorf("audit: gzip entries: %w", err)
	}
	if _, err := gz.Write(sigLine); err != nil {
		return nil, fmt.Errorf("audit: gzip signature: %w", err)
	}
	if _, err := gz.Write([]byte{'\n'}); err != nil {
		return nil, fmt.Errorf("audit: gzip newline: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("audit: gzip close: %w", err)
	}
	return io.NopCloser(&gzBuf), nil
}
