package sso_test

// sso_test.go — 도메인 헬퍼·sentinel·LoginAttempt 만료 검증 단위 테스트 (E20-A).

import (
	"errors"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
)

func TestIsValidType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   sso.Type
		want bool
	}{
		{sso.TypeOIDC, true},
		{sso.TypeSAML, true},
		{sso.Type(""), false},
		{sso.Type("ldap"), false},
		{sso.Type("OIDC"), false}, // case-sensitive
	}
	for _, tc := range cases {
		got := sso.IsValidType(tc.in)
		if got != tc.want {
			t.Errorf("IsValidType(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLoginAttemptIsExpired(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		exp  time.Time
		want bool
	}{
		{"future", now.Add(time.Minute), false},
		{"now exact", now, true},
		{"past", now.Add(-time.Minute), true},
		{"zero", time.Time{}, false}, // zero ExpiresAt = no expiry
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := sso.LoginAttempt{ExpiresAt: tc.exp}
			if got := a.IsExpired(now); got != tc.want {
				t.Errorf("IsExpired = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoginAttemptIsCompleted(t *testing.T) {
	t.Parallel()
	a := sso.LoginAttempt{}
	if a.IsCompleted() {
		t.Errorf("nil CompletedAt should not be completed")
	}
	now := time.Now().UTC()
	a.CompletedAt = &now
	if !a.IsCompleted() {
		t.Errorf("non-nil CompletedAt should be completed")
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()
	errs := []error{
		sso.ErrProviderNotFound,
		sso.ErrProviderDisabled,
		sso.ErrProviderNameExists,
		sso.ErrInvalidState,
		sso.ErrStateExpired,
		sso.ErrIdPMismatch,
		sso.ErrUnsupportedType,
		sso.ErrEmptyName,
		sso.ErrEmptyConfig,
		sso.ErrEmptyState,
		sso.ErrEmptySubject,
	}
	for i, a := range errs {
		for j, b := range errs {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("errs[%d] should not Is errs[%d]: %v vs %v", i, j, a, b)
			}
		}
	}
}

func TestDefaultAttemptTTLReasonable(t *testing.T) {
	t.Parallel()
	if sso.DefaultAttemptTTL < time.Minute {
		t.Errorf("DefaultAttemptTTL = %v, too short for IdP round-trip", sso.DefaultAttemptTTL)
	}
	if sso.DefaultAttemptTTL > 30*time.Minute {
		t.Errorf("DefaultAttemptTTL = %v, too long — replay window risk", sso.DefaultAttemptTTL)
	}
}
