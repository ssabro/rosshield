package handlers_test

// advisor_test.go — E19-3-A Advisor HTTP 표면 통합 테스트.
//
// fixture는 handlers_test.go의 newFixture 활용 — noop LLM이라 모든 Ask는 503 Disabled.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestAskAdvisorRequiresAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()

	body, _ := json.Marshal(map[string]any{"question": "왜 이 check가 fail했나요?"})
	resp, err := http.Post(
		f.server.URL+"/api/v1/advisor/conversations:ask",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestListAdvisorConversationsRequiresAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()

	resp, err := http.Get(f.server.URL + "/api/v1/advisor/conversations")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestListAdvisorConversationsReturnsEmptyWhenNoneExist(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	resp := f.doRequest(t, http.MethodGet, "/api/v1/advisor/conversations", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(raw))
	}

	var body struct {
		Conversations []map[string]any `json:"conversations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Conversations) != 0 {
		t.Errorf("conversations = %d, want 0", len(body.Conversations))
	}
}

func TestAskAdvisorWithEmptyQuestionReturns400(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{"question": ""})
	resp := f.doRequest(t, http.MethodPost, "/api/v1/advisor/conversations:ask", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 400 (empty question), body=%s", resp.StatusCode, string(raw))
	}
}

func TestAskAdvisorReturns503WhenLLMDisabled(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	// fixture는 noop LLM 어댑터 → ErrAdvisorDisabled → 503.
	body, _ := json.Marshal(map[string]any{
		"question": "ros1_no_password_node check가 왜 fail했나요?",
	})
	resp := f.doRequest(t, http.MethodPost, "/api/v1/advisor/conversations:ask", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 503 (LLM disabled), body=%s", resp.StatusCode, string(raw))
	}
}

func TestGetAdvisorConversationNotFoundReturns404(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	defer f.closeFn()
	token := f.loginAndGetToken(t)

	resp := f.doRequest(t, http.MethodGet,
		"/api/v1/advisor/conversations/conv_NONEXISTENT", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 404, body=%s", resp.StatusCode, string(raw))
	}
}
