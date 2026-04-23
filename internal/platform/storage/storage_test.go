package storage_test

import (
	"context"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

func TestTenantIDContextRoundtrip(t *testing.T) {
	t.Parallel()

	const want storage.TenantID = "tn_abc"
	ctx := storage.WithTenantID(context.Background(), want)

	if got := storage.TenantIDFromContext(ctx); got != want {
		t.Errorf("TenantIDFromContext = %q, want %q", got, want)
	}
}

func TestTenantIDFromContextEmptyByDefault(t *testing.T) {
	t.Parallel()

	if got := storage.TenantIDFromContext(context.Background()); got != "" {
		t.Errorf("TenantIDFromContext = %q, want empty", got)
	}
}
