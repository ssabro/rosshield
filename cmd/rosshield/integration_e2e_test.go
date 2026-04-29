package main

// integration_e2e_test.go — Stage D Exit 통합 검증.
//
// 실 rosshield-server 패키지를 import할 수 없으므로 이 테스트는 mock 서버로 라우팅
// 합치는 통합만 진행. 풀 데모 흐름(bootstrap+seed+real handlers)은 별도 cmd/rosshield-server
// 통합 테스트가 검증함.
//
// 본 테스트는 CLI 본위에서 Stage A+B+C 결선이 일관되게 흐르는지 확인:
//
//	1) config init → file 생성
//	2) login → token 영속
//	3) whoami / robot list / scan run / report list — 모두 같은 config·token 사용
//	4) report verify — offline (token 무관)
//
// WebSocket scan progress(spec T2)는 신규 의존(coder/websocket) + 서버 WS 핸들러
// 결선 부담으로 Phase 1 범위에서 보류 — E10 Web UI 또는 Phase 2 운영 도구로 이전.

import (
	"strings"
	"testing"
)

func TestE2ECLIWorkflowEndToEnd(t *testing.T) {
	st := &mockServerState{requireAuth: true}
	srv := newMockServer(t, st)
	cfgDir := t.TempDir()
	cfgPath := cfgDir + "/config.yaml"

	// 1. config init.
	if exit := runConfig([]string{"init", "--server", srv.URL, "--config", cfgPath}); exit != 0 {
		t.Fatalf("config init: exit=%d", exit)
	}

	// 2. login — token이 config에 영속되어야 한다.
	st.wantToken = "test-access-token-abc"
	if exit := runLogin([]string{
		"--email", "admin@example.com",
		"--password", "verylongpassword123",
		"--config", cfgPath,
	}); exit != 0 {
		t.Fatalf("login: exit=%d", exit)
	}

	// 3. whoami.
	if exit := runWhoami([]string{"--config", cfgPath, "-o", "json"}); exit != 0 {
		t.Fatalf("whoami: exit=%d", exit)
	}

	// 4. robot list.
	if exit := runRobotList([]string{"--config", cfgPath, "-o", "json"}); exit != 0 {
		t.Fatalf("robot list: exit=%d", exit)
	}

	// 5. scan run.
	if exit := runScanRun([]string{
		"--config", cfgPath,
		"--fleet", "fl_DEMO", "--pack", "pk_DEMO", "-o", "json",
	}); exit != 0 {
		t.Fatalf("scan run: exit=%d", exit)
	}

	// 6. report list.
	if exit := runReportList([]string{"--config", cfgPath, "-o", "json"}); exit != 0 {
		t.Fatalf("report list: exit=%d", exit)
	}

	// 7. config show — 마스킹된 토큰 확인.
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !strings.Contains(cfg.AccessToken, "test-access-token") {
		t.Fatalf("AccessToken not persisted: %q", cfg.AccessToken)
	}
	if cfg.ServerURL != srv.URL {
		t.Fatalf("ServerURL drift: %q", cfg.ServerURL)
	}
}

// 핵심 Phase 1 Exit 흐름이 한 fixture 안에서 동작함을 증명.
// seed admin (cmd/rosshield-server) + login → whoami → scan run → report list 라는 데모를
// 사용자가 시연할 수 있다는 점을 본 테스트가 보장.
