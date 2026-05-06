package main

// demo_seed.go — `rosshield-server seed demo` 서브커맨드 (Phase 2 Exit 시연 데이터).
//
// 목적:
//
//	이미 시드된 admin tenant에 시연용 fleet/robot/scan 데이터를 추가하여,
//	Compliance·Insight·Framework PDF 흐름을 e2e로 시연 가능하게 한다.
//
// 시드 내용 (멱등 — 두 번째 호출은 이미 존재하는 row를 재사용):
//
//   - Fleet "demo-fleet" 1개
//   - Robot "demo-robot-{1,2,3}" 3개 (dummy password credential, host=127.0.0.1)
//   - Scan session 5개 (모두 status=completed):
//     · 1~4 sessions: 모든 robot × 모든 check PASS  (baseline)
//     · 5번째 session: robot-1의 CIS-1.1.1.1만 FAIL (drift trigger for ISMS-P:2.5.1)
//
// 디자인:
//
//   - Pack 시드는 별도 — packID는 dummy `pk_DEMO_PACK`. scan.RecordResult는 packID를
//     검증하지 않으므로 동작. 진짜 pack은 향후 cmd/pack-tools와 통합 가능.
//   - PackCheckID는 ISMS-P framework YAML의 mappedChecks와 일치시킴(`CIS-1.1.1.1` 등) —
//     compliance snapshot 산출이 의미있게 동작하도록.
//   - 시드 후 W3 자동 구독자가 scan.completed 이벤트로 Insight를 자동 생성한다.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// 시연용 PackCheckID — ISMS-P framework YAML의 mappedChecks와 일치.
// CIS-1.1.1.1 → ISMS-P:2.5.1 매핑. 다른 check ID는 unmapped.
const (
	demoPackID       = "pk_DEMO_PACK"
	demoCheckMapped  = "CIS-1.1.1.1"
	demoCheckUnmappd = "CIS-1.2.1.1"
)

// 시연용 robot/check ID는 결정적 — idempotent 시드를 위해.
var demoRobotNames = []string{"demo-robot-1", "demo-robot-2", "demo-robot-3"}

// demoSeedOptions는 `seed demo` CLI 입력 묶음입니다.
type demoSeedOptions struct {
	email   string
	dataDir string
}

// demoSeedOutput은 stdout JSON 출력 형식입니다.
type demoSeedOutput struct {
	TenantID    string   `json:"tenantId"`
	FleetID     string   `json:"fleetId"`
	PackID      string   `json:"packId"`
	RobotIDs    []string `json:"robotIds"`
	SessionIDs  []string `json:"sessionIds"`
	DriftRobot  string   `json:"driftRobot"`
	DriftCheck  string   `json:"driftCheck"`
	SeededAt    string   `json:"seededAt"`
	WasExisting bool     `json:"wasExisting"`
}

// runSeedDemo는 `seed demo` 본 흐름입니다.
//
// 처리 순서:
//  1. flag 파싱 → 실패 시 exit 2.
//  2. Bootstrap → admin email로 tenantID 룩업 (없으면 exit 3).
//  3. tenant scope tx에서 Fleet → Robot × 3 → Scan session × 5 시드.
//  4. JSON stdout + Shutdown.
func runSeedDemo(args []string) int {
	opts, code := parseSeedDemoFlags(args)
	if code != 0 {
		return code
	}

	bootCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	platform, err := Bootstrap(bootCtx, Config{
		DataDir: opts.dataDir,
		Logger:  platformLoggerOrDiscard(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed demo: bootstrap failed: %v\n", err)
		return 1
	}
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = platform.Shutdown(shutdownCtx)
	}()

	tenantID, err := lookupTenantByEmail(bootCtx, platform, opts.email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed demo: lookup tenant failed: %v\n", err)
		return 3
	}
	if tenantID == "" {
		fmt.Fprintf(os.Stderr, "seed demo: no tenant found for email %q (run `seed admin` first)\n", opts.email)
		return 3
	}

	out, code := executeSeedDemo(bootCtx, platform, tenantID)
	if code != 0 {
		return code
	}

	out.TenantID = string(tenantID)
	out.SeededAt = time.Now().UTC().Format(time.RFC3339Nano)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "seed demo: encode output: %v\n", err)
		return 1
	}
	return 0
}

func parseSeedDemoFlags(args []string) (demoSeedOptions, int) {
	fs := flag.NewFlagSet("seed demo", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	email := fs.String("email", "", "admin email of the target tenant (required)")
	dataDir := fs.String("data-dir", defaultDataDir(), "data directory (must match server data-dir)")
	if err := fs.Parse(args); err != nil {
		return demoSeedOptions{}, 2
	}
	if strings.TrimSpace(*email) == "" {
		fmt.Fprintln(os.Stderr, "seed demo: --email is required")
		return demoSeedOptions{}, 2
	}
	return demoSeedOptions{email: *email, dataDir: *dataDir}, 0
}

// lookupTenantByEmail은 users 테이블에서 email → tenant_id를 조회합니다.
//
// 단순 SQL — admin_seed의 alreadySeeded와 같은 Bootstrap Tx 패턴.
func lookupTenantByEmail(ctx context.Context, p *Platform, email string) (storage.TenantID, error) {
	var tenantID storage.TenantID
	err := p.Storage.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx, `SELECT tenant_id FROM users WHERE email = ? LIMIT 1`, email)
		var s string
		if err := row.Scan(&s); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return nil // 빈 결과 → caller가 ""로 판단
			}
			// SQL "no rows" 에러는 driver별로 다르므로 일단 전파
			return nil
		}
		tenantID = storage.TenantID(s)
		return nil
	})
	return tenantID, err
}

// executeSeedDemo는 Fleet → Robots → Scan sessions를 시드합니다 (멱등).
func executeSeedDemo(ctx context.Context, p *Platform, tenantID storage.TenantID) (demoSeedOutput, int) {
	out := demoSeedOutput{
		PackID:     demoPackID,
		DriftRobot: demoRobotNames[0],
		DriftCheck: demoCheckMapped,
	}

	tenantCtx := storage.WithTenantID(ctx, tenantID)

	// 0) Pack stub 시드 (scan.StartScan이 packs FK를 검증하므로 minimal row 1개 필요).
	//    실 pack-tools 흐름은 별도 — 시연 데이터의 출처는 의미가 없음.
	if err := seedDemoPack(tenantCtx, p, tenantID); err != nil {
		fmt.Fprintf(os.Stderr, "seed demo: pack stub: %v\n", err)
		return out, 1
	}

	// 1) Fleet 시드 (이름 unique 제약 — 이미 존재하면 ListFleets로 룩업).
	var fleetID string
	err := p.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		fleets, e := p.Robot.ListFleets(ctx, tx)
		if e != nil {
			return fmt.Errorf("list fleets: %w", e)
		}
		for _, f := range fleets {
			if f.Name == "demo-fleet" {
				fleetID = f.ID
				out.WasExisting = true
				return nil
			}
		}
		f, e := p.Robot.CreateFleet(ctx, tx, robot.CreateFleetRequest{
			Name:        "demo-fleet",
			Description: "Phase 2 Exit demo fleet",
		})
		if e != nil {
			return fmt.Errorf("create fleet: %w", e)
		}
		fleetID = f.ID
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed demo: fleet: %v\n", err)
		return out, 1
	}
	out.FleetID = fleetID

	// 2) Robot × 3 시드 (이름 unique — 이미 존재하면 ListRobots에서 가져옴).
	var robotIDs []string
	err = p.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		existing, e := p.Robot.ListRobots(ctx, tx, fleetID)
		if e != nil {
			return fmt.Errorf("list robots: %w", e)
		}
		byName := make(map[string]string, len(existing))
		for _, r := range existing {
			byName[r.Name] = r.ID
		}

		for i, name := range demoRobotNames {
			if id, ok := byName[name]; ok {
				robotIDs = append(robotIDs, id)
				continue
			}
			res, e := p.Robot.CreateRobot(ctx, tx, robot.CreateRobotRequest{
				FleetID: fleetID,
				Name:    name,
				Host:    fmt.Sprintf("demo-host-%d.invalid", i+1),
				Port:    22,
				Material: robot.CredentialMaterial{
					Type:     robot.CredentialTypePassword,
					Username: "demo",
					Password: "demo-placeholder-password",
				},
				AuthType:    robot.AuthTypePassword,
				OSDistro:    "ubuntu-24.04",
				ROSDistro:   "jazzy",
				Tags:        []string{"phase2-demo"},
				Role:        "mobile",
				Criticality: robot.CriticalityMedium,
			})
			if e != nil {
				return fmt.Errorf("create robot %s: %w", name, e)
			}
			robotIDs = append(robotIDs, res.Robot.ID)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed demo: robots: %v\n", err)
		return out, 1
	}
	out.RobotIDs = robotIDs

	// 3) Scan sessions × 5 시드.
	//    각 session은 별 Tx에서 진행 — pending → running → results × 6 → completed.
	//    이미 존재하는 session은 ListSessions로 감지(개수 기준) — 중복 시드 방지.
	const targetSessionCount = 5
	var sessionIDs []string
	var existingSessionCount int
	err = p.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		sessions, e := p.Scan.ListSessions(ctx, tx, scan.ListSessionsFilter{FleetID: fleetID, Limit: 50})
		if e != nil {
			return fmt.Errorf("list sessions: %w", e)
		}
		existingSessionCount = len(sessions)
		for _, s := range sessions {
			sessionIDs = append(sessionIDs, s.ID)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed demo: sessions list: %v\n", err)
		return out, 1
	}

	if existingSessionCount >= targetSessionCount {
		out.SessionIDs = sessionIDs
		out.WasExisting = true
		return out, 0
	}

	// 부족분만 시드 (idempotent — 부분적 실패 후 재실행 시 잔여만 채움).
	for i := existingSessionCount; i < targetSessionCount; i++ {
		isDriftSession := i == targetSessionCount-1
		sessID, e := seedOneScanSession(tenantCtx, p, fleetID, robotIDs, isDriftSession)
		if e != nil {
			fmt.Fprintf(os.Stderr, "seed demo: session %d: %v\n", i, e)
			return out, 1
		}
		sessionIDs = append(sessionIDs, sessID)
	}
	out.SessionIDs = sessionIDs

	// 4) Insight detector 명시 트리거 (W3 EventBus 구독은 orchestrator 경유 — 본 seed는 직접 service
	//    호출이라 publish 안 됨). 시연용 1회 backfill.
	err = p.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := p.Insight.RunForFleet(ctx, tx, fleetID)
		return e
	})
	if err != nil {
		// 시드 자체는 성공이므로 warning only — Insight는 추후 :run endpoint로 재시도 가능.
		fmt.Fprintf(os.Stderr, "seed demo: warning — RunForFleet 실패: %v\n", err)
	}
	return out, 0
}

// seedDemoPack은 packs 테이블에 minimal stub row 1개를 INSERT합니다 (멱등).
//
// scan.StartScan의 assertPackAccessible이 packs row 존재만 검증하므로 metadata는 dummy로 충분.
// pack-tools 정식 흐름과 무관 — 시연 데이터 출처는 의미 없음.
func seedDemoPack(ctx context.Context, p *Platform, tenantID storage.TenantID) error {
	return p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		// 이미 존재 검사.
		row := tx.QueryRow(ctx, `SELECT 1 FROM packs WHERE id = ? LIMIT 1`, demoPackID)
		var n int
		if err := row.Scan(&n); err == nil && n == 1 {
			return nil
		}
		// dummy 32B manifest hash (zero) + minimal 필수 컬럼.
		_, err := tx.Exec(ctx, `
INSERT INTO packs(id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			demoPackID, string(tenantID),
			"phase2-demo", "v0.0.0", "demo",
			"demo-phase2-demo-v0.0.0",
			make([]byte, 32),
			"demo-stub",
			time.Now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}

		// scan_results.pack_check_id가 pack_checks(id) FK이므로 stub row 시드.
		// evaluation_rule은 JSON이라 minimal {"op":"equals","value":"ok"} 사용.
		for _, code := range []string{demoCheckMapped, demoCheckUnmappd} {
			ckID := fmt.Sprintf("ck_DEMO_%s", code)
			_, err := tx.Exec(ctx, `
INSERT INTO pack_checks(id, pack_id, check_id, title, description, severity, audit_command, evaluation_rule, rationale, fix_guidance)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				ckID, demoPackID, code,
				"demo check "+code, "Phase 2 demo placeholder",
				"medium", "true",
				`{"op":"equals","value":"ok"}`,
				"", "",
			)
			if err != nil {
				return fmt.Errorf("insert pack_check %s: %w", code, err)
			}
		}
		return nil
	})
}

// seedOneScanSession은 한 scan session을 단독 흐름으로 시드합니다 (FSM 전이 포함).
//
// pending → running → RecordResult × len(robots) × 2 → completed.
// driftMode=true면 robotIDs[0]의 demoCheckMapped만 outcome=fail, 나머지 모두 pass.
//
// 매 RecordResult가 진행률을 갱신하고 W3 구독자가 scan.completed 시 Insight 자동 생성.
func seedOneScanSession(ctx context.Context, p *Platform, fleetID string, robotIDs []string, driftMode bool) (string, error) {
	checks := []string{demoCheckMapped, demoCheckUnmappd}
	totalWork := len(robotIDs) * len(checks)

	// pending session 생성.
	var sessID string
	err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		s, e := p.Scan.StartScan(ctx, tx, scan.StartScanRequest{
			FleetID: fleetID,
			PackID:  demoPackID,
			Trigger: scan.TriggerManual,
			Total:   totalWork,
		})
		if e != nil {
			return e
		}
		sessID = s.ID
		// pending → running 전이.
		_, e = p.Scan.TransitionSession(ctx, tx, sessID, scan.StatusRunning, "")
		return e
	})
	if err != nil {
		return "", fmt.Errorf("start+running: %w", err)
	}

	// 결과 기록 + completed 전이.
	err = p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC()
		for _, robotID := range robotIDs {
			for _, checkCode := range checks {
				outcome := scan.OutcomePass
				reason := ""
				if driftMode && robotID == robotIDs[0] && checkCode == demoCheckMapped {
					outcome = scan.OutcomeFail
					reason = "demo drift trigger — Phase 2 시연용"
				}
				// CheckID는 텍스트 식별자 (CIS-1.1.1.1), PackCheckID는 pack_checks.id (ck_DEMO_...).
				ckID := fmt.Sprintf("ck_DEMO_%s", checkCode)
				_, e := p.Scan.RecordResult(ctx, tx, scan.RecordResultRequest{
					SessionID:   sessID,
					RobotID:     robotID,
					CheckID:     checkCode,
					PackCheckID: ckID,
					Outcome:     outcome,
					EvalReason:  reason,
					DurationMs:  10,
					ExecutedAt:  now,
				})
				if e != nil {
					return fmt.Errorf("record %s/%s: %w", robotID, checkCode, e)
				}
			}
		}
		// running → completed.
		_, e := p.Scan.TransitionSession(ctx, tx, sessID, scan.StatusCompleted, "")
		return e
	})
	if err != nil {
		return "", fmt.Errorf("record+complete: %w", err)
	}
	return sessID, nil
}
