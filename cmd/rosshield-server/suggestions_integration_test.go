package main

// suggestions_integration_test.go — E17-B LLM Suggester 결선 검증.
//
// 실 LLM 어댑터(noop)를 거쳐 SuggestMappings 호출 시 ErrLLMSuggesterUnavailable로 매핑되는지
// 결선 흐름을 e2e로 검증.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func TestSuggestMappingsWithNoopLLMReturnsSentinel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		// LLMProvider 기본값(noop) — Suggester는 ErrLLMDisabled를 반환하고
		// llmmapper가 propagate, sqliterepo가 ErrLLMSuggesterUnavailable로 매핑.
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	// tenant 시드.
	var tenantID storage.TenantID
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		res, err := p.Tenant.Create(ctx, tx, tenant.CreateRequest{
			Name:             "Sug Test",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "admin@sug.local",
			AdminPassword:    "longpassword123",
			AdminDisplayName: "Sug Admin",
		})
		if err != nil {
			return err
		}
		tenantID = res.Tenant.ID
		return nil
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// SuggestMappings 호출 — noop이므로 sentinel 반환 기대.
	tCtx := storage.WithTenantID(context.Background(), tenantID)
	err = p.Storage.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := p.Compliance.SuggestMappings(ctx, tx, compliance.SuggestMappingsRequest{
			CheckCode:  "CIS-1.1.1.1",
			CheckTitle: "Test",
			Framework:  compliance.FrameworkISMSP,
		})
		return e
	})
	if !errors.Is(err, compliance.ErrLLMSuggesterUnavailable) {
		t.Errorf("err = %v, want ErrLLMSuggesterUnavailable (noop LLM 결선)", err)
	}
}
