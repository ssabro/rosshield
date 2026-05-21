package main

// audit_test.go — Phase 10.D-6 `rosshield audit rotation abort` CLI 통합 테스트.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newAuditAbortMockServer(t *testing.T, wantToken string, status int, body any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/audit/rotation/abort", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != wantToken {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAuditRotationAbortHappyPath(t *testing.T) {
	srv := newAuditAbortMockServer(t, "tok", http.StatusOK, map[string]any{
		"aborted":       true,
		"auditEntryId":  42,
		"abortedAt":     "2026-05-21T12:00:00Z",
		"previousEpoch": 3,
		"reason":        "drill",
	})
	cfgPath := configWithServer(t, srv.URL, "tok")

	stdout, _ := captureStdio(t, func() {
		exit := runAudit([]string{"rotation", "abort", "--reason", "drill", "--config", cfgPath, "-o", "json"})
		if exit != 0 {
			t.Errorf("exit=%d, want 0", exit)
		}
	})
	if !strings.Contains(stdout, `"aborted": true`) {
		t.Errorf("output missing aborted=true: %s", stdout)
	}
	if !strings.Contains(stdout, `"auditEntryId": 42`) {
		t.Errorf("output missing auditEntryId: %s", stdout)
	}
}

func TestAuditRotationAbortMissingReason(t *testing.T) {
	cfgPath := configWithServer(t, "http://x", "tok")
	exit := runAudit([]string{"rotation", "abort", "--config", cfgPath})
	if exit != 2 {
		t.Errorf("exit=%d, want 2 (missing --reason)", exit)
	}
}

func TestAuditRotationAbortMaps403ToExitTwo(t *testing.T) {
	srv := newAuditAbortMockServer(t, "tok-admin", http.StatusForbidden, map[string]string{"error": "forbidden"})
	cfgPath := configWithServer(t, srv.URL, "tok-non-admin")

	exit := runAudit([]string{"rotation", "abort", "--reason", "drill", "--config", cfgPath})
	if exit != 2 {
		t.Errorf("exit=%d, want 2 (403 → 4xx → 2)", exit)
	}
}

func TestAuditHelpExitsZero(t *testing.T) {
	exit := runAudit([]string{"--help"})
	if exit != 0 {
		t.Errorf("exit=%d, want 0", exit)
	}
}

func TestAuditUnknownSubcommandExitsTwo(t *testing.T) {
	exit := runAudit([]string{"bogus"})
	if exit != 2 {
		t.Errorf("exit=%d, want 2 (unknown subcommand)", exit)
	}
}
