package tenant_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/tenant"
)

// E3.T2 본체.
func TestUserArgon2PasswordRoundtrip(t *testing.T) {
	t.Parallel()

	password := "correct horse battery staple"
	encoded, err := tenant.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Errorf("encoded prefix mismatch: %q", encoded)
	}
	// 6 segments separated by `$`.
	if got := strings.Count(encoded, "$"); got != 5 {
		t.Errorf("expected 5 `$` separators (6 parts), got %d in %q", got, encoded)
	}

	if err := tenant.VerifyPassword(password, encoded); err != nil {
		t.Errorf("VerifyPassword same password: %v", err)
	}
}

func TestUserArgon2RejectsWrongPassword(t *testing.T) {
	t.Parallel()

	encoded, err := tenant.HashPassword("the right one")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	err = tenant.VerifyPassword("the wrong one", encoded)
	if !errors.Is(err, tenant.ErrInvalidPasswordCheck) {
		t.Errorf("err = %v, want ErrInvalidPasswordCheck", err)
	}
}

func TestUserArgon2DistinctSaltsForSamePassword(t *testing.T) {
	t.Parallel()

	password := "same password twice"
	a, _ := tenant.HashPassword(password)
	b, _ := tenant.HashPassword(password)
	if a == b {
		t.Error("two encodings of same password are equal — salt not random")
	}

	// 둘 다 같은 raw 비밀번호로 검증돼야 함.
	if err := tenant.VerifyPassword(password, a); err != nil {
		t.Errorf("a verify: %v", err)
	}
	if err := tenant.VerifyPassword(password, b); err != nil {
		t.Errorf("b verify: %v", err)
	}
}

func TestUserArgon2RejectsMalformedHash(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":         "",
		"not argon":     "$bcrypt$...",
		"truncated":     "$argon2id$v=19$m=65536,t=3,p=1$abc",
		"wrong version": "$argon2id$v=99$m=65536,t=3,p=1$YWFhYWFhYWFhYWFhYWFhYQ$YWFhYQ",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			err := tenant.VerifyPassword("anything", encoded)
			if err == nil {
				t.Error("expected error for malformed hash")
			}
		})
	}
}

func TestUserArgon2EmptyPasswordRejected(t *testing.T) {
	t.Parallel()

	if _, err := tenant.HashPassword(""); !errors.Is(err, tenant.ErrEmptyPassword) {
		t.Errorf("HashPassword(\"\") err = %v, want ErrEmptyPassword", err)
	}
}
