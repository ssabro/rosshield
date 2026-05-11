package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/api/handlers"
)

type fakeRole struct {
	leader bool
}

func (f *fakeRole) IsLeader() bool      { return f.leader }
func (f *fakeRole) CurrentEpoch() int64 { return 0 }

func newWriteRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, path, strings.NewReader(`{}`))
}

func TestRequireLeaderForWritesNilProviderAllowsAll(t *testing.T) {
	t.Parallel()
	called := 0
	mw := handlers.RequireLeaderForWrites(nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newWriteRequest(method, "/x"))
		if rec.Code != http.StatusOK {
			t.Errorf("method %s: code = %d, want 200 (nil provider)", method, rec.Code)
		}
	}
	if called != 5 {
		t.Errorf("called = %d, want 5", called)
	}
}

func TestRequireLeaderForWritesGetPassesEvenForFollower(t *testing.T) {
	t.Parallel()
	role := &fakeRole{leader: false}
	mw := handlers.RequireLeaderForWrites(role)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newWriteRequest(method, "/x"))
		if rec.Code != http.StatusOK {
			t.Errorf("method %s: code = %d, want 200 (read passes for follower)", method, rec.Code)
		}
	}
}

func TestRequireLeaderForWritesFollowerWriteReturns503(t *testing.T) {
	t.Parallel()
	role := &fakeRole{leader: false}
	mw := handlers.RequireLeaderForWrites(role)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream handler should not run for follower write")
	}))

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newWriteRequest(method, "/x"))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("method %s: code = %d, want 503", method, rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got != "5" {
			t.Errorf("method %s: Retry-After = %q, want %q", method, got, "5")
		}

		var body struct {
			OK    bool `json:"ok"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Errorf("method %s: decode body: %v", method, err)
			continue
		}
		if body.OK {
			t.Errorf("method %s: ok = true, want false", method)
		}
		if body.Error.Code != "NOT_LEADER" {
			t.Errorf("method %s: error.code = %q, want NOT_LEADER", method, body.Error.Code)
		}
	}
}

func TestRequireLeaderForWritesLeaderWritePasses(t *testing.T) {
	t.Parallel()
	role := &fakeRole{leader: true}
	mw := handlers.RequireLeaderForWrites(role)
	called := 0
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusCreated)
	}))

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newWriteRequest(method, "/x"))
		if rec.Code != http.StatusCreated {
			t.Errorf("method %s: code = %d, want 201 (leader write)", method, rec.Code)
		}
	}
	if called != 4 {
		t.Errorf("called = %d, want 4 (POST/PUT/PATCH/DELETE)", called)
	}
}
