package postgres

import "testing"

// rebind 는 unexported 이므로 internal 테스트에서 직접 검증합니다.
func TestRebind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no placeholder", "SELECT 1", "SELECT 1"},
		{"single", "SELECT * FROM t WHERE id = ?", "SELECT * FROM t WHERE id = $1"},
		{"multiple", "INSERT INTO t (a, b, c) VALUES (?, ?, ?)", "INSERT INTO t (a, b, c) VALUES ($1, $2, $3)"},
		{"preserve in single quotes", "SELECT '?' FROM t WHERE id = ?", "SELECT '?' FROM t WHERE id = $1"},
		{"preserve in double quotes", `SELECT "?col" FROM t WHERE id = ?`, `SELECT "?col" FROM t WHERE id = $1`},
		{"many sequential", "?,?,?,?,?,?,?,?,?,?", "$1,$2,$3,$4,$5,$6,$7,$8,$9,$10"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := rebind(tc.in)
			if got != tc.want {
				t.Errorf("rebind(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestItoa(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{123, "123"},
		{99999, "99999"},
	}
	for _, tc := range cases {
		got := itoa(tc.in)
		if got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
