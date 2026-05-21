package handlers

// replication.go — E-MR (Phase 8) Stage 2 — Multi-region HA manual failover + replica
// inventory HTTP 핸들러.
//
// 본 round endpoint 3종:
//
//	GET  /api/v1/replication/replicas            — 현 replicas 목록 + lag (last_replay_at vs now)
//	POST /api/v1/replication/heartbeat           — standby가 primary에 LSN + timestamp ping
//	POST /api/v1/replication/failover            — admin 권한 manual failover (region swap)
//	GET  /api/v1/replication/failovers           — Phase 10.A-4 region cutover 이력 timeline
//	GET  /api/v1/audit/head-sha                  — cross-region audit chain head 비교용
//
// design: docs/design/notes/multi-region-ha-design.md §4.3 failover 절차 + §4.4
// audit cross-region 정합성 + D-MR-3 수동 failover (Phase 8).
//
// failover 트랜잭션 절차 (단일 storage.Tx로 묶음):
//  1. ValidateFailoverRequest
//  2. SetRole(from_region, standby)
//  3. SetRole(to_region, primary)
//  4. RecordFailover (status=in-progress, completed_at=NULL)
//  5. audit.Service.Append(action="audit.replication.failover", actor=user)
//  6. LinkFailoverAudit(failoverID, auditEntryID, completedAt=now)
//
// 본 round 미진행 (Stage 3+):
//   - DNS hook 실 호출 (Route53 SDK)
//   - 자동 failover (heartbeat timeout 기반)
//   - PG pg_promote() 실행 (별 ops runbook)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/replication"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// --- response DTOs ---

type replicaResponse struct {
	Region          string `json:"region"`
	Role            string `json:"role"`
	Endpoint        string `json:"endpoint"`
	LastReplayLSN   string `json:"lastReplayLsn,omitempty"`
	LastReplayAt    string `json:"lastReplayAt,omitempty"`
	LastHeartbeatAt string `json:"lastHeartbeatAt,omitempty"`
	LagSeconds      int64  `json:"lagSeconds"`
	Enabled         bool   `json:"enabled"`
}

type listReplicasResponse struct {
	SelfRegion string            `json:"selfRegion"`
	SelfRole   string            `json:"selfRole"`
	Replicas   []replicaResponse `json:"replicas"`
}

type heartbeatRequest struct {
	Region        string `json:"region"`
	LastReplayLSN string `json:"lastReplayLsn"`
}

type failoverRequest struct {
	FromRegion string `json:"fromRegion"`
	ToRegion   string `json:"toRegion"`
	Reason     string `json:"reason"`
}

type failoverResponse struct {
	FailoverID  int64  `json:"failoverId"`
	FromRegion  string `json:"fromRegion"`
	ToRegion    string `json:"toRegion"`
	InitiatedAt string `json:"initiatedAt"`
	CompletedAt string `json:"completedAt,omitempty"`
}

type auditHeadSHAResponse struct {
	TenantID  string `json:"tenantId"`
	Seq       int64  `json:"seq"`
	HashHex   string `json:"hashHex"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type failoverHistoryItem struct {
	ID              int64  `json:"id"`
	FromRegion      string `json:"fromRegion"`
	ToRegion        string `json:"toRegion"`
	InitiatedByUser string `json:"initiatedByUser,omitempty"`
	InitiatedAt     string `json:"initiatedAt"`
	CompletedAt     string `json:"completedAt,omitempty"`
	Reason          string `json:"reason,omitempty"`
	AuditEntryID    int64  `json:"auditEntryId,omitempty"`
	Status          string `json:"status"` // "in-progress" | "completed"
}

type listFailoversResponse struct {
	Failovers []failoverHistoryItem `json:"failovers"`
}

// --- handlers ---

// ListReplicas는 모든 replica를 lag 계산과 함께 반환합니다.
//
// lag = now - last_replay_at (초). last_replay_at zero면 lag = -1 (unknown).
func (h *Handlers) ListReplicas(w http.ResponseWriter, r *http.Request) {
	if h.deps.Replication == nil {
		writeError(w, http.StatusServiceUnavailable, "replication not configured")
		return
	}
	var replicas []replication.Replica
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		rs, e := h.deps.Replication.ListReplicas(ctx, tx)
		if e != nil {
			return e
		}
		replicas = rs
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list replicas failed")
		return
	}

	now := h.deps.Clock.Now().UTC()
	out := listReplicasResponse{
		SelfRegion: h.deps.ReplicationConfig.Region,
		SelfRole:   string(h.deps.ReplicationConfig.Role),
		Replicas:   make([]replicaResponse, 0, len(replicas)),
	}
	for _, rep := range replicas {
		lag := int64(-1)
		if !rep.LastReplayAt.IsZero() {
			diff := now.Sub(rep.LastReplayAt.UTC())
			if diff < 0 {
				diff = 0
			}
			lag = int64(diff.Seconds())
		}
		item := replicaResponse{
			Region:        rep.Region,
			Role:          string(rep.Role),
			Endpoint:      rep.Endpoint,
			LastReplayLSN: rep.LastReplayLSN,
			LagSeconds:    lag,
			Enabled:       rep.Enabled,
		}
		if !rep.LastReplayAt.IsZero() {
			item.LastReplayAt = rep.LastReplayAt.UTC().Format(time.RFC3339Nano)
		}
		if !rep.LastHeartbeatAt.IsZero() {
			item.LastHeartbeatAt = rep.LastHeartbeatAt.UTC().Format(time.RFC3339Nano)
		}
		out.Replicas = append(out.Replicas, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// ReplicationHeartbeat은 standby의 LSN + timestamp ping을 수신합니다.
//
// 본 endpoint는 인증 미적용 (standby 자기 자신이 primary에 호출 — middleware로 우회).
// 향후 stage에서 cross-region 공유 시크릿 인증 도입 예정.
func (h *Handlers) ReplicationHeartbeat(w http.ResponseWriter, r *http.Request) {
	if h.deps.Replication == nil {
		writeError(w, http.StatusServiceUnavailable, "replication not configured")
		return
	}
	var body heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body.Region = strings.TrimSpace(body.Region)
	if body.Region == "" {
		writeError(w, http.StatusBadRequest, "region is required")
		return
	}

	now := h.deps.Clock.Now().UTC()
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		return h.deps.Replication.UpdateHeartbeat(ctx, tx, replication.HeartbeatRequest{
			Region:        body.Region,
			LastReplayLSN: body.LastReplayLSN,
			Now:           now,
		})
	})
	if err != nil {
		if errors.Is(err, replication.ErrReplicaNotFound) {
			writeError(w, http.StatusNotFound, "replica not registered for region")
			return
		}
		writeError(w, http.StatusInternalServerError, "heartbeat update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "acceptedAt": now.Format(time.RFC3339Nano)})
}

// TriggerFailover는 manual failover (region swap)를 실행합니다.
//
// admin 권한 필수 — handlers.go Mount에서 RequirePermission(tenant_admin, admin) 게이트.
// 절차는 본 파일 doc comment 참조.
func (h *Handlers) TriggerFailover(w http.ResponseWriter, r *http.Request) {
	if h.deps.Replication == nil {
		writeError(w, http.StatusServiceUnavailable, "replication not configured")
		return
	}
	if h.deps.Audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit service not configured")
		return
	}
	var body failoverRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body.FromRegion = strings.TrimSpace(body.FromRegion)
	body.ToRegion = strings.TrimSpace(body.ToRegion)

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no claims in context")
		return
	}

	now := h.deps.Clock.Now().UTC()
	fReq := replication.FailoverRequest{
		FromRegion:      body.FromRegion,
		ToRegion:        body.ToRegion,
		InitiatedByUser: claims.Subject,
		Reason:          body.Reason,
		Now:             now,
	}
	if vErr := replication.ValidateFailoverRequest(fReq); vErr != nil {
		writeError(w, http.StatusBadRequest, vErr.Error())
		return
	}

	var resultRow replication.Failover
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		// 1. role swap (둘 다 같은 Tx).
		if e := h.deps.Replication.SetRole(ctx, tx, fReq.FromRegion, replication.RoleStandby); e != nil {
			return fmt.Errorf("demote from_region: %w", e)
		}
		if e := h.deps.Replication.SetRole(ctx, tx, fReq.ToRegion, replication.RolePrimary); e != nil {
			return fmt.Errorf("promote to_region: %w", e)
		}

		// 2. failover 이력 INSERT.
		row, e := h.deps.Replication.RecordFailover(ctx, tx, fReq)
		if e != nil {
			return fmt.Errorf("record failover: %w", e)
		}

		// 3. audit emit (audit.replication.failover) — 같은 Tx로 묶어 P9 무결성 + chain link.
		payload := fmt.Sprintf(
			`{"fromRegion":%q,"toRegion":%q,"initiatedByUser":%q,"reason":%q,"failoverId":%d}`,
			fReq.FromRegion, fReq.ToRegion, fReq.InitiatedByUser, fReq.Reason, row.ID)
		entry, e := h.deps.Audit.Append(ctx, tx, audit.AppendRequest{
			TenantID: tx.TenantID(),
			Actor:    audit.Actor{Type: audit.ActorUser, ID: claims.Subject},
			Action:   "audit.replication.failover",
			Target:   audit.Target{Type: "region", ID: fReq.ToRegion},
			Payload:  []byte(payload),
			Outcome:  audit.OutcomeSuccess,
		})
		if e != nil {
			return fmt.Errorf("audit emit: %w", e)
		}

		// 4. failover row에 audit_entry_id + completed_at link.
		if e := h.deps.Replication.LinkFailoverAudit(ctx, tx, row.ID, entry.Seq, now); e != nil {
			return fmt.Errorf("link audit: %w", e)
		}
		row.AuditEntryID = entry.Seq
		row.CompletedAt = now
		resultRow = row
		return nil
	})
	if err != nil {
		if errors.Is(err, replication.ErrReplicaNotFound) {
			writeError(w, http.StatusNotFound, "replica not registered for region")
			return
		}
		writeError(w, errorStatusFor(err), "failover failed: "+err.Error())
		return
	}

	resp := failoverResponse{
		FailoverID:  resultRow.ID,
		FromRegion:  resultRow.FromRegion,
		ToRegion:    resultRow.ToRegion,
		InitiatedAt: resultRow.InitiatedAt.Format(time.RFC3339Nano),
	}
	if !resultRow.CompletedAt.IsZero() {
		resp.CompletedAt = resultRow.CompletedAt.Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetAuditHeadSHA는 cross-region audit chain head 비교용 응답을 제공합니다.
//
// D-MR-5 cross-region audit verify — 외부에서 region 간 head SHA를 비교하여
// consistency 검증. AccessClaims에서 tenant scope 추출 (AuthMiddleware 통과 후).
//
// 본 endpoint는 audit.go의 GetAuditHead와 동등 응답이지만, segment_count·entry_count
// 등 cross-region 정합성 분석을 위한 메타도 함께 노출하기 위해 별 endpoint로 분리.
// 본 round Stage 2 — head SHA + seq만 제공. segment_count·entry_count는 Stage 6
// (cross-region audit witness fold-in) 시 추가.
func (h *Handlers) GetAuditHeadSHA(w http.ResponseWriter, r *http.Request) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	var head audit.ChainHead
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		hd, e := h.deps.Audit.Head(ctx, tx, tenantID)
		if e != nil {
			return e
		}
		head = hd
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "read audit head failed")
		return
	}

	resp := auditHeadSHAResponse{
		TenantID: string(head.TenantID),
		Seq:      head.Seq,
		HashHex:  hexEncode32(head.Hash),
	}
	if !head.UpdatedAt.IsZero() {
		resp.UpdatedAt = head.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListFailovers는 Phase 10.A-4 — region cutover history endpoint입니다.
//
// admin 권한 필수(인프라 토폴로지 정보 — 운영자 외 노출 금지).
// query param `limit`(default 50, max 200) — initiated_at DESC 정렬.
// status는 completed_at NULL 여부로 도출: NULL → "in-progress", 그 외 → "completed".
func (h *Handlers) ListFailovers(w http.ResponseWriter, r *http.Request) {
	if h.deps.Replication == nil {
		writeError(w, http.StatusServiceUnavailable, "replication not configured")
		return
	}
	limit := 50
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	var rows []replication.Failover
	err := h.deps.Storage.Bootstrap(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		rs, e := h.deps.Replication.ListFailovers(ctx, tx, limit)
		if e != nil {
			return e
		}
		rows = rs
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failovers failed")
		return
	}

	items := make([]failoverHistoryItem, 0, len(rows))
	for _, f := range rows {
		status := "in-progress"
		if !f.CompletedAt.IsZero() {
			status = "completed"
		}
		item := failoverHistoryItem{
			ID:              f.ID,
			FromRegion:      f.FromRegion,
			ToRegion:        f.ToRegion,
			InitiatedByUser: f.InitiatedByUser,
			InitiatedAt:     f.InitiatedAt.UTC().Format(time.RFC3339Nano),
			Reason:          f.Reason,
			AuditEntryID:    f.AuditEntryID,
			Status:          status,
		}
		if !f.CompletedAt.IsZero() {
			item.CompletedAt = f.CompletedAt.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, listFailoversResponse{Failovers: items})
}

// hexEncode32는 audit.Hash([32]byte)를 64자 hex로 직렬화합니다.
// audit.go의 hex.EncodeToString(head.Hash[:])과 동등 — 별 함수로 두는 이유는
// 본 파일의 import 의존을 줄이기 위함 (audit.go가 이미 encoding/hex import).
func hexEncode32(h audit.Hash) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(h)*2)
	for i, b := range h {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0F]
	}
	return string(out)
}
