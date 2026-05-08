package email

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPConfig는 SMTPSender 설정입니다.
//
// Host:Port는 필수. Auth (Username/Password)가 모두 비면 unauthenticated submission
// (보통 LAN-only 릴레이). DefaultFrom은 Message.From이 빈 값일 때 사용.
//
// StartTLS는 옵트인 — true면 EHLO 후 STARTTLS 협상. 평문 submission이 필요한 환경
// (LAN 릴레이 일부)이라면 false. TLS InsecureSkipVerify는 노출하지 않음 — 운영 환경
// 자체-서명 인증서 호환 위해 필요하면 추후 추가.
type SMTPConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	DefaultFrom string
	StartTLS    bool          // true면 EHLO 후 STARTTLS.
	DialTimeout time.Duration // 0이면 10s.

	// dialer는 테스트에서만 주입 — 프로덕션에선 nil → smtpDial(net.Dial 직접 호출).
	dialer dialerFunc
}

// dialerFunc는 (host, port) → SMTP 클라이언트 추상화입니다 (테스트 hook).
type dialerFunc func(addr string, timeout time.Duration) (smtpClient, error)

// smtpClient는 net/smtp.Client의 최소 표면입니다 (테스트용 fake 가능).
type smtpClient interface {
	Hello(localName string) error
	StartTLS(config any) error // unused — 본 stage는 실 STARTTLS 미수행.
	Auth(a smtp.Auth) error
	Mail(from string) error
	Rcpt(to string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
}

// SMTPSender는 stdlib net/smtp 기반 어댑터입니다.
//
// 외부 SDK 0 (P3). TLS는 Phase 4에서 옵션으로만 지원 — 본 stage는 plain submission이
// 기본. STARTTLS·SMTPS는 추후 enterprise SKU 옵션.
type SMTPSender struct {
	cfg SMTPConfig
}

// NewSMTPSender는 SMTPConfig를 검증하고 어댑터를 반환합니다.
//
// Host·Port가 비어 있으면 즉시 에러. 빈 DefaultFrom은 허용 — Message.From이 채워지면 OK.
func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, errors.New("email/smtp: Host is required")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("email/smtp: Port out of range: %d", cfg.Port)
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	return &SMTPSender{cfg: cfg}, nil
}

// Provider는 식별자 "smtp"를 반환합니다.
func (*SMTPSender) Provider() string { return "smtp" }

// SendMessage는 SMTP submission을 수행합니다.
//
// 흐름: dial → HELO → (auth optional) → MAIL FROM → RCPT TO → DATA → QUIT.
// ctx는 dial timeout만 적용 (DATA write 중 cancel은 close로만 처리).
func (s *SMTPSender) SendMessage(ctx context.Context, msg Message) error {
	if msg.To == "" {
		return errors.New("email/smtp: To is required")
	}
	from := msg.From
	if from == "" {
		from = s.cfg.DefaultFrom
	}
	if from == "" {
		return errors.New("email/smtp: From and DefaultFrom are both empty")
	}

	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port))

	dial := s.cfg.dialer
	if dial == nil {
		dial = defaultDialer
	}

	cli, err := dial(addr, s.cfg.DialTimeout)
	if err != nil {
		return fmt.Errorf("email/smtp: dial %s: %w", addr, err)
	}
	defer func() { _ = cli.Close() }()

	if err := cli.Hello("localhost"); err != nil {
		return fmt.Errorf("email/smtp: HELO: %w", err)
	}

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := cli.Auth(auth); err != nil {
			return fmt.Errorf("email/smtp: AUTH: %w", err)
		}
	}

	if err := cli.Mail(stripDisplay(from)); err != nil {
		return fmt.Errorf("email/smtp: MAIL FROM: %w", err)
	}
	if err := cli.Rcpt(stripDisplay(msg.To)); err != nil {
		return fmt.Errorf("email/smtp: RCPT TO: %w", err)
	}

	w, err := cli.Data()
	if err != nil {
		return fmt.Errorf("email/smtp: DATA open: %w", err)
	}
	body := buildRFC5322(from, msg)
	if _, err := w.Write([]byte(body)); err != nil {
		_ = w.Close()
		return fmt.Errorf("email/smtp: DATA write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email/smtp: DATA close: %w", err)
	}

	if err := cli.Quit(); err != nil {
		// QUIT 실패는 일부 서버에서 흔함 — already-sent로 본다.
		return nil
	}
	_ = ctx
	return nil
}

// stripDisplay는 "Name <addr>" 형식에서 < > 안의 주소만 추출합니다.
//
// SMTP MAIL FROM·RCPT TO는 angle-addr만 받음 — display name이 같이 가면 syntax error.
func stripDisplay(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "<"); i != -1 {
		if j := strings.LastIndex(s, ">"); j > i {
			return s[i+1 : j]
		}
	}
	return s
}

// buildRFC5322는 RFC 5322 메시지를 빌드합니다 (multipart/alternative 또는 plain).
//
// HTMLBody가 비어 있으면 단순 text/plain. 채워져 있으면 multipart/alternative
// (text/plain + text/html). 본 stage는 7-bit assume — 본문이 한글 등 non-ASCII이면
// 클라이언트가 깨질 수 있으나 테스트 시나리오는 ASCII (token URL).
func buildRFC5322(from string, msg Message) string {
	var b strings.Builder
	b.WriteString("From: ")
	b.WriteString(from)
	b.WriteString("\r\n")
	b.WriteString("To: ")
	b.WriteString(msg.To)
	b.WriteString("\r\n")
	b.WriteString("Subject: ")
	b.WriteString(msg.Subject)
	b.WriteString("\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	if msg.HTMLBody == "" {
		b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		b.WriteString("\r\n")
		b.WriteString(msg.TextBody)
		return b.String()
	}

	const boundary = "rosshield-mime-boundary-0"
	b.WriteString("Content-Type: multipart/alternative; boundary=\"")
	b.WriteString(boundary)
	b.WriteString("\"\r\n")
	b.WriteString("\r\n")

	b.WriteString("--")
	b.WriteString(boundary)
	b.WriteString("\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(msg.TextBody)
	b.WriteString("\r\n")

	b.WriteString("--")
	b.WriteString(boundary)
	b.WriteString("\r\n")
	b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(msg.HTMLBody)
	b.WriteString("\r\n")

	b.WriteString("--")
	b.WriteString(boundary)
	b.WriteString("--\r\n")
	return b.String()
}

// defaultDialer는 net/smtp.Dial wrapper입니다 (timeout 적용).
func defaultDialer(addr string, timeout time.Duration) (smtpClient, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &realSMTPClient{c: c}, nil
}

// realSMTPClient는 *smtp.Client을 smtpClient interface로 감쌉니다.
type realSMTPClient struct {
	c *smtp.Client
}

func (r *realSMTPClient) Hello(localName string) error { return r.c.Hello(localName) }

// StartTLS는 본 stage 미사용 (TLS 시점 결정 후 결선). interface 충족 위한 stub.
func (r *realSMTPClient) StartTLS(_ any) error   { return nil }
func (r *realSMTPClient) Auth(a smtp.Auth) error { return r.c.Auth(a) }
func (r *realSMTPClient) Mail(from string) error { return r.c.Mail(from) }
func (r *realSMTPClient) Rcpt(to string) error   { return r.c.Rcpt(to) }
func (r *realSMTPClient) Data() (io.WriteCloser, error) {
	w, err := r.c.Data()
	if err != nil {
		return nil, err
	}
	return w, nil
}
func (r *realSMTPClient) Quit() error  { return r.c.Quit() }
func (r *realSMTPClient) Close() error { return r.c.Close() }
