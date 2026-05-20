package setup

import (
	"context"
	"fmt"
	"sort"
)

// syncPublicationTables는 운영 중 PublicationSpec.Tables가 바뀐 경우 기존
// publication에 누락된 테이블을 ADD하고 잉여 테이블을 DROP합니다 (E-MR Stage 3
// 후속). 첫 생성은 ensurePublication이 처리하므로, 본 helper는 publication이
// 이미 존재할 때만 호출됩니다.
//
// 동작:
//  1. AllTables=true → FOR ALL TABLES는 신규 테이블 자동 포함이므로 no-op.
//  2. AllTables=false → pg_publication_tables에서 현재 테이블 목록 조회 →
//     spec.Tables와 set diff →
//     - 누락: ALTER PUBLICATION <name> ADD TABLE <t1>, <t2>, ...
//     - 잉여: ALTER PUBLICATION <name> DROP TABLE <t1>, <t2>, ...
//
// PG 13+에서 ALTER PUBLICATION ADD/DROP TABLE 지원 (multi-region-ha-design §5).
//
// 순서·중복 무관 — set 기반 diff.
func syncPublicationTables(ctx context.Context, exec Executor, spec PublicationSpec) error {
	if spec.AllTables {
		return nil
	}
	if err := validateName(spec.Name); err != nil {
		return fmt.Errorf("syncPublicationTables: %w", err)
	}

	current, err := exec.QueryStrings(ctx,
		"SELECT tablename FROM pg_publication_tables WHERE pubname = $1",
		spec.Name,
	)
	if err != nil {
		return fmt.Errorf("syncPublicationTables: query current: %w", err)
	}

	toAdd, toDrop := diffTables(current, spec.Tables)

	if len(toAdd) > 0 {
		if err := alterPublicationTables(ctx, exec, spec.Name, "ADD", toAdd); err != nil {
			return err
		}
	}
	if len(toDrop) > 0 {
		if err := alterPublicationTables(ctx, exec, spec.Name, "DROP", toDrop); err != nil {
			return err
		}
	}
	return nil
}

// diffTables는 두 set의 차집합을 계산합니다.
//
// 반환:
//   - toAdd: desired에는 있고 current에는 없는 테이블 (정렬됨, 결정론적)
//   - toDrop: current에는 있고 desired에는 없는 테이블 (정렬됨)
func diffTables(current, desired []string) (toAdd, toDrop []string) {
	curSet := make(map[string]struct{}, len(current))
	for _, t := range current {
		curSet[t] = struct{}{}
	}
	desSet := make(map[string]struct{}, len(desired))
	for _, t := range desired {
		desSet[t] = struct{}{}
	}
	for t := range desSet {
		if _, ok := curSet[t]; !ok {
			toAdd = append(toAdd, t)
		}
	}
	for t := range curSet {
		if _, ok := desSet[t]; !ok {
			toDrop = append(toDrop, t)
		}
	}
	sort.Strings(toAdd)
	sort.Strings(toDrop)
	return toAdd, toDrop
}

// alterPublicationTables는 ADD/DROP TABLE을 한 번에 묶어 실행합니다.
//
// op는 "ADD" 또는 "DROP"만 허용 — 호출자 책임으로 단정.
func alterPublicationTables(ctx context.Context, exec Executor, pubName, op string, tables []string) error {
	joined, err := joinQuotedTables(tables)
	if err != nil {
		return fmt.Errorf("alterPublicationTables: %w", err)
	}
	stmt := fmt.Sprintf("ALTER PUBLICATION %s %s TABLE %s",
		quoteIdent(pubName), op, joined,
	)
	if err := exec.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("alterPublicationTables %s: %w", op, err)
	}
	return nil
}
