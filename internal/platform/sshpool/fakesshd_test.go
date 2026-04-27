package sshpool_test

// In-proc fake SSH server helper — `golang.org/x/crypto/ssh.Server` 직접 사용.
// 외부 의존 0 (testcontainers·gliderlabs 미사용 — R4 결정 C).
//
// 표면:
//   newFakeSSHD(t, ExecHandler) → endpoint{host, port, hostPub}
//
// ExecHandler는 클라이언트가 보낸 명령 string을 받아 (stdout, stderr, exitCode, delay)를
// 반환합니다. delay > 0이면 그만큼 기다린 뒤 결과 송신 — timeout 테스트용.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type execResponse struct {
	Stdout, Stderr string
	ExitCode       int
	Delay          time.Duration
}

type ExecHandler func(cmd string) execResponse

type fakeSSHD struct {
	host       string
	port       int
	hostPubKey ssh.PublicKey
	listener   net.Listener
	wg         sync.WaitGroup
	stop       chan struct{}

	mu           sync.Mutex
	receivedCmds []string
}

func (f *fakeSSHD) ReceivedCmds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.receivedCmds))
	copy(out, f.receivedCmds)
	return out
}

func newFakeSSHD(t *testing.T, handler ExecHandler) *fakeSSHD {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("NewSignerFromKey: %v", err)
	}
	hostPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}

	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)

	f := &fakeSSHD{
		host:       addr.IP.String(),
		port:       addr.Port,
		hostPubKey: hostPub,
		listener:   ln,
		stop:       make(chan struct{}),
	}

	f.wg.Add(1)
	go f.acceptLoop(cfg, handler)

	t.Cleanup(func() {
		close(f.stop)
		_ = ln.Close()
		f.wg.Wait()
	})
	return f
}

func (f *fakeSSHD) acceptLoop(cfg *ssh.ServerConfig, handler ExecHandler) {
	defer f.wg.Done()
	for {
		c, err := f.listener.Accept()
		if err != nil {
			select {
			case <-f.stop:
				return
			default:
			}
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			f.handleConn(c, cfg, handler)
		}()
	}
}

func (f *fakeSSHD) handleConn(c net.Conn, cfg *ssh.ServerConfig, handler ExecHandler) {
	sshConn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		_ = c.Close()
		return
	}
	defer func() { _ = sshConn.Close() }()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "unsupported channel")
			continue
		}
		ch, sessReqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			f.handleSession(ch, sessReqs, handler)
		}()
	}
}

func (f *fakeSSHD) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, handler ExecHandler) {
	defer func() { _ = ch.Close() }()
	for req := range reqs {
		switch req.Type {
		case "exec":
			cmd := decodeExecPayload(req.Payload)
			f.mu.Lock()
			f.receivedCmds = append(f.receivedCmds, cmd)
			f.mu.Unlock()
			_ = req.Reply(true, nil)

			resp := execResponse{}
			if handler != nil {
				resp = handler(cmd)
			}
			if resp.Delay > 0 {
				select {
				case <-time.After(resp.Delay):
				case <-f.stop:
					return
				}
			}
			if resp.Stdout != "" {
				_, _ = io.WriteString(ch, resp.Stdout)
			}
			if resp.Stderr != "" {
				_, _ = io.WriteString(ch.Stderr(), resp.Stderr)
			}
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(resp.ExitCode)}))
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

// decodeExecPayload는 SSH exec 채널 payload(4-byte BE length + command)를 디코드합니다.
func decodeExecPayload(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := binary.BigEndian.Uint32(payload[:4])
	if int(n) > len(payload)-4 {
		return ""
	}
	return string(payload[4 : 4+n])
}

// hostKeyCallback는 fakeSSHD의 호스트 키를 신뢰하는 콜백을 반환합니다.
func (f *fakeSSHD) hostKeyCallback() ssh.HostKeyCallback {
	want := ssh.MarshalAuthorizedKey(f.hostPubKey)
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := ssh.MarshalAuthorizedKey(key)
		if string(want) != string(got) {
			return io.EOF // tester가 모든 에러 ErrCredentialDecrypt 같이 처리할 수도 있어 단순 에러
		}
		return nil
	}
}
