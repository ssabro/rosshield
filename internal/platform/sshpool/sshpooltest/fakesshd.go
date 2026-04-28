// Package sshpooltest는 sshpool 통합 테스트용 in-proc fake SSH 서버를 제공합니다 (E6 R7-1).
//
// 표준 라이브러리 `net/http/httptest`와 같은 패턴 — 외부 패키지가 import해서 쓰는
// 테스트 헬퍼지만 _test.go에 갇히지 않은 일반 패키지입니다.
//
// 외부 의존: `golang.org/x/crypto/ssh.Server` 직접 사용 (gliderlabs·testcontainers 미사용 — R4 결정 C).
//
// 표면:
//
//	srv := sshpooltest.New(t, handler)
//	defer 자동 — t.Cleanup으로 등록.
//	srv.Host, srv.Port — 클라이언트 dial 주소
//	srv.HostKeyCallback() — ssh.HostKeyCallback (정확한 키 일치만 통과)
//	srv.ReceivedCmds() — 누적된 exec 명령 리스트
//
// handler는 클라이언트가 보낸 명령 string을 받아 ExecResponse를 반환합니다.
// Delay > 0이면 그만큼 기다린 뒤 결과 송신 — timeout 테스트용.
package sshpooltest

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

// ExecResponse는 fake SSHD가 클라이언트에 반환할 응답입니다.
type ExecResponse struct {
	Stdout, Stderr string
	ExitCode       int
	Delay          time.Duration
}

// ExecHandler는 클라이언트가 보낸 명령 string을 받아 응답을 반환하는 핸들러입니다.
type ExecHandler func(cmd string) ExecResponse

// FakeSSHD는 in-proc fake SSH 서버 인스턴스입니다.
type FakeSSHD struct {
	Host       string
	Port       int
	HostPubKey ssh.PublicKey

	listener net.Listener
	wg       sync.WaitGroup
	stop     chan struct{}

	mu           sync.Mutex
	receivedCmds []string
}

// ReceivedCmds는 fake가 지금까지 수신한 exec 명령 string 슬라이스를 반환합니다.
func (f *FakeSSHD) ReceivedCmds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.receivedCmds))
	copy(out, f.receivedCmds)
	return out
}

// New는 새 fake SSHD를 시작하고 t.Cleanup으로 정리를 등록합니다.
//
// handler가 nil이면 빈 ExecResponse(stdout/stderr 빈 string, exit 0)를 모든 명령에 반환합니다.
func New(t *testing.T, handler ExecHandler) *FakeSSHD {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("sshpooltest: GenerateKey: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("sshpooltest: NewSignerFromKey: %v", err)
	}
	hostPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("sshpooltest: NewPublicKey: %v", err)
	}

	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("sshpooltest: Listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)

	f := &FakeSSHD{
		Host:       addr.IP.String(),
		Port:       addr.Port,
		HostPubKey: hostPub,
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

func (f *FakeSSHD) acceptLoop(cfg *ssh.ServerConfig, handler ExecHandler) {
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

func (f *FakeSSHD) handleConn(c net.Conn, cfg *ssh.ServerConfig, handler ExecHandler) {
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

func (f *FakeSSHD) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, handler ExecHandler) {
	defer func() { _ = ch.Close() }()
	for req := range reqs {
		switch req.Type {
		case "exec":
			cmd := decodeExecPayload(req.Payload)
			f.mu.Lock()
			f.receivedCmds = append(f.receivedCmds, cmd)
			f.mu.Unlock()
			_ = req.Reply(true, nil)

			resp := ExecResponse{}
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

// HostKeyCallback는 fake SSHD의 호스트 키만 신뢰하는 ssh.HostKeyCallback을 반환합니다.
func (f *FakeSSHD) HostKeyCallback() ssh.HostKeyCallback {
	want := ssh.MarshalAuthorizedKey(f.HostPubKey)
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		got := ssh.MarshalAuthorizedKey(key)
		if string(want) != string(got) {
			return io.EOF // tester가 단순 에러 처리
		}
		return nil
	}
}
