package compliance

// soc2_mapping.go — Phase 11.B-6 effectiveness dashboard 매핑 정의.
//
// 본 파일은 SOC2 Trust Services Criteria(TSC) sub-control 별 매핑된
// audit event action 패턴을 정의합니다. Lodestar 자체의 audit chain 으로부터
// 통제 effectiveness 를 자동 측정하기 위한 truth source.
//
// design doc: docs/design/notes/soc2-readiness-design.md §7.6 (Stage 11.B-6).
//
// audit event action 명명 규약 (cmd/rosshield-server/bootstrap.go + handlers/* 인벤토리):
//
//	tenant.created          robot.created/deleted    fleet.created/updated/deleted
//	scan.started/completed  pack.installed           reporting.generate/sign
//	audit.compliance.export audit.replication.failover  audit.chain.key_rotated
//	user_role.synced        sso.provider.<action>    invitation.sent/accepted
//	insight.created/dismissed  advisor.*           compliance.profile.created
//	compliance.snapshot.generated  evidence.stored
//
// 일관성 보장: cmd/rosshield-server/bootstrap.go 의 audit emit 사이트가 truth source —
// 본 매핑이 사용하는 action 문자열은 모두 실제 emit 되는 action 과 정확히 일치해야 합니다.
// 신규 audit emit 추가 시 본 파일에 매핑 추가 검토 권장.
//
// 매핑 정책:
//
//	covered = true  → 해당 sub-control 은 Lodestar 결선 자산이 직접/자동으로 cover.
//	covered = false → Lodestar 만으로 cover 불가 (외부 트랙 ★ 필수 — ethics audit · HR · board 등).
//
// 47 sub-control 가정은 docs/compliance/soc2/ 의 실 sub-control 카운트와 일치하지 않을 수
// 있습니다. 본 매핑은 docs/compliance/soc2/*.md 의 sub-control 식별자를 truth source 로
// 사용 — 총 14 카테고리(CC1~CC9 + A1~A5).

import "time"

// SubControlMapping 은 한 SOC2 sub-control 의 매핑 정의입니다.
//
// Actions 가 비어있으면 통제는 audit event 로 측정 불가 — 외부 트랙 ★ 만 cover.
// Covered 는 Lodestar 결선 자산으로 cover 가능한지의 정적 판단 (외부 트랙 의존 0).
type SubControlMapping struct {
	ID      string   // "CC1.1" · "A1.2" 등 — docs/compliance/soc2/*.md 와 일치.
	Title   string   // 짧은 한글/영어 라벨 (자체 작성, 저작권 안전).
	Actions []string // 매핑된 audit event action 패턴 (정확 매칭 — wildcard 미지원).
	Covered bool     // Lodestar 결선 자산이 cover 가능하면 true; 외부 트랙 ★ 의존이면 false.
	GapNote string   // covered=false 일 때 gap 요약 (UI gap 카드 표시용). covered=true 면 빈 문자열.
}

// CategoryMapping 은 한 SOC2 카테고리(CC1~CC9 + A1~A5)의 정의입니다.
//
// Code 는 정렬 키 — UI 매트릭스 표 행 순서 결정. Sub-controls 는 ID 정렬 권장
// (CC1.1 → CC1.2 → ... ).
type CategoryMapping struct {
	Code        string              // "CC1" · "A1" 등.
	Name        string              // "Control Environment" 등 — 짧은 영어 라벨 (i18n 키 dispatch).
	SubControls []SubControlMapping // 정렬: ID ASC.
}

// SOC2CategoryMappings 는 14 SOC2 카테고리 × sub-control 정적 매핑입니다.
//
// 본 슬라이스는 effectiveness dashboard 의 cover% rollup + audit event 집계의 truth source.
// 변경 시 web/src/i18n/dict.ts 의 compliance.dashboard.category.<code> 키도 동기화.
//
// covered 판단은 docs/compliance/soc2/*.md 의 "Lodestar 결선 자산" + "gap" 컬럼을 기반:
//
//	결선 자산이 모든 요소를 cover → covered=true
//	gap 이 "외부 firm" · "HR 트랙" · "board 절차" 등 외부 트랙 ★ 의존 → covered=false
var SOC2CategoryMappings = []CategoryMapping{
	{
		Code: "CC1", Name: "Control Environment",
		SubControls: []SubControlMapping{
			{ID: "CC1.1", Title: "Integrity and Ethical Values",
				Actions: nil, Covered: true,
				GapNote: ""},
			{ID: "CC1.2", Title: "Board Oversight",
				Actions: nil, Covered: false,
				GapNote: "공식 board structure docs 부재 (small team) — 외부 트랙 ★"},
			{ID: "CC1.3", Title: "Structure, Authority and Responsibilities",
				Actions: []string{"user_role.synced", "invitation.sent", "invitation.accepted"},
				Covered: true, GapNote: ""},
			{ID: "CC1.4", Title: "Commitment to Competence",
				Actions: nil, Covered: false,
				GapNote: "security awareness training 콘텐츠 부재 — 외부 트랙 ★"},
			{ID: "CC1.5", Title: "Accountability",
				Actions: []string{"audit.compliance.export"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "CC2", Name: "Communication and Information",
		SubControls: []SubControlMapping{
			{ID: "CC2.1", Title: "Information Quality",
				Actions: []string{"evidence.stored", "reporting.generate"},
				Covered: true, GapNote: ""},
			{ID: "CC2.2", Title: "Internal Communication",
				Actions: []string{"insight.created", "insight.dismissed"},
				Covered: true, GapNote: ""},
			{ID: "CC2.3", Title: "External Communication",
				Actions: []string{"reporting.framework.generate", "reporting.framework.sign"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "CC3", Name: "Risk Assessment",
		SubControls: []SubControlMapping{
			{ID: "CC3.1", Title: "Specifies Objectives",
				Actions: nil, Covered: false,
				GapNote: "공식 risk register 부재 — 외부 트랙 ★"},
			{ID: "CC3.2", Title: "Identifies Risks",
				Actions: []string{"scan.started", "scan.completed", "scan.failed"},
				Covered: true, GapNote: ""},
			{ID: "CC3.3", Title: "Fraud Risk",
				Actions: []string{"audit.chain.key_rotated", "audit.chain.rotation_aborted"},
				Covered: true, GapNote: ""},
			{ID: "CC3.4", Title: "Significant Change",
				Actions: []string{"pack.installed", "pack.lifecycle.deprecated"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "CC4", Name: "Monitoring Activities",
		SubControls: []SubControlMapping{
			{ID: "CC4.1", Title: "Ongoing Monitoring",
				Actions: []string{"scan.completed", "scan.failed", "scan.cancelled"},
				Covered: true, GapNote: ""},
			{ID: "CC4.2", Title: "Deficiency Communication",
				Actions: []string{"insight.created", "audit.compliance.export"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "CC5", Name: "Control Activities",
		SubControls: []SubControlMapping{
			{ID: "CC5.1", Title: "Selects and Develops Controls",
				Actions: []string{"pack.installed"},
				Covered: true, GapNote: ""},
			{ID: "CC5.2", Title: "Technology Controls",
				Actions: []string{"compliance.profile.created", "compliance.snapshot.generated"},
				Covered: true, GapNote: ""},
			{ID: "CC5.3", Title: "Policies and Procedures",
				Actions: []string{"user_role.synced"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "CC6", Name: "Logical and Physical Access",
		SubControls: []SubControlMapping{
			{ID: "CC6.1", Title: "Logical Access Security",
				Actions: []string{"user_role.synced", "sso.login.started", "sso.login.completed"},
				Covered: true, GapNote: ""},
			{ID: "CC6.2", Title: "User Registration and Authorization",
				Actions: []string{"invitation.sent", "invitation.accepted"},
				Covered: true, GapNote: ""},
			{ID: "CC6.3", Title: "Role-based Access Control",
				Actions: []string{"user_role.synced", "sso.provider.created", "sso.provider.updated", "sso.provider.deleted"},
				Covered: true, GapNote: ""},
			{ID: "CC6.4", Title: "Physical Access",
				Actions: nil, Covered: false,
				GapNote: "데이터센터 물리 접근 — cloud provider SOC2 의존 ★"},
			{ID: "CC6.5", Title: "Asset Disposal",
				Actions: nil, Covered: false,
				GapNote: "물리 자산 disposal 절차 부재 — 외부 트랙 ★"},
			{ID: "CC6.6", Title: "Cryptographic Key Management",
				Actions: []string{"audit.chain.key_rotated", "audit.chain.rotation_aborted"},
				Covered: true, GapNote: ""},
			{ID: "CC6.7", Title: "Data Transmission Security",
				Actions: []string{"audit.compliance.export"},
				Covered: true, GapNote: ""},
			{ID: "CC6.8", Title: "Malicious Software",
				Actions: []string{"scan.completed"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "CC7", Name: "System Operations",
		SubControls: []SubControlMapping{
			{ID: "CC7.1", Title: "Detect and Monitor",
				Actions: []string{"scan.started", "scan.completed", "scan.failed", "audit.replication.failover"},
				Covered: true, GapNote: ""},
			{ID: "CC7.2", Title: "Anomaly Monitoring",
				Actions: []string{"audit.chain.rotation_aborted", "scan.failed"},
				Covered: true, GapNote: ""},
			{ID: "CC7.3", Title: "Security Event Evaluation",
				Actions: []string{"insight.created", "insight.dismissed"},
				Covered: true, GapNote: ""},
			{ID: "CC7.4", Title: "Incident Response",
				Actions: []string{"audit.replication.failover", "audit.chain.rotation_aborted"},
				Covered: true, GapNote: ""},
			{ID: "CC7.5", Title: "Recovery Procedures",
				Actions: []string{"audit.replication.failover"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "CC8", Name: "Change Management",
		SubControls: []SubControlMapping{
			{ID: "CC8.1", Title: "Authorized Changes",
				Actions: []string{"pack.installed", "pack.lifecycle.active", "pack.lifecycle.deprecated", "pack.lifecycle.retired"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "CC9", Name: "Risk Mitigation",
		SubControls: []SubControlMapping{
			{ID: "CC9.1", Title: "Business Disruption",
				Actions: []string{"audit.replication.failover"},
				Covered: true, GapNote: ""},
			{ID: "CC9.2", Title: "Vendor Risk Management",
				Actions: nil, Covered: false,
				GapNote: "공식 vendor risk assessment docs 부재 — 외부 트랙 ★"},
		},
	},
	{
		Code: "A1", Name: "Availability",
		SubControls: []SubControlMapping{
			{ID: "A1.1", Title: "Capacity Planning",
				Actions: []string{"audit.replication.failover"},
				Covered: true, GapNote: ""},
			{ID: "A1.2", Title: "Environmental Protections",
				Actions: nil, Covered: false,
				GapNote: "물리 환경 보호 — cloud provider SOC2 의존 ★"},
			{ID: "A1.3", Title: "Recovery Testing",
				Actions: []string{"audit.replication.failover", "scan.completed"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "A2", Name: "Confidentiality",
		SubControls: []SubControlMapping{
			{ID: "A2.1", Title: "Confidentiality of Information",
				Actions: []string{"audit.compliance.export", "evidence.stored"},
				Covered: true, GapNote: ""},
			{ID: "A2.2", Title: "Disposal of Confidential Information",
				Actions: []string{"audit.gc.complete"},
				Covered: true, GapNote: ""},
		},
	},
	{
		Code: "A5", Name: "Security",
		SubControls: []SubControlMapping{
			{ID: "A5.1", Title: "Logical Access Protection",
				Actions: []string{"user_role.synced", "sso.login.completed"},
				Covered: true, GapNote: ""},
			{ID: "A5.2", Title: "Boundary Protection",
				Actions: []string{"audit.compliance.export"},
				Covered: true, GapNote: ""},
		},
	},
}

// MappedActions 는 SOC2CategoryMappings 의 모든 action 의 dedup 정렬 슬라이스를 반환합니다.
//
// effectiveness handler 의 audit_entries 집계 query 가 IN (...) 절에 사용 — 한 번의 쿼리로
// 모든 매핑 대상 action count 를 회수합니다.
func MappedActions() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, cat := range SOC2CategoryMappings {
		for _, sc := range cat.SubControls {
			for _, a := range sc.Actions {
				if _, ok := seen[a]; ok {
					continue
				}
				seen[a] = struct{}{}
				out = append(out, a)
			}
		}
	}
	return out
}

// EffectivenessWindow 는 effectiveness aggregate 의 시간 윈도우 셋입니다.
//
// Now 는 호출 시점 — clock.Clock 으로 주입. Day/Week/Month 는 Now 로부터 거꾸로 1/7/30 일.
type EffectivenessWindow struct {
	Now        time.Time
	OneDayAgo  time.Time
	SevenDays  time.Time
	ThirtyDays time.Time
}

// NewEffectivenessWindow 는 now 로부터 표준 1/7/30 일 윈도우를 빌드합니다.
func NewEffectivenessWindow(now time.Time) EffectivenessWindow {
	now = now.UTC()
	return EffectivenessWindow{
		Now:        now,
		OneDayAgo:  now.Add(-24 * time.Hour),
		SevenDays:  now.Add(-7 * 24 * time.Hour),
		ThirtyDays: now.Add(-30 * 24 * time.Hour),
	}
}

// ActionCounts 는 한 action 의 윈도우별 카운트 묶음입니다.
type ActionCounts struct {
	LastDay    int64
	Last7Days  int64
	Last30Days int64
}

// EffectivenessSnapshot 은 한 sub-control 의 effectiveness 평가입니다 (UI rendering 단위).
type SubControlSnapshot struct {
	ID         string
	Title      string
	Actions    []string
	Covered    bool
	GapNote    string
	LastDay    int64
	Last7Days  int64
	Last30Days int64
}

// CategorySnapshot 은 한 카테고리의 effectiveness rollup 입니다.
type CategorySnapshot struct {
	Code         string
	Name         string
	SubControls  int                  // 본 카테고리의 sub-control 총수.
	Covered      int                  // covered=true 인 sub-control 카운트.
	CoverPercent float64              // (Covered / SubControls) * 100. SubControls=0 이면 0.
	LastDay      int64                // 본 카테고리 매핑 action 의 1 일 합계.
	Last7Days    int64                // 본 카테고리 매핑 action 의 7 일 합계.
	Last30Days   int64                // 본 카테고리 매핑 action 의 30 일 합계.
	Gaps         []string             // covered=false 인 sub-control 라벨 ("CC1.4 Commitment to Competence" 등).
	Items        []SubControlSnapshot // 정렬: ID ASC.
}

// EffectivenessDashboard 는 effectiveness handler 응답의 최상위 묶음입니다.
type EffectivenessDashboard struct {
	TotalSubControls   int
	CoveredSubControls int
	CoverPercent       float64
	Categories         []CategorySnapshot
	GeneratedAt        time.Time
}

// BuildEffectivenessDashboard 는 SOC2CategoryMappings 와 action counts 를 묶어
// EffectivenessDashboard 를 빌드합니다.
//
// counts 는 caller (handler) 가 audit_entries 집계 query 결과를 dedup action 별로 채운 map.
// 매핑되지 않은 action 은 무시 — 합산 0 처리.
//
// 본 함수는 도메인 순수 함수 (storage 의존 0) — 단위 테스트 직접 작성 가능.
func BuildEffectivenessDashboard(counts map[string]ActionCounts, generatedAt time.Time) EffectivenessDashboard {
	total := 0
	covered := 0
	cats := make([]CategorySnapshot, 0, len(SOC2CategoryMappings))

	for _, cat := range SOC2CategoryMappings {
		catLastDay, cat7, cat30 := int64(0), int64(0), int64(0)
		catCovered := 0
		gaps := make([]string, 0)
		items := make([]SubControlSnapshot, 0, len(cat.SubControls))

		for _, sc := range cat.SubControls {
			scLastDay, sc7, sc30 := int64(0), int64(0), int64(0)
			for _, a := range sc.Actions {
				if c, ok := counts[a]; ok {
					scLastDay += c.LastDay
					sc7 += c.Last7Days
					sc30 += c.Last30Days
				}
			}
			catLastDay += scLastDay
			cat7 += sc7
			cat30 += sc30

			if sc.Covered {
				catCovered++
			} else {
				gaps = append(gaps, sc.ID+" "+sc.Title)
			}

			items = append(items, SubControlSnapshot{
				ID:         sc.ID,
				Title:      sc.Title,
				Actions:    sc.Actions,
				Covered:    sc.Covered,
				GapNote:    sc.GapNote,
				LastDay:    scLastDay,
				Last7Days:  sc7,
				Last30Days: sc30,
			})
		}

		coverPct := 0.0
		if len(cat.SubControls) > 0 {
			coverPct = float64(catCovered) / float64(len(cat.SubControls)) * 100.0
		}

		cats = append(cats, CategorySnapshot{
			Code:         cat.Code,
			Name:         cat.Name,
			SubControls:  len(cat.SubControls),
			Covered:      catCovered,
			CoverPercent: coverPct,
			LastDay:      catLastDay,
			Last7Days:    cat7,
			Last30Days:   cat30,
			Gaps:         gaps,
			Items:        items,
		})

		total += len(cat.SubControls)
		covered += catCovered
	}

	overallPct := 0.0
	if total > 0 {
		overallPct = float64(covered) / float64(total) * 100.0
	}

	return EffectivenessDashboard{
		TotalSubControls:   total,
		CoveredSubControls: covered,
		CoverPercent:       overallPct,
		Categories:         cats,
		GeneratedAt:        generatedAt.UTC(),
	}
}
