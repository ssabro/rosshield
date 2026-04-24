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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/clock"
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
