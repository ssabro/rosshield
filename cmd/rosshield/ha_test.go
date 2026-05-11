package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRunHANoArgs — args 없으면 usage + exit 2.
func TestRunHANoArgs(t *testing.T) {
	t.Parallel()
	if got := runHA(nil); got != 2 {
		t.Errorf("runHA(nil) = %d, want 2", got)
	}
}

// TestRunHAUnknownSub — 알 수 없는 sub-command → exit 2.
func TestRunHAUnknownSub(t *testing.T) {
	t.Parallel()
	if got := runHA([]string{"foo"}); got != 2 {
		t.Errorf("runHA(foo) = %d, want 2", got)
	}
}

// TestRunHAHelp — help 명시는 exit 0.
func TestRunHAHelp(t *testing.T) {
	t.Parallel()
	for _, h := range []string{"-h", "--help", "help"} {
		if got := runHA([]string{h}); got != 0 {
			t.Errorf("runHA(%q) = %d, want 0", h, got)
		}
	}
}

// TestHAStatusFetchHaActive — fake healthz가 ha 필드 응답하면 정상 출력 + exit 0.
func TestHAStatusFetchHaActive(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthzView{
			Status: "ok",
			HA: &haHealthView{
				Enabled:         true,
				Role:            "leader",
				Epoch:           42,
				LeaderID:        "host-test:1234",
				LastHeartbeatAt: "2026-05-11T12:00:00Z",
			},
		})
	}))
	defer srv.Close()

	code := runHAStatus([]string{"--server", srv.URL, "-o", "json"})
	if code != 0 {
		t.Errorf("runHAStatus = %d, want 0", code)
	}
}

// TestHAStatusFetchHaDisabled — ha 필드 없으면 exit 0 + 안내 메시지.
func TestHAStatusFetchHaDisabled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthzView{Status: "ok"}) // ha omitempty
	}))
	defer srv.Close()

	code := runHAStatus([]string{"--server", srv.URL})
	if code != 0 {
		t.Errorf("runHAStatus = %d, want 0", code)
	}
}
