package main

// scanrun_e2e_test.go — E12 Stage 8 cmd-level e2e.
//
// 시나리오:
//   1. Bootstrap (자동 builtin pack seed)
//   2. fakesshd 1개 시작 (모든 cmd "** PASS **" stdout)
//   3. tenant·admin 시드 (tenant.Service.Create)
//   4. fleet 시드 (robot.Service.CreateFleet)
//   5. robot 시드 (robot.Service.CreateRobot, host=fakesshd)
//   6. builtin pack의 packID 추출 (Benchmark.GetPackByKey)
//   7. login → JWT token
//   8. POST /api/v1/scans { fleetId, packId } → 201 sessionId
//   9. session 완주 폴링 (최대 10초)
//  10. 검증: status=completed, Outcome PASS, audit chain entry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/sshpool/sshpooltest"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// TestScanrunE2E_BuiltinPackOneRobotOneCheck —
// Bootstrap → POST /api/v1/scans → 비동기 Orchestrator → fakesshd → DB → 검증.
func TestScanrunE2E_BuiltinPackOneRobotOneCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test — skipped in -short mode")
	}

	// 1. Bootstrap.
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})

	// 2. fakesshd 시작 — 모든 cmd "** PASS **" stdout.
	fake := sshpooltest.New(t, func(_ string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{Stdout: "** PASS **\n"}
	})

	// 3. tenant + admin 시드.
	const (
		adminEmail = "e2e-admin@example.com"
		adminPw    = "verylongpassword123"
	)
	var (
		tenantID storage.TenantID
		fleetID  string
		robotID  string
	)
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		r, e := p.Tenant.Create(ctx, tx, tenant.CreateRequest{
			Name:             "E2E Tenant",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       adminEmail,
			AdminPassword:    adminPw,
			AdminDisplayName: "E2E Admin",
		})
		if e != nil {
			return e
		}
		tenantID = r.Tenant.ID
		return nil
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	// 4. fleet 시드.
	tenantCtx := storage.WithTenantID(context.Background(), tenantID)
	if err := p.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		fl, e := p.Robot.CreateFleet(ctx, tx, robot.CreateFleetRequest{Name: "E2E Fleet"})
		if e != nil {
			return e
		}
		fleetID = fl.ID
		return nil
	}); err != nil {
		t.Fatalf("create fleet: %v", err)
	}

	// 5. robot 시드 (host=fakesshd).
	if err := p.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		res, e := p.Robot.CreateRobot(ctx, tx, robot.CreateRobotRequest{
			FleetID:  fleetID,
			Name:     "fake-robot",
			Host:     fake.Host,
			Port:     fake.Port,
			AuthType: robot.AuthTypePassword,
			Material: robot.CredentialMaterial{
				Type:     robot.CredentialTypePassword,
				Username: "u",
				Password: "ignored",
			},
		})
		if e != nil {
			return e
		}
		robotID = res.Robot.ID
		return nil
	}); err != nil {
		t.Fatalf("create robot: %v", err)
	}
	_ = robotID

	// 6. builtin pack(systemTenant) packID 추출 — bootstrap이 자동 install했음.
	systemCtx := storage.WithTenantID(context.Background(), storage.TenantID("system"))
	var packID string
	if err := p.Storage.Tx(systemCtx, func(ctx context.Context, tx storage.Tx) error {
		packs, e := p.Benchmark.ListPacks(ctx, tx, storage.TenantID("system"))
		if e != nil {
			return e
		}
		if len(packs) == 0 {
			t.Skip("no built-in packs seeded — run 'make pack-archive'")
		}
		// 가장 작은 pack(checks 적은) 골라 빠른 e2e — alphabet 첫 = cis (큼). ros2가 더 큼.
		// 실은 둘 다 큼(312·329) — Total=312 또는 329 work item. 1 robot이라 1 cycle.
		packID = packs[0].ID
		return nil
	}); err != nil {
		t.Fatalf("fetch builtin pack: %v", err)
	}

	// 7. mux + login.
	mux := newMux(p)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	loginBody, _ := json.Marshal(map[string]string{
		"email": adminEmail, "password": adminPw,
	})
	loginResp, err := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer func() { _ = loginResp.Body.Close() }()
	if loginResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(loginResp.Body)
		t.Fatalf("login status %d: %s", loginResp.StatusCode, string(body))
	}
	var loginOut struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginOut); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if loginOut.AccessToken == "" {
		t.Fatal("login: empty accessToken")
	}

	// 8. POST /api/v1/scans.
	scanBody, _ := json.Marshal(map[string]interface{}{
		"fleetId": fleetID,
		"packId":  packID,
		"trigger": "manual",
	})
	scanReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/scans", bytes.NewReader(scanBody))
	scanReq.Header.Set("Authorization", "Bearer "+loginOut.AccessToken)
	scanReq.Header.Set("Content-Type", "application/json")
	scanResp, err := http.DefaultClient.Do(scanReq)
	if err != nil {
		t.Fatalf("POST scan: %v", err)
	}
	defer func() { _ = scanResp.Body.Close() }()
	if scanResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(scanResp.Body)
		t.Fatalf("create scan status %d: %s", scanResp.StatusCode, string(body))
	}
	var scanOut struct {
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(scanResp.Body).Decode(&scanOut); err != nil {
		t.Fatalf("decode scan: %v", err)
	}
	if scanOut.SessionID == "" {
		t.Fatal("create scan: empty sessionId")
	}
	if scanOut.Status != "pending" {
		t.Errorf("initial status = %q, want pending (async run not yet kicked)", scanOut.Status)
	}
	sessionID := scanOut.SessionID

	// 9. session 완주 폴링 — pack에 따라 다름. cis=312, ros2=329 work items.
	// fakesshd 단순 응답이지만 실 SSH handshake × N → ~10초 상한 (1 robot × N checks).
	deadline := time.Now().Add(60 * time.Second)
	var finalSession scan.ScanSession
	for time.Now().Before(deadline) {
		var s scan.ScanSession
		err := p.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
			ss, e := p.Scan.GetSession(ctx, tx, sessionID)
			s = ss
			return e
		})
		if err != nil {
			t.Fatalf("reload session: %v", err)
		}
		if s.Status == scan.StatusCompleted ||
			s.Status == scan.StatusFailed ||
			s.Status == scan.StatusCancelled {
			finalSession = s
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if finalSession.ID == "" {
		t.Fatalf("session did not terminate within deadline (last status: pending or running)")
	}

	// 10. 검증.
	if finalSession.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed", finalSession.Status)
	}
	if finalSession.Progress.Total == 0 {
		t.Error("Total = 0, want > 0 (pack should have checks)")
	}
	if finalSession.Progress.Completed != finalSession.Progress.Total {
		t.Errorf("Completed = %d, Total = %d (incomplete)", finalSession.Progress.Completed, finalSession.Progress.Total)
	}
	// 모든 cmd "** PASS **" 응답 + selftest fixture 패턴 매칭이라 PASS 비율 매우 높음.
	// 단 일부 check는 다른 marker(예: degraded check가 ** PASS **를 contains 안 하는 경우) → FAIL.
	// 단순 검증: Failed가 Total과 같지 않으면 = 일부는 PASS = 결선 동작.
	if finalSession.Progress.Failed == finalSession.Progress.Total {
		t.Errorf("Failed = %d == Total = %d (no PASS at all — Orchestrator wiring suspicious)",
			finalSession.Progress.Failed, finalSession.Progress.Total)
	}

	// fakesshd 수신 cmd 개수 = Total과 일치 (1 robot × N checks).
	cmds := fake.ReceivedCmds()
	if len(cmds) != finalSession.Progress.Total {
		t.Errorf("fakesshd received %d cmds, want %d (Total)", len(cmds), finalSession.Progress.Total)
	}
	// 모든 cmd가 'bash' 인자 escape 포함 (sshpool.JoinArgv 결과).
	bashCount := 0
	for _, c := range cmds {
		if strings.Contains(c, "'bash'") {
			bashCount++
		}
	}
	if bashCount != len(cmds) {
		t.Errorf("only %d/%d cmds contain 'bash' quoting", bashCount, len(cmds))
	}

	// 11. audit chain entry — scan.started + scan.completed.
	var auditActions []string
	if err := p.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		rows, e := tx.Query(ctx, `SELECT action FROM audit_entries WHERE target_type=? AND target_id=? ORDER BY seq ASC`,
			"scan_session", sessionID)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var a string
			if e := rows.Scan(&a); e != nil {
				return e
			}
			auditActions = append(auditActions, a)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(auditActions) < 2 {
		t.Errorf("audit actions = %v, want at least scan.started + scan.completed", auditActions)
	}

}
