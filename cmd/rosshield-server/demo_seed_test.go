package main

// demo_seed_test.go — `seed demo` 서브커맨드 단위 테스트 (Phase 2 Exit 시연 데이터 회귀 방지).
//
// 시나리오: 정상 시드·필수 옵션 누락·미존재 tenant·멱등성·drift trigger 패턴·
// e2e 컴플라이언스/insight 흐름 가능한 데이터 보장.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// seedDemoFixture는 admin seed 후 데모 시드 호출 직전 상태를 만듭니다.
func seedDemoFixture(t *testing.T) (dir, email string) {
	t.Helper()
	dir = t.TempDir()
	email = "demo@example.com"
	first := runSeedCapture(t, []string{
		"admin",
		"--email", email,
		"--password", "verylongdemopassword123",
		"--data-dir", dir,
		"--name", "Demo Tenant",
	}, "")
	if first.exit != 0 {
		t.Fatalf("admin seed prerequisite exit=%d stderr=%s", first.exit, first.stderr)
	}
	return dir, email
}

func TestSeedDemoRejectsMissingEmail(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{"demo", "--data-dir", dir}, "")
	if res.exit != 2 {
		t.Errorf("exit=%d, want 2; stderr=%s", res.exit, res.stderr)
	}
}

func TestSeedDemoRejectsUnknownTenant(t *testing.T) {
	dir := t.TempDir()
	// admin 시드 없이 demo 시도 — exit 3 (no tenant for email).
	res := runSeedCapture(t, []string{
		"demo",
		"--email", "nobody@example.com",
		"--data-dir", dir,
	}, "")
	if res.exit != 3 {
		t.Errorf("exit=%d, want 3 (unknown tenant); stderr=%s", res.exit, res.stderr)
	}
}

func TestSeedDemoCreatesAllEntities(t *testing.T) {
	dir, email := seedDemoFixture(t)

	res := runSeedCapture(t, []string{"demo", "--email", email, "--data-dir", dir}, "")
	if res.exit != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exit, res.stderr)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout: %v\nraw: %s", err, res.stdout)
	}

	for _, k := range []string{"tenantId", "fleetId", "packId", "robotIds", "sessionIds", "driftRobot", "driftCheck"} {
		if _, ok := out[k]; !ok {
			t.Errorf("missing JSON key %q", k)
		}
	}

	if pid, _ := out["packId"].(string); pid != demoPackID {
		t.Errorf("packId=%q, want %q", pid, demoPackID)
	}
	if dr, _ := out["driftRobot"].(string); dr != "demo-robot-1" {
		t.Errorf("driftRobot=%q, want demo-robot-1", dr)
	}
	if dc, _ := out["driftCheck"].(string); dc != demoCheckMapped {
		t.Errorf("driftCheck=%q, want %q", dc, demoCheckMapped)
	}

	robotIDs := stringSlice(t, out["robotIds"])
	if len(robotIDs) != 3 {
		t.Errorf("robotIds count=%d, want 3", len(robotIDs))
	}
	for _, id := range robotIDs {
		if !strings.HasPrefix(id, "ro_") {
			t.Errorf("robot ID %q missing 'ro_' prefix", id)
		}
	}

	sessionIDs := stringSlice(t, out["sessionIds"])
	if len(sessionIDs) != 5 {
		t.Errorf("sessionIds count=%d, want 5", len(sessionIDs))
	}
}

func TestSeedDemoIsIdempotent(t *testing.T) {
	dir, email := seedDemoFixture(t)

	first := runSeedCapture(t, []string{"demo", "--email", email, "--data-dir", dir}, "")
	if first.exit != 0 {
		t.Fatalf("first demo seed exit=%d stderr=%s", first.exit, first.stderr)
	}
	var firstOut map[string]any
	_ = json.Unmarshal([]byte(first.stdout), &firstOut)

	second := runSeedCapture(t, []string{"demo", "--email", email, "--data-dir", dir}, "")
	if second.exit != 0 {
		t.Fatalf("second demo seed exit=%d stderr=%s", second.exit, second.stderr)
	}
	var secondOut map[string]any
	if err := json.Unmarshal([]byte(second.stdout), &secondOut); err != nil {
		t.Fatalf("unmarshal second stdout: %v", err)
	}
	if w, _ := secondOut["wasExisting"].(bool); !w {
		t.Errorf("second seed wasExisting=false, want true")
	}

	// fleetID·packID·robotIDs·sessionIDs 모두 첫 번째와 동일해야 함 (멱등 — 같은 row 재사용).
	for _, k := range []string{"fleetId", "packId"} {
		if firstOut[k] != secondOut[k] {
			t.Errorf("%s drift: first=%v second=%v", k, firstOut[k], secondOut[k])
		}
	}
}

func TestSeedDemoCreatesDriftPattern(t *testing.T) {
	dir, email := seedDemoFixture(t)
	res := runSeedCapture(t, []string{"demo", "--email", email, "--data-dir", dir}, "")
	if res.exit != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exit, res.stderr)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(res.stdout), &out)

	tenantIDStr, _ := out["tenantId"].(string)
	tenantID := storage.TenantID(tenantIDStr)
	sessionIDs := stringSlice(t, out["sessionIds"])
	robotIDs := stringSlice(t, out["robotIds"])
	if len(sessionIDs) < 5 || len(robotIDs) < 1 {
		t.Fatalf("preconditions: sessions=%d robots=%d", len(sessionIDs), len(robotIDs))
	}

	platform := bootstrapForVerify(t, dir)
	defer shutdownPlatform(t, platform)

	ctx := storage.WithTenantID(context.Background(), tenantID)

	// 마지막(drift) session — robot[0]의 demoCheckMapped만 fail, 나머지는 pass.
	driftSessionID := sessionIDs[len(sessionIDs)-1]
	var failsOnRobotZero, passesOnOthers int
	err := platform.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		results, e := platform.Scan.ListResults(ctx, tx, driftSessionID)
		if e != nil {
			return e
		}
		for _, r := range results {
			if r.RobotID == robotIDs[0] && r.CheckID == demoCheckMapped {
				if r.Outcome == scan.OutcomeFail {
					failsOnRobotZero++
				}
			} else if r.Outcome == scan.OutcomePass {
				passesOnOthers++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ListResults: %v", err)
	}
	if failsOnRobotZero != 1 {
		t.Errorf("drift session: robot-1 demoCheckMapped fail count=%d, want 1", failsOnRobotZero)
	}
	if passesOnOthers != 5 {
		// 6 results - 1 fail = 5 pass on others (3 robots × 2 checks - 1 fail).
		t.Errorf("drift session: passes on other robot/check pairs=%d, want 5", passesOnOthers)
	}

	// Insight backfill — drift kind 1건 이상.
	var driftInsights int
	err = platform.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		ins, e := platform.Insight.ListActive(ctx, tx, insight.ListFilter{})
		if e != nil {
			return e
		}
		for _, i := range ins {
			if i.Kind == insight.KindDrift {
				driftInsights++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if driftInsights == 0 {
		t.Errorf("drift insight count=0, want ≥1 (RunForFleet backfill 실패)")
	}
}

// stringSlice는 JSON 배열 (any)을 []string으로 변환합니다.
func stringSlice(t *testing.T, v any) []string {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("not a JSON array: %T %v", v, v)
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// bootstrapForVerify는 테스트 검증용 platform을 부트합니다 (시드된 같은 dir).
func bootstrapForVerify(t *testing.T, dir string) *Platform {
	t.Helper()
	ctx := context.Background()
	p, err := Bootstrap(ctx, Config{DataDir: dir, Logger: platformLoggerOrDiscard()})
	if err != nil {
		t.Fatalf("verify Bootstrap: %v", err)
	}
	return p
}

func shutdownPlatform(t *testing.T, p *Platform) {
	t.Helper()
	ctx := context.Background()
	_ = p.Shutdown(ctx)
}
