package email_test

// email_test.go — O6 invite email 어댑터 단위 테스트.
//
// 시나리오:
//   T1 TestNoopSenderWritesJSONLine
//      - NoopSender.SendMessage → 주입된 writer에 JSON 한 줄 + 항상 nil err.
//      - body 자체는 출력에 포함하지 않음 (token leak 방지).
//   T2 TestNoopSenderRequiresTo
//      - To가 비면 에러.
//   T3 TestSMTPSenderConstructorValidation
//      - Host 빈 값 / Port 범위 outside → 에러.
//   T4 TestSMTPSenderSendsViaInjectedDialer
//      - dialer hook으로 fake client 주입 → MAIL FROM·RCPT TO·DATA 흐름 모두 호출 + body가
//        Subject/HTMLBody 포함.
//   T5 TestSMTPSenderUsesDefaultFrom
//      - Message.From이 비고 cfg.DefaultFrom이 채워지면 그것이 MAIL FROM에 사용.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/email"
)

// === T1: noop writes JSON line ===

func TestNoopSenderWritesJSONLine(t *testing.T) {
	var buf bytes.Buffer
	fixedNow := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	s := email.NewNoopSenderWith(&buf, func() time.Time { return fixedNow })

	if got := s.Provider(); got != "noop" {
		t.Errorf("Provider() = %q, want noop", got)
	}

	err := s.SendMessage(context.Background(), email.Message{
		From:     "noreply@example.com",
		To:       "user@acme.test",
		Subject:  "Welcome",
		TextBody: "secret-token-must-not-appear",
		HTMLBody: "<a>secret-token-must-not-appear</a>",
	})
	if err != nil {
		t.Fatalf("SendMessage err = %v, want nil", err)
	}

	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output not newline-terminated: %q", out)
	}
	if strings.Contains(out, "secret-token-must-not-appear") {
		t.Errorf("noop output leaked body: %q", out)
	}

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
		t.Fatalf("output not valid JSON: %v -- %q", err, out)
	}
	if rec["provider"] != "noop" {
		t.Errorf("provider = %v, want noop", rec["provider"])
	}
	if rec["to"] != "user@acme.test" {
		t.Errorf("to = %v", rec["to"])
	}
	if rec["subject"] != "Welcome" {
		t.Errorf("subject = %v", rec["subject"])
	}
}

// === T2: noop requires To ===

func TestNoopSenderRequiresTo(t *testing.T) {
	s := email.NewNoopSenderWith(&bytes.Buffer{}, nil)
	err := s.SendMessage(context.Background(), email.Message{
		From:     "noreply@example.com",
		Subject:  "Hi",
		TextBody: "x",
	})
	if err == nil {
		t.Fatal("expected error for empty To")
	}
}

// === T3: SMTP constructor validation ===

func TestSMTPSenderConstructorValidation(t *testing.T) {
	cases := []struct {
		name string
		cfg  email.SMTPConfig
	}{
		{"empty host", email.SMTPConfig{Host: "", Port: 25}},
		{"port zero", email.SMTPConfig{Host: "smtp.example.com", Port: 0}},
		{"port negative", email.SMTPConfig{Host: "smtp.example.com", Port: -1}},
		{"port too big", email.SMTPConfig{Host: "smtp.example.com", Port: 70000}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, err := email.NewSMTPSender(c.cfg)
			if err == nil {
				t.Errorf("expected error for cfg=%+v", c.cfg)
			}
		})
	}

	// happy path
	if _, err := email.NewSMTPSender(email.SMTPConfig{Host: "smtp.example.com", Port: 587}); err != nil {
		t.Errorf("happy path err = %v", err)
	}
}

// === fakeSMTPClient: smtpClient interface 만족, 호출을 record ===

type fakeSMTPClient struct {
	helloName string
	authCalls int
	mailFrom  string
	rcptTo    string
	dataBuf   bytes.Buffer
	quitCalls int
	closeCnt  int
}

func (f *fakeSMTPClient) Hello(localName string) error { f.helloName = localName; return nil }
func (f *fakeSMTPClient) StartTLS(_ any) error         { return nil }
func (f *fakeSMTPClient) Auth(_ smtp.Auth) error       { f.authCalls++; return nil }
func (f *fakeSMTPClient) Mail(from string) error       { f.mailFrom = from; return nil }
func (f *fakeSMTPClient) Rcpt(to string) error         { f.rcptTo = to; return nil }
func (f *fakeSMTPClient) Data() (io.WriteCloser, error) {
	return &fakeDataWriter{buf: &f.dataBuf}, nil
}
func (f *fakeSMTPClient) Quit() error  { f.quitCalls++; return nil }
func (f *fakeSMTPClient) Close() error { f.closeCnt++; return nil }

type fakeDataWriter struct {
	buf *bytes.Buffer
}

func (w *fakeDataWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *fakeDataWriter) Close() error                { return nil }

// === T4: SMTP sends via injected dialer ===

func TestSMTPSenderSendsViaInjectedDialer(t *testing.T) {
	fake := &fakeSMTPClient{}
	cfg := email.SMTPConfig{
		Host:        "smtp.example.com",
		Port:        587,
		Username:    "user",
		Password:    "pw",
		DefaultFrom: "noreply@example.com",
	}
	// 테스트 hook 주입.
	email.SetDialerForTest(&cfg, func(_ string, _ time.Duration) (any, error) {
		return fake, nil
	})

	s, err := email.NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}

	msg := email.Message{
		From:     "Bob <bob@acme.test>",
		To:       "Alice <alice@acme.test>",
		Subject:  "Invitation",
		TextBody: "Click https://app.example.com/invitations/accept/TOKEN123",
		HTMLBody: "<a href=\"https://app.example.com/invitations/accept/TOKEN123\">Accept</a>",
	}
	if err := s.SendMessage(context.Background(), msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if fake.authCalls != 1 {
		t.Errorf("authCalls = %d, want 1", fake.authCalls)
	}
	if fake.mailFrom != "bob@acme.test" {
		t.Errorf("mailFrom = %q, want bob@acme.test", fake.mailFrom)
	}
	if fake.rcptTo != "alice@acme.test" {
		t.Errorf("rcptTo = %q, want alice@acme.test", fake.rcptTo)
	}
	if fake.quitCalls != 1 {
		t.Errorf("quitCalls = %d, want 1", fake.quitCalls)
	}

	body := fake.dataBuf.String()
	if !strings.Contains(body, "Subject: Invitation") {
		t.Errorf("body missing Subject header: %q", body)
	}
	if !strings.Contains(body, "TOKEN123") {
		t.Errorf("body missing token: %q", body)
	}
	if !strings.Contains(body, "multipart/alternative") {
		t.Errorf("body missing multipart marker: %q", body)
	}
	if !strings.Contains(body, "<a href=") {
		t.Errorf("body missing HTML part: %q", body)
	}
}

// === T5: SMTP uses DefaultFrom when Message.From is empty ===

func TestSMTPSenderUsesDefaultFrom(t *testing.T) {
	fake := &fakeSMTPClient{}
	cfg := email.SMTPConfig{
		Host:        "smtp.example.com",
		Port:        25, // unauthenticated
		DefaultFrom: "default@example.com",
	}
	email.SetDialerForTest(&cfg, func(_ string, _ time.Duration) (any, error) {
		return fake, nil
	})
	s, _ := email.NewSMTPSender(cfg)
	if err := s.SendMessage(context.Background(), email.Message{
		To:       "user@acme.test",
		Subject:  "x",
		TextBody: "y",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if fake.mailFrom != "default@example.com" {
		t.Errorf("mailFrom = %q, want default@example.com", fake.mailFrom)
	}
	if fake.authCalls != 0 {
		t.Errorf("authCalls = %d, want 0 (unauth)", fake.authCalls)
	}
	if !strings.Contains(fake.dataBuf.String(), "From: default@example.com") {
		t.Errorf("body missing default From header: %q", fake.dataBuf.String())
	}
}

// === T6: SMTP rejects empty From + DefaultFrom ===

func TestSMTPSenderRejectsEmptyFrom(t *testing.T) {
	fake := &fakeSMTPClient{}
	cfg := email.SMTPConfig{Host: "smtp.example.com", Port: 25}
	email.SetDialerForTest(&cfg, func(_ string, _ time.Duration) (any, error) {
		return fake, nil
	})
	s, _ := email.NewSMTPSender(cfg)
	err := s.SendMessage(context.Background(), email.Message{
		To: "user@acme.test", Subject: "x", TextBody: "y",
	})
	if err == nil {
		t.Fatal("expected error for empty From + DefaultFrom")
	}
}
