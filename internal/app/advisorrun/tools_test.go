package advisorrun_test

// tools_test.go — E16-B Dispatcher 단위 테스트.
//
// fake scan/evidence Service로 도메인 의존 격리 — tool 호출 → JSON 출력 검증.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/app/advisorrun"
	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/domain/evidence"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// === fakes ===

type fakeScan struct {
	session scan.ScanSession
	results []scan.ScanResult
	err     error
}

func (f *fakeScan) GetSession(_ context.Context, _ storage.Tx, _ string) (scan.ScanSession, error) {
	if f.err != nil {
		return scan.ScanSession{}, f.err
	}
	return f.session, nil
}
func (f *fakeScan) ListResults(_ context.Context, _ storage.Tx, _ string) ([]scan.ScanResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}
func (f *fakeScan) ListResultsByRobot(_ context.Context, _ storage.Tx, _ string, _ int) ([]scan.ScanResult, error) {
	return f.results, nil
}

// 미사용 메서드 (interface 만족용 panic).
func (f *fakeScan) StartScan(_ context.Context, _ storage.Tx, _ scan.StartScanRequest) (scan.ScanSession, error) {
	panic("unused")
}
func (f *fakeScan) ListSessions(_ context.Context, _ storage.Tx, _ scan.ListSessionsFilter) ([]scan.ScanSession, error) {
	panic("unused")
}
func (f *fakeScan) TransitionSession(_ context.Context, _ storage.Tx, _ string, _ scan.SessionStatus, _ string) (scan.ScanSession, error) {
	panic("unused")
}
func (f *fakeScan) CancelSession(_ context.Context, _ storage.Tx, _, _ string) (scan.ScanSession, error) {
	panic("unused")
}
func (f *fakeScan) RecordResult(_ context.Context, _ storage.Tx, _ scan.RecordResultRequest) (scan.ScanResult, error) {
	panic("unused")
}
func (f *fakeScan) RecomputeSeverityAggregate(_ context.Context, _ storage.Tx, _ string) error {
	panic("unused")
}

type fakeEvidence struct {
	rec  evidence.Record
	body []byte
	err  error
}

func (f *fakeEvidence) Read(_ context.Context, _ storage.Tx, _ string) (evidence.Record, []byte, error) {
	if f.err != nil {
		return evidence.Record{}, nil, f.err
	}
	return f.rec, f.body, nil
}

func (f *fakeEvidence) Store(_ context.Context, _ storage.Tx, _ evidence.StoreInput) (evidence.StoreResult, error) {
	panic("unused")
}
func (f *fakeEvidence) LinkToResult(_ context.Context, _ storage.Tx, _ string, _ []string) ([]evidence.RecordedRef, error) {
	panic("unused")
}
func (f *fakeEvidence) ListForResult(_ context.Context, _ storage.Tx, _ string) ([]evidence.Record, error) {
	panic("unused")
}

// === tests ===

func TestAvailableToolsExposesThree(t *testing.T) {
	t.Parallel()
	d := advisorrun.NewDispatcher(&fakeScan{}, &fakeEvidence{}, clock.System())
	tools := d.AvailableTools()
	if len(tools) != 3 {
		t.Fatalf("len = %d, want 3", len(tools))
	}
	names := map[string]bool{}
	for _, tdef := range tools {
		names[tdef.Name] = true
	}
	for _, want := range []string{"get_session", "list_results", "get_evidence"} {
		if !names[want] {
			t.Errorf("missing tool: %s", want)
		}
	}
}

func TestDispatchUnknownToolReturnsErrUnknownTool(t *testing.T) {
	t.Parallel()
	d := advisorrun.NewDispatcher(&fakeScan{}, &fakeEvidence{}, clock.System())
	_, err := d.Dispatch(context.Background(), nil, advisor.ToolCallRequest{ToolName: "delete_all_data"})
	if !errors.Is(err, advisor.ErrUnknownTool) {
		t.Errorf("err = %v, want ErrUnknownTool (write attempt blocked)", err)
	}
}

func TestGetSessionReturnsJSON(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	scn := &fakeScan{
		session: scan.ScanSession{
			ID:          "scan_X",
			FleetID:     "fl_X",
			PackID:      "pk_X",
			Status:      scan.StatusCompleted,
			Trigger:     scan.TriggerManual,
			Progress:    scan.SessionProgress{Total: 10, Completed: 8, Failed: 2},
			CompletedAt: &completedAt,
		},
	}
	d := advisorrun.NewDispatcher(scn, &fakeEvidence{}, clock.System())
	args, _ := json.Marshal(map[string]string{"sessionId": "scan_X"})

	res, err := d.Dispatch(context.Background(), nil, advisor.ToolCallRequest{
		ToolName: "get_session",
		ArgsJSON: args,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var view map[string]any
	if err := json.Unmarshal(res.ResultJSON, &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view["id"] != "scan_X" {
		t.Errorf("id = %v, want scan_X", view["id"])
	}
	if view["status"] != "completed" {
		t.Errorf("status = %v", view["status"])
	}
	if int(view["failed"].(float64)) != 2 {
		t.Errorf("failed = %v", view["failed"])
	}
}

func TestGetSessionRequiresSessionId(t *testing.T) {
	t.Parallel()
	d := advisorrun.NewDispatcher(&fakeScan{}, &fakeEvidence{}, clock.System())
	args, _ := json.Marshal(map[string]string{})
	_, err := d.Dispatch(context.Background(), nil, advisor.ToolCallRequest{
		ToolName: "get_session",
		ArgsJSON: args,
	})
	if err == nil || !strings.Contains(err.Error(), "sessionId is required") {
		t.Errorf("err = %v, want sessionId required", err)
	}
}

func TestListResultsFiltersByRobotId(t *testing.T) {
	t.Parallel()
	scn := &fakeScan{
		results: []scan.ScanResult{
			{ID: "scr_A", RobotID: "rb_1", CheckID: "C1", Outcome: scan.OutcomePass, DurationMs: 10},
			{ID: "scr_B", RobotID: "rb_2", CheckID: "C1", Outcome: scan.OutcomeFail, DurationMs: 20},
			{ID: "scr_C", RobotID: "rb_1", CheckID: "C2", Outcome: scan.OutcomePass, DurationMs: 15},
		},
	}
	d := advisorrun.NewDispatcher(scn, &fakeEvidence{}, clock.System())
	args, _ := json.Marshal(map[string]string{"sessionId": "s1", "robotId": "rb_1"})
	res, err := d.Dispatch(context.Background(), nil, advisor.ToolCallRequest{
		ToolName: "list_results",
		ArgsJSON: args,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var view map[string]any
	_ = json.Unmarshal(res.ResultJSON, &view)
	count := int(view["count"].(float64))
	if count != 2 {
		t.Errorf("count = %d, want 2 (filtered by rb_1)", count)
	}
}

func TestGetEvidenceIncludesBodyForTextOnly(t *testing.T) {
	t.Parallel()
	ev := &fakeEvidence{
		rec: evidence.Record{
			ID:          "ev_X",
			SHA256:      "deadbeef",
			ContentType: evidence.ContentStdout,
			SizeBytes:   12,
		},
		body: []byte("hello world\n"),
	}
	d := advisorrun.NewDispatcher(&fakeScan{}, ev, clock.System())
	args, _ := json.Marshal(map[string]string{"evidenceId": "ev_X"})
	res, err := d.Dispatch(context.Background(), nil, advisor.ToolCallRequest{
		ToolName: "get_evidence",
		ArgsJSON: args,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var view map[string]any
	_ = json.Unmarshal(res.ResultJSON, &view)
	if view["bodyText"] != "hello world\n" {
		t.Errorf("bodyText = %v", view["bodyText"])
	}
	if view["contentType"] != "stdout" {
		t.Errorf("contentType = %v", view["contentType"])
	}
}

func TestGetEvidenceTruncatesLargeBody(t *testing.T) {
	t.Parallel()
	body := make([]byte, 10000)
	for i := range body {
		body[i] = 'x'
	}
	ev := &fakeEvidence{
		rec: evidence.Record{
			ID:          "ev_BIG",
			SHA256:      "abc",
			ContentType: evidence.ContentStdout,
			SizeBytes:   int64(len(body)),
		},
		body: body,
	}
	d := advisorrun.NewDispatcher(&fakeScan{}, ev, clock.System())
	args, _ := json.Marshal(map[string]string{"evidenceId": "ev_BIG"})
	res, _ := d.Dispatch(context.Background(), nil, advisor.ToolCallRequest{
		ToolName: "get_evidence",
		ArgsJSON: args,
	})
	var view map[string]any
	_ = json.Unmarshal(res.ResultJSON, &view)
	if view["truncated"] != true {
		t.Errorf("truncated = %v, want true (10KB > 8KB max)", view["truncated"])
	}
	if len(view["bodyText"].(string)) != 8*1024 {
		t.Errorf("bodyText len = %d, want 8192", len(view["bodyText"].(string)))
	}
}

func TestGetEvidenceOmitsBodyForBinary(t *testing.T) {
	t.Parallel()
	ev := &fakeEvidence{
		rec: evidence.Record{
			ID:          "ev_BIN",
			SHA256:      "abc",
			ContentType: evidence.ContentType("binary"),
			SizeBytes:   100,
		},
		body: []byte{0x00, 0xFF, 0x42},
	}
	d := advisorrun.NewDispatcher(&fakeScan{}, ev, clock.System())
	args, _ := json.Marshal(map[string]string{"evidenceId": "ev_BIN"})
	res, _ := d.Dispatch(context.Background(), nil, advisor.ToolCallRequest{
		ToolName: "get_evidence",
		ArgsJSON: args,
	})
	var view map[string]any
	_ = json.Unmarshal(res.ResultJSON, &view)
	if _, has := view["bodyText"]; has {
		t.Errorf("bodyText should be omitted for binary, got %v", view["bodyText"])
	}
}
