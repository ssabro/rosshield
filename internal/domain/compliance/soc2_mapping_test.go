package compliance

import (
	"sort"
	"testing"
	"time"
)

// soc2_mapping_test.go — Phase 11.B-6 effectiveness dashboard 매핑 단위 테스트.

func TestSOC2CategoryMappings_Coverage14Categories(t *testing.T) {
	t.Parallel()
	wantCodes := []string{"CC1", "CC2", "CC3", "CC4", "CC5", "CC6", "CC7", "CC8", "CC9", "A1", "A2", "A5"}
	if len(SOC2CategoryMappings) != len(wantCodes) {
		t.Fatalf("category count = %d, want %d", len(SOC2CategoryMappings), len(wantCodes))
	}
	for i, c := range SOC2CategoryMappings {
		if c.Code != wantCodes[i] {
			t.Errorf("category[%d].Code = %q, want %q", i, c.Code, wantCodes[i])
		}
	}
}

func TestSOC2CategoryMappings_TotalSubControls(t *testing.T) {
	t.Parallel()
	total := 0
	for _, c := range SOC2CategoryMappings {
		total += len(c.SubControls)
	}
	// CC1(5) + CC2(3) + CC3(4) + CC4(2) + CC5(3) + CC6(8) + CC7(5) + CC8(1) + CC9(2)
	//    = 33 CC sub-controls
	// A1(3) + A2(2) + A5(2) = 7 A sub-controls
	// total = 40 (D-P11B-2 A1+A2+A5 default).
	want := 40
	if total != want {
		t.Errorf("total sub-controls = %d, want %d", total, want)
	}
}

func TestSOC2CategoryMappings_AllSubControlIDsValid(t *testing.T) {
	t.Parallel()
	for _, cat := range SOC2CategoryMappings {
		for _, sc := range cat.SubControls {
			if sc.ID == "" {
				t.Errorf("category %s has empty sub-control ID", cat.Code)
			}
			if sc.Title == "" {
				t.Errorf("sub-control %s has empty title", sc.ID)
			}
			if !sc.Covered && sc.GapNote == "" {
				t.Errorf("sub-control %s covered=false but GapNote empty", sc.ID)
			}
			if sc.Covered && sc.GapNote != "" {
				t.Errorf("sub-control %s covered=true but GapNote non-empty: %q", sc.ID, sc.GapNote)
			}
		}
	}
}

func TestMappedActions_DedupAndNonEmpty(t *testing.T) {
	t.Parallel()
	actions := MappedActions()
	if len(actions) == 0 {
		t.Fatal("MappedActions returned empty slice")
	}
	seen := make(map[string]bool)
	for _, a := range actions {
		if seen[a] {
			t.Errorf("MappedActions contains duplicate %q", a)
		}
		seen[a] = true
	}
	// 핵심 action 들이 포함됐는지 spot-check (cmd/bootstrap.go 의 emit 사이트 일관).
	mustHave := []string{
		"audit.compliance.export",
		"audit.chain.key_rotated",
		"audit.replication.failover",
		"user_role.synced",
		"scan.completed",
		"pack.installed",
	}
	for _, a := range mustHave {
		if !seen[a] {
			t.Errorf("MappedActions missing required action %q", a)
		}
	}
}

func TestNewEffectivenessWindow_StandardOffsets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	w := NewEffectivenessWindow(now)
	if !w.Now.Equal(now) {
		t.Errorf("Now = %v, want %v", w.Now, now)
	}
	if !w.OneDayAgo.Equal(now.Add(-24 * time.Hour)) {
		t.Errorf("OneDayAgo offset wrong: %v", w.OneDayAgo)
	}
	if !w.SevenDays.Equal(now.Add(-7 * 24 * time.Hour)) {
		t.Errorf("SevenDays offset wrong: %v", w.SevenDays)
	}
	if !w.ThirtyDays.Equal(now.Add(-30 * 24 * time.Hour)) {
		t.Errorf("ThirtyDays offset wrong: %v", w.ThirtyDays)
	}
}

func TestBuildEffectivenessDashboard_EmptyCounts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	d := BuildEffectivenessDashboard(map[string]ActionCounts{}, now)
	if d.TotalSubControls != 40 {
		t.Errorf("TotalSubControls = %d, want 40", d.TotalSubControls)
	}
	// covered 카운트는 매핑 정의의 static covered=true 수 — empty counts 와 무관.
	if d.CoveredSubControls == 0 {
		t.Error("CoveredSubControls = 0; SOC2 매핑은 covered=true sub-control 최소 1 이상 보유")
	}
	if d.CoverPercent < 0 || d.CoverPercent > 100 {
		t.Errorf("CoverPercent = %f, want [0, 100]", d.CoverPercent)
	}
	if len(d.Categories) != 12 {
		t.Errorf("Categories length = %d, want 12", len(d.Categories))
	}
	// 모든 카테고리는 lastDay/7d/30d == 0 — empty counts.
	for _, cat := range d.Categories {
		if cat.LastDay != 0 || cat.Last7Days != 0 || cat.Last30Days != 0 {
			t.Errorf("category %s expected all-zero counts, got %d/%d/%d",
				cat.Code, cat.LastDay, cat.Last7Days, cat.Last30Days)
		}
	}
	if !d.GeneratedAt.Equal(now) {
		t.Errorf("GeneratedAt = %v, want %v", d.GeneratedAt, now)
	}
}

func TestBuildEffectivenessDashboard_AggregatesCountsByAction(t *testing.T) {
	t.Parallel()
	counts := map[string]ActionCounts{
		"audit.chain.key_rotated":      {LastDay: 1, Last7Days: 3, Last30Days: 10},
		"audit.chain.rotation_aborted": {LastDay: 0, Last7Days: 0, Last30Days: 2},
		"user_role.synced":             {LastDay: 5, Last7Days: 20, Last30Days: 80},
	}
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	d := BuildEffectivenessDashboard(counts, now)

	// CC6.6 매핑 = {audit.chain.key_rotated, audit.chain.rotation_aborted}
	// => last30 = 10 + 2 = 12.
	var cc6 *CategorySnapshot
	for i := range d.Categories {
		if d.Categories[i].Code == "CC6" {
			cc6 = &d.Categories[i]
			break
		}
	}
	if cc6 == nil {
		t.Fatal("CC6 category missing")
	}
	var cc66 *SubControlSnapshot
	for i := range cc6.Items {
		if cc6.Items[i].ID == "CC6.6" {
			cc66 = &cc6.Items[i]
			break
		}
	}
	if cc66 == nil {
		t.Fatal("CC6.6 sub-control missing")
	}
	if cc66.Last30Days != 12 {
		t.Errorf("CC6.6 Last30Days = %d, want 12", cc66.Last30Days)
	}
}

func TestBuildEffectivenessDashboard_CoverPercentRollupPerCategory(t *testing.T) {
	t.Parallel()
	d := BuildEffectivenessDashboard(nil, time.Now())
	// CC1: 5 sub-controls, covered=true 셋: CC1.1 · CC1.3 · CC1.5 → covered=3 → 60.0.
	var cc1 *CategorySnapshot
	for i := range d.Categories {
		if d.Categories[i].Code == "CC1" {
			cc1 = &d.Categories[i]
			break
		}
	}
	if cc1 == nil {
		t.Fatal("CC1 missing")
	}
	if cc1.SubControls != 5 || cc1.Covered != 3 {
		t.Errorf("CC1: subControls=%d covered=%d, want 5/3", cc1.SubControls, cc1.Covered)
	}
	if cc1.CoverPercent < 59.9 || cc1.CoverPercent > 60.1 {
		t.Errorf("CC1.CoverPercent = %f, want ~60.0", cc1.CoverPercent)
	}
	// CC1 gaps: CC1.2 + CC1.4 — covered=false.
	if len(cc1.Gaps) != 2 {
		t.Errorf("CC1.Gaps length = %d, want 2", len(cc1.Gaps))
	}
	// gaps 정렬 안정 — sub-control 순서 보존.
	sortedGaps := append([]string(nil), cc1.Gaps...)
	sort.Strings(sortedGaps)
	if sortedGaps[0] != cc1.Gaps[0] || sortedGaps[1] != cc1.Gaps[1] {
		// 매핑 정의가 ID 정렬이면 자연스럽게 정렬됨 — 강제 sort 와 일치해야.
		t.Errorf("CC1.Gaps not ID-sorted: %v", cc1.Gaps)
	}
}
