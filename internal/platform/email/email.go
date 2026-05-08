// Package email은 옵트인 발송 어댑터 표면입니다 (O6 — invite email).
//
// 결정 근거:
//   - P2 옵트인 지능화·외부 의존: 기본값은 noop (외부 호출 X — 주입된 sink에 메타데이터 한 줄).
//   - P3 에어갭 1급: stdlib net/smtp만 사용 — 외부 SDK 0.
//   - P4 멀티테넌시: 본 패키지는 도메인 미지각 — caller(domain layer)가 tenant 컨텍스트에서 호출.
//   - P5 도메인 결합: tenant 도메인이 net/smtp를 직접 import하지 않게 Notifier interface로 분리.
//
// 사용 흐름:
//
//  1. main.go 가 --email-provider flag로 noop 또는 smtp 어댑터 선택 (기본 noop).
//  2. bootstrap.go 가 어댑터를 InvitationNotifier 어댑터로 감싸 tenantrepo Deps에 주입.
//  3. tenant.InvitationService.CreateInvitation 안에서 Notifier.NotifyInvitationSent 호출.
//
// 본 표면은 도메인 비지각 — 'invitation' 단어를 모름. caller가 subject·body를
// 직접 빌드해서 SendMessage를 부른다.
package email

import (
	"context"
	"errors"
)

// Sender는 단일 email message 발송 인터페이스입니다.
//
// 모든 어댑터는 이 표면을 만족 — caller(bootstrap)가 어댑터 종류와 무관하게 같은
// 코드를 쓴다. error는 nil이면 발송 성공으로 간주.
type Sender interface {
	// SendMessage는 단일 수신자에게 텍스트·옵션 HTML email을 발송합니다.
	//
	// to: 수신자 주소 (단일). subject: Subject 헤더. textBody: text/plain 본문.
	// htmlBody: text/html 본문 — 빈 문자열이면 multipart 없이 plain만 발송.
	SendMessage(ctx context.Context, msg Message) error

	// Provider는 어댑터 식별자 ("noop"|"smtp")를 반환합니다 — 운영 로그/health에 사용.
	Provider() string
}

// Message는 한 통의 email입니다.
//
// From은 어댑터 설정에서 채울 수도 있지만 caller가 명시적으로 채우면 우선.
// 어댑터가 자체 default From을 갖는 경우 (ex SMTP --email-from), Message.From이
// 빈 문자열이면 default를 사용한다.
type Message struct {
	From     string // 발신자 (예: "rosshield <noreply@example.com>"). 빈 값이면 어댑터 default.
	To       string // 수신자 단일.
	Subject  string
	TextBody string // text/plain 본문 (필수).
	HTMLBody string // text/html 본문 — 빈 값이면 multipart 없이 plain만.
}

// ErrEmailDisabled는 noop 어댑터가 발송을 의도적으로 skip할 때 반환할 수 있는 sentinel입니다.
//
// 현재 noop은 항상 nil을 반환하지만(사용자에게 silent success), 운영 정책 변경 시
// caller가 errors.Is로 분기 가능.
var ErrEmailDisabled = errors.New("email: provider disabled (noop)")
