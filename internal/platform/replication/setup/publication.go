package setup

import (
	"context"
	"fmt"
)

// ensurePublication은 publication이 없으면 생성합니다 (idempotent).
//
//  1. pg_publication에 같은 이름 존재 확인 → 있으면 skip
//  2. AllTables=true → `CREATE PUBLICATION <name> FOR ALL TABLES`
//     AllTables=false → `CREATE PUBLICATION <name> FOR TABLE <t1>, <t2>, ...`
//
// 본 함수는 publication tables 변경을 추적하지 않습니다 (Stage 3 범위 외). 운영자가
// 테이블 목록을 바꿀 때는 수동 `ALTER PUBLICATION ... ADD/DROP TABLE`이 필요합니다
// — 향후 별 epic에서 ALTER 동기화 자동화 고려.
func ensurePublication(ctx context.Context, exec Executor, spec PublicationSpec) error {
	if err := validateName(spec.Name); err != nil {
		return fmt.Errorf("ensurePublication: %w", err)
	}

	exists, err := exec.QueryBool(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)",
		spec.Name,
	)
	if err != nil {
		return fmt.Errorf("ensurePublication: check existence: %w", err)
	}
	if exists {
		return nil
	}

	var sqlStmt string
	if spec.AllTables {
		sqlStmt = fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", quoteIdent(spec.Name))
	} else {
		joined, jerr := joinQuotedTables(spec.Tables)
		if jerr != nil {
			return fmt.Errorf("ensurePublication: %w", jerr)
		}
		sqlStmt = fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", quoteIdent(spec.Name), joined)
	}

	if err := exec.Exec(ctx, sqlStmt); err != nil {
		return fmt.Errorf("ensurePublication: create: %w", err)
	}
	return nil
}

// DropPublication은 publication을 제거합니다 (운영/테스트 cleanup용).
//
// 본 round bootstrap에서는 호출하지 않습니다 — 별도 customer migration 또는
// 테스트 teardown에서 사용. IF EXISTS로 idempotent.
func DropPublication(ctx context.Context, exec Executor, name string) error {
	if err := validateName(name); err != nil {
		return fmt.Errorf("DropPublication: %w", err)
	}
	stmt := fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", quoteIdent(name))
	if err := exec.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("DropPublication: %w", err)
	}
	return nil
}
