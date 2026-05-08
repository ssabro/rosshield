package main

// invite_test.go — E29 invite 서브커맨드 통합 테스트.
//
// 시나리오:
//
//	T1 TestInviteCreateOutputsTokenAndAcceptUrl (E29.T1)
//	T2 TestInviteListOutputsTable
//	T3 TestInviteRevokeExitsZero
//	T4 TestInviteCreateRequiresEmailAndRole — args 검증
//	T5 TestInviteCreateMaps409ToExitTwo — backend 중복 → exit 2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// inviteServerState 는 invite mock backend 상태.
type inviteServerState struct {
	wantToken     string
	revokedID     string
	createCalls   int
	lastBody      map[string]any
	respondStatus int // 0이면 default (201). 다른 값이면 그 status로 응답.
}

func newInviteMockServer(t *testing.T, st *inviteServerState) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if st.wantToken != "" {
				token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				if token != st.wantToken {
					w.WriteHeader(http.StatusUnauthorized)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
					return
				}
			}
			h(w, r)
		}
	}

	mux.HandleFunc("POST /api/v1/invitations", auth(func(w http.ResponseWriter, r *http.Request) {
		st.createCalls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.lastBody = body
		if st.respondStatus != 0 && st.respondStatus != 201 {
			w.WriteHeader(st.respondStatus)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "duplicate"})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        "inv_TEST",
			"email":     body["email"],
			"roleName":  body["roleName"],
			"invitedBy": "us_admin",
			"expiresAt": "2026-12-31T23:59:59Z",
			"createdAt": "2026-05-08T00:00:00Z",
			"token":     "test-invitation-token-base64url",
		})
	}))

	mux.HandleFunc("GET /api/v1/invitations", auth(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"invitations": []map[string]any{
				{
					"id": "inv_A", "email": "a@test", "roleName": "operator",
					"invitedBy": "us_admin", "expiresAt": "2026-12-31T23:59:59Z",
					"createdAt": "2026-05-08T00:00:00Z",
				},
			},
		})
	}))

	mux.HandleFunc("DELETE /api/v1/invitations/", auth(func(w http.ResponseWriter, r *http.Request) {
		st.revokedID = strings.TrimPrefix(r.URL.Path, "/api/v1/invitations/")
		w.WriteHeader(http.StatusNoContent)
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// === T1 ===

func TestInviteCreateOutputsTokenAndAcceptUrl(t *testing.T) {
	st := &inviteServerState{wantToken: "tok-admin"}
	srv := newInviteMockServer(t, st)
	cfgPath := configWithServer(t, srv.URL, "tok-admin")

	stdout, _ := captureStdio(t, func() {
		exit := runInvite([]string{
			"create", "--config", cfgPath,
			"--email", "new@test", "--role", "operator",
			"-o", "json",
		})
		if exit != 0 {
			t.Errorf("exit=%d, want 0", exit)
		}
	})

	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, stdout)
	}
	if out["token"] != "test-invitation-token-base64url" {
		t.Errorf("token field missing/wrong: %v", out["token"])
	}
	wantAccept := srv.URL + "/invitations/accept/test-invitation-token-base64url"
	if out["acceptUrl"] != wantAccept {
		t.Errorf("acceptUrl = %v, want %s", out["acceptUrl"], wantAccept)
	}
	if st.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1", st.createCalls)
	}
	if st.lastBody["roleName"] != "operator" {
		t.Errorf("body roleName = %v, want operator", st.lastBody["roleName"])
	}
}

func TestInviteListOutputsTable(t *testing.T) {
	st := &inviteServerState{wantToken: "tok-admin"}
	srv := newInviteMockServer(t, st)
	cfgPath := configWithServer(t, srv.URL, "tok-admin")

	stdout, _ := captureStdio(t, func() {
		exit := runInvite([]string{"list", "--config", cfgPath, "-o", "json"})
		if exit != 0 {
			t.Errorf("exit=%d, want 0", exit)
		}
	})
	if !strings.Contains(stdout, "inv_A") {
		t.Errorf("output missing inv_A: %s", stdout)
	}
}

func TestInviteRevokeExitsZero(t *testing.T) {
	st := &inviteServerState{wantToken: "tok-admin"}
	srv := newInviteMockServer(t, st)
	cfgPath := configWithServer(t, srv.URL, "tok-admin")

	exit := runInvite([]string{"revoke", "--config", cfgPath, "inv_X"})
	if exit != 0 {
		t.Errorf("exit=%d, want 0", exit)
	}
	if st.revokedID != "inv_X" {
		t.Errorf("revokedID = %q, want inv_X", st.revokedID)
	}
}

func TestInviteCreateRequiresEmailAndRole(t *testing.T) {
	cfgPath := configWithServer(t, "http://x", "tok")
	for _, args := range [][]string{
		{"create", "--config", cfgPath, "--role", "operator"}, // no email
		{"create", "--config", cfgPath, "--email", "a@b"},     // no role
		{"create", "--config", cfgPath},                       // both missing
	} {
		exit := runInvite(args)
		if exit != 2 {
			t.Errorf("args=%v exit=%d, want 2", args, exit)
		}
	}
}

func TestInviteCreateMaps409ToExitTwo(t *testing.T) {
	st := &inviteServerState{wantToken: "tok-admin", respondStatus: http.StatusConflict}
	srv := newInviteMockServer(t, st)
	cfgPath := configWithServer(t, srv.URL, "tok-admin")

	exit := runInvite([]string{
		"create", "--config", cfgPath,
		"--email", "dup@test", "--role", "operator",
	})
	if exit != 2 {
		t.Errorf("exit=%d, want 2 (4xx → exit 2)", exit)
	}
}
