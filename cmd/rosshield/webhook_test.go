package main

// webhook_test.go — E29 webhook 서브커맨드 통합 테스트.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newWebhookMockServer(t *testing.T, wantToken string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != wantToken {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoints": []map[string]any{
				{
					"id":          "wh_TEST",
					"url":         "https://siem.example.com/hook",
					"secretLast4": "1234",
					"events":      []string{"scan.completed"},
					"format":      "json",
					"enabled":     true,
					"createdAt":   "2026-05-08T00:00:00Z",
					"updatedAt":   "2026-05-08T00:00:00Z",
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestWebhookListOutputsTable(t *testing.T) {
	srv := newWebhookMockServer(t, "tok-admin")
	cfgPath := configWithServer(t, srv.URL, "tok-admin")

	stdout, _ := captureStdio(t, func() {
		exit := runWebhook([]string{"list", "--config", cfgPath, "-o", "json"})
		if exit != 0 {
			t.Errorf("exit=%d, want 0", exit)
		}
	})
	if !strings.Contains(stdout, "wh_TEST") {
		t.Errorf("output missing wh_TEST: %s", stdout)
	}
	if !strings.Contains(stdout, "scan.completed") {
		t.Errorf("output missing event: %s", stdout)
	}
}

func TestWebhookListMaps401ToExitTwo(t *testing.T) {
	srv := newWebhookMockServer(t, "different-token")
	cfgPath := configWithServer(t, srv.URL, "tok-admin")

	exit := runWebhook([]string{"list", "--config", cfgPath})
	if exit != 2 {
		t.Errorf("exit=%d, want 2 (401 → 4xx → 2)", exit)
	}
}

func TestWebhookHelpExitsZero(t *testing.T) {
	exit := runWebhook([]string{"--help"})
	if exit != 0 {
		t.Errorf("exit=%d, want 0", exit)
	}
}
