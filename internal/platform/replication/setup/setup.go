// Package setup은 PG logical replication의 PUBLICATION/SUBSCRIPTION을 자동으로
// 생성·검증합니다 (E-MR Stage 3, multi-region HA).
//
// 본 패키지는 부팅 시점에 단일 region의 role에 따라 다음 중 하나를 수행합니다:
//   - primary role: pg_publication에 `rosshield_main` PUBLICATION 존재 여부 확인,
//     없으면 CREATE PUBLICATION 실행 (idempotent).
//   - standby role: pg_subscription에 `rosshield_main_sub` SUBSCRIPTION 존재 여부
//     확인, 없으면 CREATE SUBSCRIPTION 실행 (idempotent, primary conn string 필요).
//
// 설계 doc: docs/design/notes/multi-region-ha-design.md §5 Stage 3.
//
// 도메인 경계 (P5): internal/platform/replication 의 Role 타입만 import. 직접
// pgxpool을 다루지만 도메인 코드 진입 0.
//
// 본 round (Stage 3) 미진행 (Stage 4~7 carryover):
//   - Route53 DNS hook 실 SDK 호출
//   - 자동 failover (heartbeat timeout)
//   - cross-region audit witness fold-in
//   - publication tables 변경 자동 동기화 (현재는 첫 생성만 처리)
//   - replication slot cleanup 자동화
package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ssabro/rosshield/internal/platform/replication"
)

// Executor는 PG pgxpool 또는 mock의 최소 인터페이스입니다.
//
// pgxpool.Pool은 본 인터페이스를 자연스럽게 만족 — 단위 test는 fake 구현으로 mock.
type Executor interface {
	// Exec은 결과 row가 없는 SQL을 실행합니다.
	Exec(ctx context.Context, sql string, args ...any) error
	// QueryBool은 단일 boolean 값을 반환하는 SELECT를 실행합니다.
	QueryBool(ctx context.Context, sql string, args ...any) (bool, error)
}

// PublicationSpec은 primary 인스턴스가 publish할 테이블 spec입니다.
//
// AllTables=true (권장): FOR ALL TABLES. 신규 테이블 자동 포함 — 마이그레이션 추가
// 시 publication 갱신 누락 risk 회피. tenant 메타 + audit chain 등 모든 테이블이
// 자동으로 cross-region replicate (multi-region-ha-design §4.5).
//
// AllTables=false: Tables 명시. 일부 테이블만 선택할 때 사용 (운영 사정).
type PublicationSpec struct {
	Name      string
	Tables    []string
	AllTables bool
}

// SubscriptionSpec은 standby 인스턴스가 따라갈 publication spec입니다.
//
// PrimaryConnString은 standby가 primary PG에 logical replication 연결할 때
// 사용하는 conn string (예: "host=primary.us-west-2 port=5432 user=replica
// password=*** dbname=rosshield sslmode=require").
//
// Copy=true: subscription 생성 직후 publication 측 모든 데이터를 복사한 다음
// streaming 시작 (초기 부트스트랩). Copy=false: 이미 데이터 복사 완료 가정 —
// LSN 기준으로 즉시 streaming (운영 시나리오).
type SubscriptionSpec struct {
	Name              string
	PublicationName   string
	PrimaryConnString string
	Copy              bool
}

// 공통 에러.
var (
	ErrPublicationSpecMissing  = errors.New("setup: primary role requires PublicationSpec")
	ErrSubscriptionSpecMissing = errors.New("setup: standby role requires SubscriptionSpec")
	ErrUnknownRole             = errors.New("setup: unknown replication role")
	ErrEmptyName               = errors.New("setup: name is required")
	ErrEmptyTables             = errors.New("setup: tables empty (use AllTables=true for all)")
	ErrEmptyConnString         = errors.New("setup: primary conn string is required")
	ErrEmptyPublicationName    = errors.New("setup: publication name is required")
)

// Setup은 role에 따라 publication 또는 subscription을 idempotent하게 생성합니다.
//
// 동작:
//   - role == primary → ensurePublication(exec, pubSpec)
//   - role == standby → ensureSubscription(exec, subSpec)
//   - 그 외 → ErrUnknownRole
//
// idempotent: pg_publication / pg_subscription에 이미 같은 이름이 있으면 skip
// (no-op 반환). 두 번째 부팅에서 에러 없음.
func Setup(
	ctx context.Context,
	exec Executor,
	role replication.Role,
	pubSpec *PublicationSpec,
	subSpec *SubscriptionSpec,
) error {
	switch role {
	case replication.RolePrimary:
		if pubSpec == nil {
			return ErrPublicationSpecMissing
		}
		return ensurePublication(ctx, exec, *pubSpec)
	case replication.RoleStandby:
		if subSpec == nil {
			return ErrSubscriptionSpecMissing
		}
		return ensureSubscription(ctx, exec, *subSpec)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownRole, role)
	}
}

// DefaultPublicationSpec은 docs/design/notes/multi-region-ha-design.md §4.5에서
// 권장된 default를 반환합니다 (FOR ALL TABLES, name="rosshield_main").
func DefaultPublicationSpec() PublicationSpec {
	return PublicationSpec{
		Name:      "rosshield_main",
		AllTables: true,
	}
}

// DefaultSubscriptionSpec은 standby의 default를 반환합니다. primaryConnString은
// 호출자가 채워야 합니다 (env에서 주입). PublicationName은 primary와 일치 필요.
func DefaultSubscriptionSpec(primaryConnString string) SubscriptionSpec {
	return SubscriptionSpec{
		Name:              "rosshield_main_sub",
		PublicationName:   "rosshield_main",
		PrimaryConnString: primaryConnString,
		Copy:              false, // 운영 default: 이미 데이터 복사 완료 가정
	}
}

// validateName은 PUBLICATION/SUBSCRIPTION 이름이 SQL identifier로 안전한지 검증.
// 알파벳·숫자·underscore만 허용 — quoteIdent escape는 추가 방어, 본 함수가 1차 차단.
func validateName(name string) error {
	if name == "" {
		return ErrEmptyName
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return fmt.Errorf("setup: invalid identifier %q at position %d (allowed: a-z A-Z 0-9 _)", name, i)
		}
	}
	return nil
}

// joinQuotedTables는 테이블 이름 목록을 quoteIdent + "," 로 합칩니다.
func joinQuotedTables(tables []string) (string, error) {
	if len(tables) == 0 {
		return "", ErrEmptyTables
	}
	quoted := make([]string, 0, len(tables))
	for _, t := range tables {
		if err := validateName(t); err != nil {
			return "", err
		}
		quoted = append(quoted, quoteIdent(t))
	}
	return strings.Join(quoted, ", "), nil
}
