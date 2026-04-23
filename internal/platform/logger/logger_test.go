package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/logger"
)

func TestLoggerIncludesContextFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logger.New(&buf, nil)

	ctx := context.Background()
	ctx = logger.WithTenantID(ctx, "tn_abc")
	ctx = logger.WithRequestID(ctx, "req_xyz")
	ctx = logger.WithTraceID(ctx, "tr_123")

	log.InfoContext(ctx, "hello")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v; raw=%s", err, buf.String())
	}

	want := map[string]string{
		"tenantId":  "tn_abc",
		"requestId": "req_xyz",
		"traceId":   "tr_123",
		"msg":       "hello",
	}
	for k, v := range want {
		got, _ := entry[k].(string)
		if got != v {
			t.Errorf("field %q = %q, want %q (entry=%v)", k, got, v, entry)
		}
	}
}

func TestLoggerOmitsUnsetContextFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logger.New(&buf, nil)

	log.InfoContext(context.Background(), "hello")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"tenantId", "requestId", "traceId"} {
		if _, present := entry[k]; present {
			t.Errorf("field %q should be absent when ctx is empty, entry=%v", k, entry)
		}
	}
}

func TestLoggerContextExtractors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = logger.WithTenantID(ctx, "tn_1")
	ctx = logger.WithRequestID(ctx, "req_2")
	ctx = logger.WithTraceID(ctx, "tr_3")

	if got := logger.TenantID(ctx); got != "tn_1" {
		t.Errorf("TenantID = %q, want tn_1", got)
	}
	if got := logger.RequestID(ctx); got != "req_2" {
		t.Errorf("RequestID = %q, want req_2", got)
	}
	if got := logger.TraceID(ctx); got != "tr_3" {
		t.Errorf("TraceID = %q, want tr_3", got)
	}
}

func TestLoggerExtractorsReturnEmptyWhenUnset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if got := logger.TenantID(ctx); got != "" {
		t.Errorf("TenantID = %q, want empty", got)
	}
	if got := logger.RequestID(ctx); got != "" {
		t.Errorf("RequestID = %q, want empty", got)
	}
	if got := logger.TraceID(ctx); got != "" {
		t.Errorf("TraceID = %q, want empty", got)
	}
}

func TestLoggerWithAttrsPreservesContextHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logger.New(&buf, nil).With("component", "scan")

	ctx := logger.WithTenantID(context.Background(), "tn_abc")
	log.InfoContext(ctx, "hello")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry["component"] != "scan" {
		t.Errorf("component = %v, want scan", entry["component"])
	}
	if entry["tenantId"] != "tn_abc" {
		t.Errorf("tenantId = %v, want tn_abc (ctx fields must survive With)", entry["tenantId"])
	}
}
