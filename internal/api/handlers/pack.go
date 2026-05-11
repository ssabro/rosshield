package handlers

// pack.go — GET /api/v1/packs 핸들러 (E12 Stage 3).
//
// 호출자 tenant pack + systemTenant("system") cross-tenant 공유 pack 합쳐 반환.
// systemTenant pack은 §4.2 명시(cross-tenant 공유 자산) — built-in seed pack(E12 Stage 2)
// 또는 운영자가 명시적으로 system tenant에 install한 자산.
//
// 결과는 packKey 알파벳 정렬. checks는 미포함(메타만, scans.tsx Select 드롭다운 용도).

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// systemTenantID는 cross-tenant 공유 pack 소속 tenant입니다.
//
// bootstrap이 cfg.SystemTenantID로 override 가능하지만, handler 측에서는 default "system"
// 으로 충분 — bootstrap default와 일관(§4.2).
const systemTenantID storage.TenantID = "system"

type packResponse struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenantId"`
	PackKey       string `json:"packKey"`
	Name          string `json:"name"`
	Vendor        string `json:"vendor"`
	Version       string `json:"version"`
	Description   string `json:"description,omitempty"`
	SchemaVersion int    `json:"schemaVersion"`
	SignerKeyID   string `json:"signerKeyId,omitempty"`
	InstalledAt   string `json:"installedAt"`
	IsBuiltin     bool   `json:"isBuiltin"`
}

type listPacksResponse struct {
	Packs []packResponse `json:"packs"`
}

func (h *Handlers) ListPacks(w http.ResponseWriter, r *http.Request) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Benchmark == nil {
		writeError(w, http.StatusServiceUnavailable, "benchmark service not configured")
		return
	}

	combined := make(map[string]packResponse, 16)

	// 1) systemTenant pack (cross-tenant 공유) — 호출자 tenant와 다르면 별도 Tx.
	if err := h.collectPacks(r.Context(), systemTenantID, combined, true); err != nil {
		writeError(w, errorStatusFor(err), "list system packs failed")
		return
	}

	// 2) 호출자 tenant pack (systemTenant와 같으면 1)에서 이미 처리, isBuiltin 가르기는 같은 키로 덮어씀)
	if tenantID != systemTenantID {
		if err := h.collectPacks(r.Context(), tenantID, combined, false); err != nil {
			writeError(w, errorStatusFor(err), "list tenant packs failed")
			return
		}
	}

	// 결정성: packKey 알파벳 정렬
	keys := make([]string, 0, len(combined))
	for k := range combined {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := listPacksResponse{Packs: make([]packResponse, 0, len(keys))}
	for _, k := range keys {
		out.Packs = append(out.Packs, combined[k])
	}
	writeJSON(w, http.StatusOK, out)
}

// collectPacks는 한 tenant의 ListPacks를 호출해 combined map에 추가합니다.
//
// systemTenant 호출은 별 Tx — handler ctx의 TenantID는 호출자 tenant이므로
// systemTenant 데이터 접근은 명시적 WithTenantID로 ctx 전환 필요.
func (h *Handlers) collectPacks(ctx context.Context, tenantID storage.TenantID,
	out map[string]packResponse, isBuiltin bool) error {
	tenantCtx := storage.WithTenantID(ctx, tenantID)
	return h.deps.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		packs, err := h.deps.Benchmark.ListPacks(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		for _, p := range packs {
			out[p.PackKey] = toPackResponse(p, isBuiltin)
		}
		return nil
	})
}

// packDetailResponse는 GET /api/v1/packs/{packKey} 응답 본문입니다.
//
// ListPacks는 메타만, Detail은 checks 포함 — 별 endpoint(N+1 회피).
type packDetailResponse struct {
	packResponse
	Checks []checkResponse `json:"checks"`
}

type checkResponse struct {
	ID          string `json:"id"`
	CheckID     string `json:"checkId"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Description string `json:"description,omitempty"`
}

// GetPack은 GET /api/v1/packs/{packKey} 핸들러입니다.
//
// systemTenant 우선 조회 → 호출자 tenant fallback. 둘 다 없으면 404.
// IsBuiltin은 적중한 tenant scope으로 결정.
func (h *Handlers) GetPack(w http.ResponseWriter, r *http.Request, packKey string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Benchmark == nil {
		writeError(w, http.StatusServiceUnavailable, "benchmark service not configured")
		return
	}
	if packKey == "" {
		writeError(w, http.StatusBadRequest, "packKey required")
		return
	}

	// 1) systemTenant 시도 (built-in)
	pack, isBuiltin, err := h.fetchPackByKey(r.Context(), systemTenantID, packKey)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		writeError(w, errorStatusFor(err), "get system pack failed")
		return
	}
	if errors.Is(err, storage.ErrNotFound) && tenantID != systemTenantID {
		// 2) 호출자 tenant 시도
		pack, isBuiltin, err = h.fetchPackByKey(r.Context(), tenantID, packKey)
		if err != nil {
			writeError(w, errorStatusFor(err), "get pack failed")
			return
		}
	}
	_ = isBuiltin // fetchPackByKey가 isBuiltin도 결정 (systemTenant scope 여부)

	out := packDetailResponse{
		packResponse: toPackResponse(pack, isBuiltin),
		Checks:       toCheckResponses(pack.Checks),
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) fetchPackByKey(ctx context.Context, tenantID storage.TenantID, packKey string) (benchmark.Pack, bool, error) {
	tenantCtx := storage.WithTenantID(ctx, tenantID)
	var pack benchmark.Pack
	err := h.deps.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		p, e := h.deps.Benchmark.GetPackByKey(ctx, tx, tenantID, packKey)
		if e != nil {
			return e
		}
		pack = p
		return nil
	})
	return pack, tenantID == systemTenantID, err
}

func toCheckResponses(checks []benchmark.Check) []checkResponse {
	out := make([]checkResponse, 0, len(checks))
	for _, c := range checks {
		out = append(out, checkResponse{
			ID:          c.ID,
			CheckID:     c.CheckID,
			Title:       c.Title,
			Severity:    string(c.Severity),
			Description: c.Description,
		})
	}
	// 결정성: CheckID 알파벳 정렬
	sort.Slice(out, func(i, j int) bool { return out[i].CheckID < out[j].CheckID })
	return out
}

func toPackResponse(p benchmark.Pack, isBuiltin bool) packResponse {
	return packResponse{
		ID:            p.ID,
		TenantID:      string(p.TenantID),
		PackKey:       p.PackKey,
		Name:          p.Name,
		Vendor:        p.Vendor,
		Version:       p.Version,
		Description:   p.Description,
		SchemaVersion: p.SchemaVersion,
		SignerKeyID:   p.SignerKeyID,
		InstalledAt:   p.InstalledAt.UTC().Format("2006-01-02T15:04:05Z"),
		IsBuiltin:     isBuiltin,
	}
}
