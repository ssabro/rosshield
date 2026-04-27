package sshpool_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ssabro/rosshield/internal/platform/sshpool"
)

func newExecutor() sshpool.Executor {
	return sshpool.New(sshpool.Deps{})
}

func dummyAuth() ssh.AuthMethod {
	return ssh.Password("ignored") // fakeSSHD는 NoClientAuth=true이라 실제로 무시됨
}

// E6.T2 — Exec가 fake sshd의 stdout/stderr/exit code를 정확히 반환.
func TestExecReturnsStdoutStderrExitCode(t *testing.T) {
	t.Parallel()
	srv := newFakeSSHD(t, func(cmd string) execResponse {
		return execResponse{
			Stdout:   "hello stdout\n",
			Stderr:   "hello stderr\n",
			ExitCode: 7,
		}
	})

	ex := newExecutor()
	res, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.host, Port: srv.port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.hostKeyCallback(),
	}, []string{"echo", "hello"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if string(res.Stdout) != "hello stdout\n" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "hello stdout\n")
	}
	if string(res.Stderr) != "hello stderr\n" {
		t.Errorf("Stderr = %q", res.Stderr)
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if res.Duration <= 0 {
		t.Error("Duration should be > 0")
	}
}

// E6.T3 — Exec가 timeout 도달 시 ctx.DeadlineExceeded 반환 + session close.
func TestExecTimeoutCancels(t *testing.T) {
	t.Parallel()
	srv := newFakeSSHD(t, func(cmd string) execResponse {
		return execResponse{
			Stdout:   "delayed",
			ExitCode: 0,
			Delay:    3 * time.Second, // timeout보다 큼
		}
	})

	ex := newExecutor()
	start := time.Now()
	res, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.host, Port: srv.port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.hostKeyCallback(),
	}, []string{"sleep", "10"}, 200*time.Millisecond)
	dur := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if dur >= 2*time.Second {
		t.Errorf("Exec returned after %v, want < 2s (timeout enforced)", dur)
	}
	// 부분 결과 — Stdout은 빈 byte slice 가능 (delay 중 송신 X).
	if res.Duration <= 0 {
		t.Error("partial result Duration should be > 0")
	}
}

// Exec ctx cancel도 timeout과 동일 동작.
func TestExecContextCancelStops(t *testing.T) {
	t.Parallel()
	srv := newFakeSSHD(t, func(cmd string) execResponse {
		return execResponse{Delay: 3 * time.Second}
	})

	ex := newExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := ex.Exec(ctx, sshpool.Target{
		Host: srv.host, Port: srv.port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.hostKeyCallback(),
	}, []string{"sleep", "10"}, 5*time.Second)
	dur := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if dur >= 2*time.Second {
		t.Errorf("Exec returned after %v, want < 2s", dur)
	}
}

// E6.T4 — argv가 shell metachar 해석 없이 그대로 전달.
func TestExecArgvNotShellParsed(t *testing.T) {
	t.Parallel()
	srv := newFakeSSHD(t, func(cmd string) execResponse {
		// fake가 받은 cmd를 그대로 stdout에 echo — 검증용
		return execResponse{Stdout: cmd}
	})

	ex := newExecutor()
	// $HOME, &&, |, > 등 shell metachar가 literal로 전달돼야.
	argv := []string{"echo", "$HOME", "&&", "rm", "-rf", "/tmp"}
	res, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.host, Port: srv.port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.hostKeyCallback(),
	}, argv, 5*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	got := string(res.Stdout)
	want := `'echo' '$HOME' '&&' 'rm' '-rf' '/tmp'`
	if got != want {
		t.Errorf("cmd = %q\nwant %q", got, want)
	}
	// fakeSSHD가 받은 cmd 검증.
	cmds := srv.ReceivedCmds()
	if len(cmds) != 1 || cmds[0] != want {
		t.Errorf("ReceivedCmds = %v, want [%q]", cmds, want)
	}
}

func TestJoinArgvEscapesSingleQuote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"echo", "hi"}, `'echo' 'hi'`},
		{[]string{"echo", "it's me"}, `'echo' 'it'\''s me'`},
		{[]string{"bash", "-c", "ls /etc"}, `'bash' '-c' 'ls /etc'`},
		{[]string{}, ""},
	}
	for _, c := range cases {
		got := sshpool.JoinArgv(c.argv)
		if got != c.want {
			t.Errorf("JoinArgv(%v) = %q, want %q", c.argv, got, c.want)
		}
	}
}

func TestExecRejectsEmptyArgv(t *testing.T) {
	t.Parallel()
	srv := newFakeSSHD(t, nil)
	ex := newExecutor()
	_, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.host, Port: srv.port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.hostKeyCallback(),
	}, nil, 0)
	if !errors.Is(err, sshpool.ErrEmptyArgv) {
		t.Errorf("err = %v, want ErrEmptyArgv", err)
	}
}

func TestExecValidatesTarget(t *testing.T) {
	t.Parallel()
	ex := newExecutor()
	cases := []struct {
		name   string
		target sshpool.Target
		want   error
	}{
		{"empty host", sshpool.Target{Port: 22, Username: "u", Auth: dummyAuth(), HostKeyCallback: ssh.InsecureIgnoreHostKey()}, sshpool.ErrEmptyHost},
		{"invalid port 0", sshpool.Target{Host: "h", Port: 0, Username: "u", Auth: dummyAuth(), HostKeyCallback: ssh.InsecureIgnoreHostKey()}, sshpool.ErrInvalidPort},
		{"invalid port 99999", sshpool.Target{Host: "h", Port: 99999, Username: "u", Auth: dummyAuth(), HostKeyCallback: ssh.InsecureIgnoreHostKey()}, sshpool.ErrInvalidPort},
		{"empty user", sshpool.Target{Host: "h", Port: 22, Auth: dummyAuth(), HostKeyCallback: ssh.InsecureIgnoreHostKey()}, sshpool.ErrEmptyUser},
		{"nil auth", sshpool.Target{Host: "h", Port: 22, Username: "u", HostKeyCallback: ssh.InsecureIgnoreHostKey()}, sshpool.ErrAuthRequired},
		{"nil host key callback", sshpool.Target{Host: "h", Port: 22, Username: "u", Auth: dummyAuth()}, sshpool.ErrHostKeyMissing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ex.Exec(context.Background(), c.target, []string{"echo"}, 0)
			if !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestExecTruncatesLargeStdout(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("X", 1000)
	srv := newFakeSSHD(t, func(cmd string) execResponse {
		return execResponse{Stdout: big, ExitCode: 0}
	})

	ex := sshpool.New(sshpool.Deps{MaxStdoutBytes: 100, MaxStderrBytes: 100})
	res, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.host, Port: srv.port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.hostKeyCallback(),
	}, []string{"large"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(res.Stdout) != 100 {
		t.Errorf("len(Stdout) = %d, want 100 (truncated)", len(res.Stdout))
	}
	if !res.Truncated {
		t.Error("Truncated should be true")
	}
}

func TestExecRejectsWrongHostKey(t *testing.T) {
	t.Parallel()
	srv := newFakeSSHD(t, func(cmd string) execResponse { return execResponse{} })

	// Mismatched host key callback — 항상 에러 반환.
	ex := newExecutor()
	_, err := ex.Exec(context.Background(), sshpool.Target{
		Host: srv.host, Port: srv.port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: ssh.FixedHostKey(srv.hostPubKey), // 정상 OK
	}, []string{"echo", "ok"}, 5*time.Second)
	if err != nil {
		t.Fatalf("matched host key should succeed: %v", err)
	}

	// 다른 호스트 키 (FixedHostKey에 dummy)
	dummyPub := dummyHostKey(t)
	_, err = ex.Exec(context.Background(), sshpool.Target{
		Host: srv.host, Port: srv.port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: ssh.FixedHostKey(dummyPub),
	}, []string{"echo"}, 5*time.Second)
	if err == nil {
		t.Error("mismatched host key should fail")
	}
}

func dummyHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	srv := newFakeSSHD(t, nil) // 별도 fake = 다른 키
	return srv.hostPubKey
}
