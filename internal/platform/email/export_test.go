package email

import "time"

// SetDialerForTest는 외부 테스트에서 SMTPConfig.dialer hook을 주입하기 위한 헬퍼입니다.
//
// fn은 (any, error) 반환 — 테스트는 fakeSMTPClient 같은 자체 타입을 그대로 돌려준다.
// 본 함수는 export_test.go에 있어 production 빌드에는 포함되지 않는다.
//
// 주의: fn이 돌려주는 값은 *반드시* smtpClient interface를 만족해야 한다 (런타임 type assert).
func SetDialerForTest(cfg *SMTPConfig, fn func(addr string, timeout time.Duration) (any, error)) {
	cfg.dialer = func(addr string, timeout time.Duration) (smtpClient, error) {
		v, err := fn(addr, timeout)
		if err != nil {
			return nil, err
		}
		c, ok := v.(smtpClient)
		if !ok {
			panic("SetDialerForTest: returned value does not satisfy smtpClient")
		}
		return c, nil
	}
}
