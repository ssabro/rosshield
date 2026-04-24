// Package sqliterepo는 audit.Service의 SQLite 어댑터입니다.
//
// Append는 단일 트랜잭션 안에서:
//  1. SELECT audit_chain_heads → prev seq·hash (없으면 genesis = seq 0, hash zeros)
//  2. INSERT audit_entries (seq = prev.seq + 1, hash 계산)
//  3. INSERT or REPLACE audit_chain_heads (UPSERT)
//
// 동일 Tx에 묶이므로 도메인 변경(예: robots INSERT)과 audit가 원자적입니다 (P5·P9).
package sqliterepo

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock clock.Clock
}

// Repo는 audit.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// Append는 audit.Service.Append 구현입니다.
func (r *Repo) Append(ctx context.Context, tx storage.Tx, req audit.AppendRequest) (audit.Entry, error) {
	if err := validateAppend(req); err != nil {
		return audit.Entry{}, err
	}
	if tx.TenantID() != "" && tx.TenantID() != req.TenantID {
		return audit.Entry{}, audit.ErrTenantMismatch
	}

	head, err := readHead(ctx, tx, req.TenantID)
	if err != nil {
		return audit.Entry{}, err
	}

	now := r.deps.Clock.Now().UTC()
	entry := audit.Entry{
		TenantID:      req.TenantID,
		Seq:           head.Seq + 1,
		OccurredAt:    now,
		Actor:         req.Actor,
		Action:        req.Action,
		Target:        req.Target,
		PayloadDigest: audit.ComputePayloadDigest(req.Payload),
		Outcome:       req.Outcome,
		Error:         req.Error,
		PrevHash:      head.Hash,
	}

	hash, err := audit.ComputeEntryHash(entry.PrevHash, entry.PayloadDigest, entry)
	if err != nil {
		return audit.Entry{}, err
	}
	entry.Hash = hash

	if err := insertEntry(ctx, tx, entry); err != nil {
		return audit.Entry{}, err
	}

	if err := upsertHead(ctx, tx, audit.ChainHead{
		TenantID:  entry.TenantID,
		Seq:       entry.Seq,
		Hash:      entry.Hash,
		UpdatedAt: now,
	}); err != nil {
		return audit.Entry{}, err
	}

	return entry, nil
}

// Head는 tenant의 현재 head를 반환합니다. 없으면 genesis(Seq=0, Hash=zero) 반환.
func (r *Repo) Head(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (audit.ChainHead, error) {
	return readHead(ctx, tx, tenantID)
}

// Verify는 fromSeq~toSeq 엔트리를 순차 SELECT하여 해시·prev_hash·seq 연속성을 재검증합니다.
//
// 검증 항목 (각 항목 위반 시 BreakAt 마킹 후 early return):
//  1. seq 연속성: 첫 entry.seq == max(fromSeq, 1), 그 다음 entry.seq == prior.seq + 1
//  2. prev_hash 연결: 첫 entry는 (fromSeq=1일 때) PrevHash=zero, 이외에는 prior.hash와 일치
//  3. hash 재계산: ComputeEntryHash(prevHash, payloadDigest, meta) == 저장된 hash
//
// fromSeq=1 부터 검증할 때만 genesis(PrevHash=zero) 검증이 일어납니다.
// 중간 구간(fromSeq>1)은 첫 entry의 PrevHash 값 자체는 검증하지 않고, 다음 entry부터의 연결을 검증합니다.
func (r *Repo) Verify(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64) (audit.VerifyResult, error) {
	if fromSeq <= 0 {
		fromSeq = 1
	}
	if toSeq <= 0 || toSeq < fromSeq {
		head, err := readHead(ctx, tx, tenantID)
		if err != nil {
			return audit.VerifyResult{}, err
		}
		toSeq = head.Seq
	}
	if toSeq < fromSeq {
		// head.Seq=0인 빈 체인 + fromSeq=1 → 검증할 게 없음 = 클린.
		return audit.VerifyResult{OK: true}, nil
	}

	rows, err := tx.Query(ctx, `
SELECT seq, occurred_at, actor_type, actor_id, actor_ip, actor_ua,
       action, target_type, target_id,
       payload_digest, outcome, error_code, error_message,
       prev_hash, hash
  FROM audit_entries
 WHERE tenant_id = ? AND seq BETWEEN ? AND ?
 ORDER BY seq ASC`,
		string(tenantID), fromSeq, toSeq)
	if err != nil {
		return audit.VerifyResult{}, fmt.Errorf("audit: verify query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		scanned   int64
		expectSeq = fromSeq
		priorHash audit.Hash // fromSeq=1이면 zero(genesis), 그 외엔 첫 row 받고 채움
		havePrior bool
	)

	for rows.Next() {
		e, err := scanEntry(rows, tenantID)
		if err != nil {
			return audit.VerifyResult{}, err
		}
		scanned++

		if e.Seq != expectSeq {
			return audit.VerifyResult{
				OK:             false,
				BreakAt:        expectSeq,
				Reason:         fmt.Sprintf("missing or out-of-order entry: expected seq %d, got %d", expectSeq, e.Seq),
				EntriesScanned: scanned,
			}, nil
		}

		// prev_hash 연결 검증.
		switch {
		case fromSeq == 1 && e.Seq == 1:
			if !e.PrevHash.IsZero() {
				return audit.VerifyResult{
					OK:             false,
					BreakAt:        e.Seq,
					Reason:         "genesis entry prev_hash is not zero",
					EntriesScanned: scanned,
				}, nil
			}
		case havePrior:
			if e.PrevHash != priorHash {
				return audit.VerifyResult{
					OK:             false,
					BreakAt:        e.Seq,
					Reason:         fmt.Sprintf("prev_hash mismatch at seq %d: chain broken", e.Seq),
					EntriesScanned: scanned,
				}, nil
			}
		}

		// hash 재계산 검증.
		expected, err := audit.ComputeEntryHash(e.PrevHash, e.PayloadDigest, e)
		if err != nil {
			return audit.VerifyResult{}, fmt.Errorf("audit: recompute hash at seq %d: %w", e.Seq, err)
		}
		if expected != e.Hash {
			return audit.VerifyResult{
				OK:             false,
				BreakAt:        e.Seq,
				Reason:         fmt.Sprintf("hash mismatch at seq %d: stored hash does not match recomputed", e.Seq),
				EntriesScanned: scanned,
			}, nil
		}

		priorHash = e.Hash
		havePrior = true
		expectSeq++
	}
	if err := rows.Err(); err != nil {
		return audit.VerifyResult{}, fmt.Errorf("audit: verify rows: %w", err)
	}

	// 요청한 toSeq까지 row가 부족하면 missing.
	if scanned < (toSeq - fromSeq + 1) {
		return audit.VerifyResult{
			OK:             false,
			BreakAt:        fromSeq + scanned,
			Reason:         fmt.Sprintf("missing entry at seq %d (have %d of %d)", fromSeq+scanned, scanned, toSeq-fromSeq+1),
			EntriesScanned: scanned,
		}, nil
	}

	return audit.VerifyResult{OK: true, EntriesScanned: scanned}, nil
}

// Export는 audit.Service.Export 구현입니다.
//
// 출력 스트림 (gzip):
//
//	<entry-line-1>\n
//	<entry-line-2>\n
//	...
//	<entry-line-N>\n
//	<signature-line>\n
//
// signature 라인은 모든 entry 라인(개행 포함)의 sha256을 Ed25519로 서명한 결과 + 공개키 + keyId.
// 외부 검증 도구는 entry 라인들을 읽어 sha256 재계산 → signer.Verify(publicKey, signature)로 무결성 확인.
func (r *Repo) Export(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64, sgn signer.Signer) (io.ReadCloser, error) {
	if sgn == nil {
		return nil, fmt.Errorf("audit: Export requires non-nil signer")
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

	// entry 라인들을 buffer에 누적 — 다음 sha256 + gzip 입력으로 사용.
	var entriesBuf bytes.Buffer

	if toSeq >= fromSeq {
		rows, err := tx.Query(ctx, `
SELECT seq, occurred_at, actor_type, actor_id, actor_ip, actor_ua,
       action, target_type, target_id,
       payload_digest, outcome, error_code, error_message,
       prev_hash, hash
  FROM audit_entries
 WHERE tenant_id = ? AND seq BETWEEN ? AND ?
 ORDER BY seq ASC`,
			string(tenantID), fromSeq, toSeq)
		if err != nil {
			return nil, fmt.Errorf("audit: export query: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			e, err := scanEntry(rows, tenantID)
			if err != nil {
				return nil, err
			}
			line, err := audit.MarshalEntryLine(e)
			if err != nil {
				return nil, fmt.Errorf("audit: marshal entry seq=%d: %w", e.Seq, err)
			}
			entriesBuf.Write(line)
			entriesBuf.WriteByte('\n')
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("audit: export rows: %w", err)
		}
	}

	// SignedDigest = sha256(모든 entry 라인 stream)
	digest := audit.SignedDigest(entriesBuf.Bytes())

	sig, keyID, err := sgn.Sign(digest[:])
	if err != nil {
		return nil, fmt.Errorf("audit: sign export: %w", err)
	}

	sigLine, err := audit.MarshalSignatureLine(audit.ExportSignatureLine{
		From:         fromSeq,
		KeyID:        keyID,
		PublicKey:    hex.EncodeToString(sgn.PublicKey()),
		SignedDigest: hex.EncodeToString(digest[:]),
		Signature:    hex.EncodeToString(sig),
		To:           toSeq,
	})
	if err != nil {
		return nil, err
	}

	// gzip 스트림 구성: entries + signature + 개행.
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(entriesBuf.Bytes()); err != nil {
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

// scanEntry는 audit_entries 한 row를 Entry로 변환합니다.
func scanEntry(rows interface {
	Scan(dest ...any) error
}, tenantID storage.TenantID) (audit.Entry, error) {
	var (
		seq                  int64
		occurredStr          string
		actorType, actorID   string
		actorIP, actorUA     sql.NullString
		action               string
		targetType, targetID string
		payloadDigest        []byte
		outcome              string
		errCode, errMessage  sql.NullString
		prevHash, hash       []byte
	)
	if err := rows.Scan(&seq, &occurredStr,
		&actorType, &actorID, &actorIP, &actorUA,
		&action, &targetType, &targetID,
		&payloadDigest, &outcome, &errCode, &errMessage,
		&prevHash, &hash); err != nil {
		return audit.Entry{}, fmt.Errorf("audit: scan entry: %w", err)
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, occurredStr)
	if err != nil {
		return audit.Entry{}, fmt.Errorf("audit: parse occurred_at seq=%d: %w", seq, err)
	}

	e := audit.Entry{
		TenantID:   tenantID,
		Seq:        seq,
		OccurredAt: occurredAt,
		Actor: audit.Actor{
			Type:      audit.ActorType(actorType),
			ID:        actorID,
			IP:        actorIP.String,
			UserAgent: actorUA.String,
		},
		Action:  action,
		Target:  audit.Target{Type: targetType, ID: targetID},
		Outcome: audit.Outcome(outcome),
	}
	if errCode.Valid || errMessage.Valid {
		e.Error = &audit.ErrorInfo{Code: errCode.String, Message: errMessage.String}
	}
	if len(payloadDigest) != audit.HashSize {
		return audit.Entry{}, fmt.Errorf("audit: payload_digest size = %d, want %d (seq=%d)", len(payloadDigest), audit.HashSize, seq)
	}
	if len(prevHash) != audit.HashSize {
		return audit.Entry{}, fmt.Errorf("audit: prev_hash size = %d, want %d (seq=%d)", len(prevHash), audit.HashSize, seq)
	}
	if len(hash) != audit.HashSize {
		return audit.Entry{}, fmt.Errorf("audit: hash size = %d, want %d (seq=%d)", len(hash), audit.HashSize, seq)
	}
	copy(e.PayloadDigest[:], payloadDigest)
	copy(e.PrevHash[:], prevHash)
	copy(e.Hash[:], hash)
	return e, nil
}

func readHead(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (audit.ChainHead, error) {
	row := tx.QueryRow(ctx,
		`SELECT seq, hash, updated_at FROM audit_chain_heads WHERE tenant_id = ?`,
		string(tenantID))

	var (
		seq        int64
		hashBytes  []byte
		updatedStr string
	)
	err := row.Scan(&seq, &hashBytes, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return audit.ChainHead{TenantID: tenantID}, nil
	}
	if err != nil {
		return audit.ChainHead{}, fmt.Errorf("audit: read head: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, updatedStr)
	if err != nil {
		return audit.ChainHead{}, fmt.Errorf("audit: parse head updated_at: %w", err)
	}

	out := audit.ChainHead{
		TenantID:  tenantID,
		Seq:       seq,
		UpdatedAt: updatedAt,
	}
	if len(hashBytes) != audit.HashSize {
		return audit.ChainHead{}, fmt.Errorf("audit: head hash size = %d, want %d", len(hashBytes), audit.HashSize)
	}
	copy(out.Hash[:], hashBytes)
	return out, nil
}

func insertEntry(ctx context.Context, tx storage.Tx, e audit.Entry) error {
	var (
		errCode    *string
		errMessage *string
		actorIP    *string
		actorUA    *string
	)
	if e.Error != nil {
		errCode = &e.Error.Code
		errMessage = &e.Error.Message
	}
	if e.Actor.IP != "" {
		actorIP = &e.Actor.IP
	}
	if e.Actor.UserAgent != "" {
		actorUA = &e.Actor.UserAgent
	}

	_, err := tx.Exec(ctx, `
INSERT INTO audit_entries (
    tenant_id, seq, occurred_at,
    actor_type, actor_id, actor_ip, actor_ua,
    action, target_type, target_id,
    payload_digest, outcome, error_code, error_message,
    prev_hash, hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(e.TenantID), e.Seq, e.OccurredAt.UTC().Format(time.RFC3339Nano),
		string(e.Actor.Type), e.Actor.ID, actorIP, actorUA,
		e.Action, e.Target.Type, e.Target.ID,
		e.PayloadDigest[:], string(e.Outcome), errCode, errMessage,
		e.PrevHash[:], e.Hash[:])
	if err != nil {
		return fmt.Errorf("audit: insert entry: %w", err)
	}
	return nil
}

func upsertHead(ctx context.Context, tx storage.Tx, h audit.ChainHead) error {
	_, err := tx.Exec(ctx, `
INSERT INTO audit_chain_heads (tenant_id, seq, hash, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(tenant_id) DO UPDATE SET
    seq = excluded.seq,
    hash = excluded.hash,
    updated_at = excluded.updated_at`,
		string(h.TenantID), h.Seq, h.Hash[:], h.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("audit: upsert head: %w", err)
	}
	return nil
}

func validateAppend(req audit.AppendRequest) error {
	if req.Action == "" {
		return audit.ErrEmptyAction
	}
	if req.Target.Type == "" || req.Target.ID == "" {
		return audit.ErrEmptyTarget
	}
	switch req.Actor.Type {
	case audit.ActorUser, audit.ActorAPI, audit.ActorSystem, audit.ActorAnonymous:
	default:
		return audit.ErrInvalidActor
	}
	switch req.Outcome {
	case audit.OutcomeSuccess, audit.OutcomeFailure, audit.OutcomePartial:
	default:
		return audit.ErrInvalidOutcome
	}
	if req.TenantID == "" {
		return storage.ErrTenantMissing
	}
	return nil
}
