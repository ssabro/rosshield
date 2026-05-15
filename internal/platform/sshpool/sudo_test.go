package sshpool_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/sshpool/sshpooltest"
)

// sudo_test.go — Target.SudoMode 단위 테스트 (scanrun SSH 통합 Stage 3).
//
// design doc `docs/design/notes/scanrun-ssh-integration-design.md` §6 Stage 3 검증:
//   - SudoNonInteractive 시 argv 앞에 "sudo -n --" prefix wrap
//   - SudoNone(zero-value) 시 argv 그대로 (기존 동작 호환)
//   - JoinArgv가 single-quote escape 적용 — fakesshd가 wrapped string 수신

func TestExecSudoNonInteractiveWrapsArgv(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{Stdout: "ok\n", ExitCode: 0}
	})

	ex := sshpool.New(sshpool.Deps{})
	_, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
		SudoMode:        sshpool.SudoNonInteractive,
	}, []string{"cat", "/etc/shadow"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	cmds := srv.ReceivedCmds()
	if len(cmds) != 1 {
		t.Fatalf("ReceivedCmds = %d, want 1", len(cmds))
	}
	got := cmds[0]
	// 기대: 'sudo' '-n' '--' 'cat' '/etc/shadow'
	wantPrefix := "'sudo' '-n' '--' "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("cmd = %q, want prefix %q", got, wantPrefix)
	}
	if !strings.Contains(got, "'cat'") || !strings.Contains(got, "'/etc/shadow'") {
		t.Errorf("cmd = %q, want argv 'cat'·'/etc/shadow' present", got)
	}
}

func TestExecSudoNoneDoesNotWrap(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{Stdout: "ok\n", ExitCode: 0}
	})

	ex := sshpool.New(sshpool.Deps{})
	_, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
		// SudoMode 미설정 — zero-value SudoNone.
	}, []string{"echo", "hello"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	cmds := srv.ReceivedCmds()
	if len(cmds) != 1 {
		t.Fatalf("ReceivedCmds = %d, want 1", len(cmds))
	}
	got := cmds[0]
	if strings.HasPrefix(got, "'sudo'") {
		t.Errorf("cmd = %q, want no sudo prefix (SudoNone)", got)
	}
	want := "'echo' 'hello'"
	if got != want {
		t.Errorf("cmd = %q, want %q", got, want)
	}
}

func TestExecSudoPreservesEscaping(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{Stdout: "ok\n", ExitCode: 0}
	})

	ex := sshpool.New(sshpool.Deps{})
	// argv에 single-quote 포함 — sudo wrap 후에도 escape 정확.
	_, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
		SudoMode:        sshpool.SudoNonInteractive,
	}, []string{"sh", "-c", "echo 'hi'"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	cmds := srv.ReceivedCmds()
	got := cmds[0]
	// sudo prefix 존재.
	if !strings.HasPrefix(got, "'sudo' '-n' '--' ") {
		t.Errorf("cmd = %q, want sudo prefix", got)
	}
	// single-quote escape 패턴 ('\'') 그대로 살아있어야 함 (POSIX 표준).
	if !strings.Contains(got, `'\''`) {
		t.Errorf("cmd = %q, want POSIX single-quote escape '\\''", got)
	}
}
