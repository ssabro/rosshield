package setup

import (
	"context"
	"fmt"
)

// ensureSubscription은 subscription이 없으면 생성합니다 (idempotent).
//
//  1. pg_subscription에 같은 이름 존재 확인 → 있으면 skip
//  2. `CREATE SUBSCRIPTION <name> CONNECTION '<conn>' PUBLICATION <pub>
//     WITH (copy_data = <bool>, create_slot = true, enabled = true)`
//
// 주의:
//   - conn string은 single quote escape 후 SQL literal로 삽입. password 같은
//     민감 정보는 server log에 남을 수 있음 — 운영 시 ROSSHIELD_REPLICATION_PRIMARY_CONN_STRING
//     env에만 두고 file로 dump 금지.
//   - create_slot=true: standby에서 publication 측에 replication slot 자동 생성.
//     slot 부족(`max_replication_slots` 초과) 시 에러.
//   - enabled=true: subscription 생성 즉시 worker 시작.
func ensureSubscription(ctx context.Context, exec Executor, spec SubscriptionSpec) error {
	if err := validateName(spec.Name); err != nil {
		return fmt.Errorf("ensureSubscription: name: %w", err)
	}
	if err := validateName(spec.PublicationName); err != nil {
		return fmt.Errorf("ensureSubscription: publication name: %w", err)
	}
	if spec.PrimaryConnString == "" {
		return ErrEmptyConnString
	}

	exists, err := exec.QueryBool(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_subscription WHERE subname = $1)",
		spec.Name,
	)
	if err != nil {
		return fmt.Errorf("ensureSubscription: check existence: %w", err)
	}
	if exists {
		return nil
	}

	copyData := "false"
	if spec.Copy {
		copyData = "true"
	}

	stmt := fmt.Sprintf(
		"CREATE SUBSCRIPTION %s CONNECTION '%s' PUBLICATION %s WITH (copy_data = %s, create_slot = true, enabled = true)",
		quoteIdent(spec.Name),
		escapeConnString(spec.PrimaryConnString),
		quoteIdent(spec.PublicationName),
		copyData,
	)

	if err := exec.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("ensureSubscription: create: %w", err)
	}
	return nil
}

// DropSubscription은 subscription을 제거합니다 (운영/테스트 cleanup용).
//
// 주의: standby PG에서 실행. 단순 DROP은 replication slot이 publication 측에 남을
// 수 있으므로, 운영 시에는 우선 `ALTER SUBSCRIPTION <name> DISABLE` + slot drop
// 절차 권장 (multi-region-ha-setup.md troubleshooting 참조).
func DropSubscription(ctx context.Context, exec Executor, name string) error {
	if err := validateName(name); err != nil {
		return fmt.Errorf("DropSubscription: %w", err)
	}
	stmt := fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", quoteIdent(name))
	if err := exec.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("DropSubscription: %w", err)
	}
	return nil
}
