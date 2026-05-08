package email

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// NoopSender는 실 SMTP 연결 없이 주입된 writer(기본 io.Discard)에 JSON 한 줄을
// 출력하고 항상 성공을 반환하는 어댑터입니다.
//
// 운영 기본값 — `--email-provider=noop`(기본). 다음 용도:
//
//   - 데스크톱 SKU에서 admin이 token URL을 직접 사용자에게 전달하는 모델.
//   - 개발·CI에서 outbound traffic 없이 발송 호출이 일어났는지만 검증.
//
// 기본 writer가 io.Discard인 이유: subcommand(예: `seed demo`)가 stdout에 단일 JSON을
// 쓰는데, NoopSender가 stdout에 동시에 쓰면 출력이 섞여 파싱 실패. bootstrap에서
// 별도 logger sink를 주입하면 (NewNoopSenderWith), 운영 중 발송 시도가 audit 가능하다.
//
// JSON 형식 (한 줄): {"ts":"...","provider":"noop","from":"...","to":"...","subject":"...","textBytes":N,"htmlBytes":N}.
type NoopSender struct {
	out io.Writer
	now func() time.Time
}

// NewNoopSender는 io.Discard에 출력하는 NoopSender를 반환합니다 (운영 안전 기본값).
//
// 운영 환경에서 발송 시도를 감사하려면 NewNoopSenderWith로 별도 sink (예: logger 어댑터)를
// 주입하라. bootstrap.go가 slog 어댑터를 주입한다.
func NewNoopSender() *NoopSender {
	return &NoopSender{out: io.Discard, now: time.Now}
}

// NewNoopSenderWith는 주입된 writer·시계로 NoopSender를 반환합니다.
//
// out=nil이면 io.Discard (안전 기본). 테스트는 *bytes.Buffer를 주입.
func NewNoopSenderWith(out io.Writer, now func() time.Time) *NoopSender {
	if out == nil {
		out = io.Discard
	}
	if now == nil {
		now = time.Now
	}
	return &NoopSender{out: out, now: now}
}

// Provider는 식별자 "noop"을 반환합니다.
func (*NoopSender) Provider() string { return "noop" }

// SendMessage는 stdout에 JSON 한 줄을 쓰고 항상 nil을 반환합니다.
//
// 네트워크 호출은 일어나지 않습니다. payload 자체(textBody/htmlBody)는 출력에 포함하지
// 않음 — token이 평문으로 stdout에 노출되어 운영 환경 로그 수집기로 흘러가는 것을 방지.
func (n *NoopSender) SendMessage(ctx context.Context, msg Message) error {
	if msg.To == "" {
		return fmt.Errorf("email/noop: To is required")
	}
	rec := struct {
		TS        string `json:"ts"`
		Provider  string `json:"provider"`
		From      string `json:"from"`
		To        string `json:"to"`
		Subject   string `json:"subject"`
		TextBytes int    `json:"textBytes"`
		HTMLBytes int    `json:"htmlBytes"`
	}{
		TS:        n.now().UTC().Format(time.RFC3339Nano),
		Provider:  "noop",
		From:      msg.From,
		To:        msg.To,
		Subject:   msg.Subject,
		TextBytes: len(msg.TextBody),
		HTMLBytes: len(msg.HTMLBody),
	}
	buf, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("email/noop: marshal log: %w", err)
	}
	if _, err := n.out.Write(append(buf, '\n')); err != nil {
		return fmt.Errorf("email/noop: write log: %w", err)
	}
	return nil
}
