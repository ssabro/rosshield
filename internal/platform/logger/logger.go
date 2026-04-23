package logger

import (
	"context"
	"io"
	"log/slog"
)

type ctxKey int

const (
	keyTenantID ctxKey = iota + 1
	keyRequestID
	keyTraceID
)

func WithTenantID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyTenantID, id)
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyTraceID, id)
}

func TenantID(ctx context.Context) string {
	v, _ := ctx.Value(keyTenantID).(string)
	return v
}

func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(keyRequestID).(string)
	return v
}

func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(keyTraceID).(string)
	return v
}

type contextHandler struct {
	next slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if v := TenantID(ctx); v != "" {
		r.AddAttrs(slog.String("tenantId", v))
	}
	if v := RequestID(ctx); v != "" {
		r.AddAttrs(slog.String("requestId", v))
	}
	if v := TraceID(ctx); v != "" {
		r.AddAttrs(slog.String("traceId", v))
	}
	return h.next.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name)}
}

func New(w io.Writer, opts *slog.HandlerOptions) *slog.Logger {
	return slog.New(&contextHandler{next: slog.NewJSONHandler(w, opts)})
}
