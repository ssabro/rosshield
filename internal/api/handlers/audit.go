package handlers

// audit.go — GET /api/v1/audit/head 핸들러 (B1 — Web UI Audit 페이지 지원).
//
// audit.Service.Head는 tenant scope에서 ChainHead를 반환합니다.
// head가 없으면 (Seq=0, Hash=zero) genesis head로 본문에 노출 — Web UI는 이를
// "초기 상태"로 렌더링.
//
// VerifyAuditChain은 별 작업 — 도메인 layer가 fromSeq/toSeq 옵션을 받음. 본 Phase에서는
// gen.Unimplemented 그대로 두고 Audit 페이지에 "후속 구현" 안내.

import (
	"context"
	"encoding/hex"
	"net/http"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// auditHeadResponse는 GET /api/v1/audit/head 응답 본문입니다.
type auditHeadResponse struct {
	TenantID  string `json:"tenantId"`
	Seq       int64  `json:"seq"`
	HashHex   string `json:"hashHex"` // Hash를 16진수로 표현 (32B → 64자).
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// GetAuditHead는 GET /api/v1/audit/head 핸들러입니다.
//
// AuthMiddleware가 ctx에 TenantID를 주입한 상태에서만 호출됩니다.
func (h *Handlers) GetAuditHead(w http.ResponseWriter, r *http.Request) {
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

	resp := auditHeadResponse{
		TenantID: string(head.TenantID),
		Seq:      head.Seq,
		HashHex:  hex.EncodeToString(head.Hash[:]),
	}
	if !head.UpdatedAt.IsZero() {
		resp.UpdatedAt = head.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	writeJSON(w, http.StatusOK, resp)
}
