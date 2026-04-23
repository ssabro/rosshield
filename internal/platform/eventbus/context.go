package eventbus

import "context"

type ctxKey int

const (
	keyCorrelationID ctxKey = iota + 1
	keyCausationID
)

// WithCorrelationID는 ctx에 correlation ID를 주입합니다.
// HTTP 미들웨어가 requestId를 correlation으로 매핑하거나, Bus가 자동 생성한 값을 전파합니다 (R2-7).
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyCorrelationID, id)
}

// CorrelationIDFromContext는 ctx에서 correlation ID를 추출합니다. 없으면 빈 문자열.
func CorrelationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyCorrelationID).(string)
	return v
}

// WithCausationID는 ctx에 "이 작업을 유발한 직전 이벤트 ID"를 주입합니다.
// 구독자 worker가 handler 호출 직전에 evt.ID를 심으므로,
// handler 안에서 발행하는 후속 이벤트의 CausationID가 자동으로 직전 이벤트 ID가 됩니다 (R2 §7).
func WithCausationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyCausationID, id)
}

// CausationIDFromContext는 ctx에서 causation ID를 추출합니다. 없으면 빈 문자열.
func CausationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyCausationID).(string)
	return v
}
