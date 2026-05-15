// E22-B — 0002~0019 PostgreSQL 마이그레이션 변환 검증.
//
// 본 테스트는 실제 PG 인스턴스 없이 정적 sanity check 만 수행합니다.
//   - 모든 0002~0019 파일이 embed 되었는지
//   - up/down 짝이 맞는지 (동일 시퀀스로 .up.sql / .down.sql 둘 다 존재)
//   - 각 SQL 파일이 PG 변환 마커(JSONB / TIMESTAMPTZ / BYTEA)를 적어도 하나 이상 보유 (NO-OP 제외)
//   - 괄호 짝, 세미콜론 종결, SQLite 잔재 토큰(WITHOUT ROWID, RAISE(ABORT, PRAGMA, AUTOINCREMENT, BLOB) 부재
//
// 실 PG 인스턴스 통합 검증은 후속 stage E22-E (testcontainers-go).
package postgres_test

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

// expectedSequences 는 본 stage 에서 작성·embed 되어야 하는 마이그레이션 시퀀스입니다.
// 0001 은 E22-A 에서 작성됨, 0002~0019 는 E22-B 에서 변환, 0020·0021 는 E22-C/D 에서 추가,
// 0022 는 E25 (HA leader-election fence token) 에서 추가.
var expectedSequences = []string{
	"0001_tenant_init",
	"0002_audit",
	"0003_tenant_user",
	"0004_roles",
	"0005_api_keys",
	"0006_auth_refresh",
	"0007_packs",
	"0008_fleets",
	"0009_credentials",
	"0010_robots",
	"0011_scan",
	"0012_evidence",
	"0013_reports",
	"0014_insights",
	"0015_compliance",
	"0016_framework_reports",
	"0017_mapping_suggestions",
	"0018_advisor",
	"0019_webhooks",
	"0020_sso",
	"0021_invitations",
	"0022_leader_epoch",
	"0023_audit_leader_epoch",
	"0024_pg_native_hotpath",
	"0025_scan_active_fleet_unique",
	"0026_scan_severity_aggregate",
	"0027_robot_host_keys",
	"0028_user_roles_scope",
}

// noopSequences 는 E22-E 폐기 — TestPGConversionMarkersPresent 함께 폐기됐으므로 미사용.
// Phase 4 PG-native repo 분리 시 함께 부활.

func TestAllMigrationFilesEmbedded(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(postgres.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	got := make(map[string]bool, len(entries))
	for _, e := range entries {
		got[e.Name()] = true
	}

	for _, seq := range expectedSequences {
		for _, suffix := range []string{".up.sql", ".down.sql"} {
			name := seq + suffix
			if !got[name] {
				t.Errorf("expected embedded migration: %s", name)
			}
		}
	}
}

func TestNoUnexpectedMigrationFiles(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(postgres.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	want := make(map[string]bool)
	for _, seq := range expectedSequences {
		want[seq+".up.sql"] = true
		want[seq+".down.sql"] = true
	}

	var unexpected []string
	for _, e := range entries {
		if !want[e.Name()] {
			unexpected = append(unexpected, e.Name())
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Errorf("unexpected migration files: %v", unexpected)
	}
}

func TestUpDownPairsExist(t *testing.T) {
	t.Parallel()

	for _, seq := range expectedSequences {
		seq := seq
		t.Run(seq, func(t *testing.T) {
			t.Parallel()
			for _, suffix := range []string{".up.sql", ".down.sql"} {
				path := "migrations/" + seq + suffix
				b, err := fs.ReadFile(postgres.MigrationsFS, path)
				if err != nil {
					t.Fatalf("ReadFile %s: %v", path, err)
				}
				if len(b) == 0 {
					t.Errorf("%s is empty", path)
				}
			}
		})
	}
}

func TestNoSQLiteRemnants(t *testing.T) {
	t.Parallel()

	// SQLite 전용 토큰이 PG 변환에 남아있으면 검출. 대소문자 무시.
	// 일부 토큰(BLOB)은 단어 경계로 검사 — JSONB 내 'B' 와 충돌 방지.
	forbidden := []struct {
		token  string
		reason string
	}{
		{"WITHOUT ROWID", "SQLite 전용"},
		{"RAISE(ABORT", "SQLite trigger 본문 — PL/pgSQL 로 변환 필요"},
		{"PRAGMA ", "SQLite 전용"},
		{"AUTOINCREMENT", "PG 는 BIGSERIAL/IDENTITY 사용"},
		{"+goose ", "golang-migrate 는 디렉티브 미사용"},
	}

	for _, seq := range expectedSequences {
		seq := seq
		t.Run(seq, func(t *testing.T) {
			t.Parallel()
			for _, suffix := range []string{".up.sql", ".down.sql"} {
				path := "migrations/" + seq + suffix
				b, err := fs.ReadFile(postgres.MigrationsFS, path)
				if err != nil {
					t.Fatalf("ReadFile %s: %v", path, err)
				}
				// 주석 제거 후 SQL 본문에서만 검사 — 변환 메모(주석)에 'BLOB → BYTEA' 같은
				// 설명 텍스트가 있어도 false positive 발생하지 않게.
				stripped := stripCommentsAndSpace(string(b))
				upper := strings.ToUpper(stripped)
				for _, f := range forbidden {
					if strings.Contains(upper, strings.ToUpper(f.token)) {
						t.Errorf("%s contains forbidden token %q (%s)", path, f.token, f.reason)
					}
				}
				// BLOB 단어 경계 검사 (BYTEA 로 치환되어야 함).
				if containsWordCI(upper, "BLOB") {
					t.Errorf("%s contains BLOB type — 변환 시 BYTEA 로 교체해야 함", path)
				}
			}
		})
	}
}

// TestPGConversionMarkersPresent는 E22-E 시점에 폐기됐습니다.
//
// 폐기 사유: PG 마이그레이션이 sqliterepo와의 type 호환을 위해 sqlite-equivalent schema
// (TEXT/INTEGER/SMALLINT)로 단순화됨. BOOLEAN/JSONB/TIMESTAMPTZ marker 검사는 더 이상
// 의미 없음. binary fields만 BYTEA로 보존. 도메인 repo 분리(E22-F 후속)에서 PG-native
// type 도입 시 본 테스트와 noopSequences 맵을 함께 부활.

func TestParenthesisBalance(t *testing.T) {
	t.Parallel()

	for _, seq := range expectedSequences {
		seq := seq
		t.Run(seq, func(t *testing.T) {
			t.Parallel()
			for _, suffix := range []string{".up.sql", ".down.sql"} {
				path := "migrations/" + seq + suffix
				b, err := fs.ReadFile(postgres.MigrationsFS, path)
				if err != nil {
					t.Fatalf("ReadFile %s: %v", path, err)
				}
				if err := checkParenBalance(string(b)); err != nil {
					t.Errorf("%s: %v", path, err)
				}
			}
		})
	}
}

func TestStatementsTerminatedBySemicolon(t *testing.T) {
	t.Parallel()

	for _, seq := range expectedSequences {
		seq := seq
		t.Run(seq, func(t *testing.T) {
			t.Parallel()
			for _, suffix := range []string{".up.sql", ".down.sql"} {
				path := "migrations/" + seq + suffix
				b, err := fs.ReadFile(postgres.MigrationsFS, path)
				if err != nil {
					t.Fatalf("ReadFile %s: %v", path, err)
				}
				// 주석·공백 제거 후 마지막 비공백 문자가 ';' 인지 확인.
				stripped := stripCommentsAndSpace(string(b))
				if stripped == "" {
					continue // 전부 주석인 파일 — 본 stage 에는 없음
				}
				last := stripped[len(stripped)-1]
				if last != ';' {
					t.Errorf("%s does not end with ';' (last char=%q)", path, last)
				}
			}
		})
	}
}

// --- helpers ---

// containsWordCI 는 대소문자 무시 단어 경계 검사 (영숫자가 인접하면 매치 안 함).
func containsWordCI(haystack, word string) bool {
	hu := strings.ToUpper(haystack)
	wu := strings.ToUpper(word)
	idx := 0
	for {
		i := strings.Index(hu[idx:], wu)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(wu)
		if isBoundary(hu, start-1) && isBoundary(hu, end) {
			return true
		}
		idx = start + 1
	}
}

func isBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
		return false
	}
	return true
}

// checkParenBalance 는 따옴표·달러 인용·라인 주석을 인지하면서 ( ) 균형을 검사합니다.
// PL/pgSQL $$...$$ 본문은 별도 처리 — 안의 괄호는 무시.
func checkParenBalance(src string) error {
	depth := 0
	i := 0
	n := len(src)
	for i < n {
		c := src[i]
		// 라인 주석
		if c == '-' && i+1 < n && src[i+1] == '-' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		// 작은 따옴표 문자열
		if c == '\'' {
			i++
			for i < n {
				if src[i] == '\'' {
					if i+1 < n && src[i+1] == '\'' {
						i += 2 // escaped quote
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		// 큰 따옴표 식별자
		if c == '"' {
			i++
			for i < n && src[i] != '"' {
				i++
			}
			if i < n {
				i++
			}
			continue
		}
		// 달러 인용 ($$...$$ 또는 $tag$...$tag$)
		if c == '$' {
			// 태그 추출
			j := i + 1
			for j < n && (src[j] == '_' || isAlnum(src[j])) {
				j++
			}
			if j < n && src[j] == '$' {
				tag := src[i : j+1]
				// 닫는 태그 찾기
				closeIdx := strings.Index(src[j+1:], tag)
				if closeIdx < 0 {
					return fmt.Errorf("unclosed dollar-quoted block starting at offset %d (tag=%q)", i, tag)
				}
				i = j + 1 + closeIdx + len(tag)
				continue
			}
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return fmt.Errorf("unbalanced ')' at offset %d", i)
			}
		}
		i++
	}
	if depth != 0 {
		return fmt.Errorf("unbalanced parens — final depth=%d", depth)
	}
	return nil
}

func isAlnum(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// stripCommentsAndSpace 는 -- 라인 주석을 제거하고 trailing whitespace 를 잘라냅니다.
func stripCommentsAndSpace(src string) string {
	var sb strings.Builder
	i := 0
	n := len(src)
	for i < n {
		if i+1 < n && src[i] == '-' && src[i+1] == '-' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		// 따옴표 안은 그대로 보존
		if src[i] == '\'' {
			sb.WriteByte(src[i])
			i++
			for i < n {
				sb.WriteByte(src[i])
				if src[i] == '\'' {
					if i+1 < n && src[i+1] == '\'' {
						i++
						sb.WriteByte(src[i])
						i++
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		sb.WriteByte(src[i])
		i++
	}
	return strings.TrimRight(sb.String(), " \t\r\n")
}
