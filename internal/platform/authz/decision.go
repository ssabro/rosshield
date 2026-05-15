// decision.go — Policy Decision Point(PDP) 진입점입니다.
//
// design doc §7 Stage 1: server middleware / DB / JWT 변경 0. 본 패키지는 호출자(Stage 4
// middleware)가 추출한 Subject + (resource, action) 입력만 받아 Decision을 반환합니다.

package authz

import (
	"fmt"
	"strings"
)

// Decision은 결정 결과입니다.
//
// Allow는 허용/거부, Reason은 결정 근거(감사 로그·디버깅용 사람 읽기 문자열).
// MatchedRole은 ALLOW를 발생시킨 첫 binding의 role 이름 (DENY면 빈 문자열).
type Decision struct {
	Allow       bool
	Reason      string
	MatchedRole string
}

// Decide는 Subject가 (resource, action) 권한을 가지는지 평가합니다.
//
// 평가 순서:
//
//  1. Subject.Bindings 빈 슬라이스 → DENY ("no bindings").
//  2. 각 binding을 순회 — 다음 모두 만족하면 ALLOW:
//     a. binding.RoleName 이 SystemRolePermissions에 있고 그 permission 셋이 (resource, action) 매치.
//     b. binding이 fleet scope면 Subject.FleetID 가 binding.ScopeID와 일치.
//     binding이 tenant scope면 fleet 매칭 무시 (모든 fleet implicit 통과).
//  3. 어떤 binding도 매치 안 하면 DENY ("no matching binding").
//
// 본 함수는 Subject 입력을 mutation하지 않습니다 (불변성).
func Decide(sub Subject, resource Resource, action Action) Decision {
	if len(sub.Bindings) == 0 {
		return Decision{Allow: false, Reason: "no bindings"}
	}

	for _, b := range sub.Bindings {
		perms, ok := SystemRolePermissions[b.RoleName]
		if !ok {
			// 알려지지 않은 role — 본 Stage 1은 시스템 role만 평가.
			// 사용자 정의 role은 Stage 2+에서 별 경로로 처리.
			continue
		}

		if !permissionsMatch(perms, resource, action) {
			continue
		}

		// scope 검증 — fleet scope binding은 Subject.FleetID와 ScopeID 정확 일치.
		// tenant scope binding은 모든 fleet에 implicit 통과 (Subject.FleetID 무관).
		if b.ScopeType == ScopeFleet {
			if b.ScopeID == "" {
				// 잘못된 binding (fleet scope인데 ScopeID 비어있음) — skip.
				continue
			}
			if b.ScopeID != sub.FleetID {
				continue
			}
		}

		return Decision{
			Allow:       true,
			Reason:      decisionReason(b, resource, action),
			MatchedRole: b.RoleName,
		}
	}

	return Decision{
		Allow:  false,
		Reason: fmt.Sprintf("no binding allows %s.%s (fleet=%q)", resource, action, sub.FleetID),
	}
}

// permissionsMatch는 permission 슬라이스 중 하나라도 (resource, action) 매치하면 true.
func permissionsMatch(perms []Permission, resource Resource, action Action) bool {
	for _, p := range perms {
		if p.Matches(resource, action) {
			return true
		}
	}
	return false
}

// decisionReason은 ALLOW 발생 binding의 사람 읽기 reason 문자열을 만듭니다.
func decisionReason(b RoleBinding, resource Resource, action Action) string {
	var sb strings.Builder
	sb.WriteString("allow by role=")
	sb.WriteString(b.RoleName)
	sb.WriteString(" scope=")
	sb.WriteString(string(b.ScopeType))
	if b.ScopeType == ScopeFleet {
		sb.WriteString("[")
		sb.WriteString(b.ScopeID)
		sb.WriteString("]")
	}
	sb.WriteString(" perm=")
	sb.WriteString(string(resource))
	sb.WriteString(".")
	sb.WriteString(string(action))
	return sb.String()
}
