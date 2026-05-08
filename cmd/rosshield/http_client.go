package main

// http_client.go — `rosshield` CLI의 HTTP 클라이언트 wrapper (E9 Stage C, R11-2 stdlib net/http).
//
// 책임:
//   - Config(ServerURL + AccessToken) 기반 base URL + Authorization 헤더 자동 부착
//   - GET/POST helper로 JSON 요청·응답 직렬화
//   - 표준 status code → exit code 매핑 (R11-8): 0 OK / 1 transport / 2 4xx / 3 5xx
//
// 외부 dep 0 — stdlib net/http + encoding/json만 사용.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPError는 서버가 4xx/5xx를 반환했을 때의 에러입니다.
type HTTPError struct {
	StatusCode int
	Message    string // 서버가 보낸 {"error":"..."} 본문 또는 raw body
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// IsClientError returns true for 4xx (R11-8 exit 2 매핑).
func (e *HTTPError) IsClientError() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500
}

// IsServerError returns true for 5xx (R11-8 exit 3 매핑).
func (e *HTTPError) IsServerError() bool {
	return e.StatusCode >= 500
}

// Client는 rosshield-server에 대한 thin HTTP 클라이언트입니다.
type Client struct {
	baseURL     string
	accessToken string
	httpClient  *http.Client
}

// NewClient는 Config로부터 새 Client를 만듭니다.
//
// ServerURL이 비어있으면 DefaultServerURL 사용. AccessToken이 비어있으면 인증 헤더 미부착
// (login 호출에서만 필요).
func NewClient(cfg Config) *Client {
	server := cfg.ServerURL
	if server == "" {
		server = DefaultServerURL
	}
	server = strings.TrimRight(server, "/")
	return &Client{
		baseURL:     server,
		accessToken: cfg.AccessToken,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Get은 GET <path>?<query> 호출 후 응답 JSON을 out에 unmarshal합니다.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	c.applyAuth(req)
	return c.do(req, out)
}

// Post는 POST <path>로 in을 JSON으로 직렬화해 보내고 응답을 out에 unmarshal합니다.
func (c *Client) Post(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)
	return c.do(req, out)
}

// Delete는 DELETE <path>를 호출합니다 (응답 본문 무시 — 204 정상).
func (c *Client) Delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	c.applyAuth(req)
	return c.do(req, nil)
}

func (c *Client) applyAuth(req *http.Request) {
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}
	req.Header.Set("Accept", "application/json")
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("transport: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &HTTPError{StatusCode: resp.StatusCode, Message: extractErrorMessage(body)}
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}

// extractErrorMessage는 {"error":"..."} 또는 raw text를 한 줄로 반환합니다.
func extractErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Error != "" {
		return env.Error
	}
	// raw body — 한 줄로 정규화.
	return strings.TrimSpace(string(body))
}

// HTTPErrorToExitCode는 HTTPError를 R11-8 exit code로 매핑합니다.
//
// nil error → 0
// HTTPError 4xx → 2
// HTTPError 5xx → 3
// 그 외 (transport 등) → 1
func HTTPErrorToExitCode(err error) int {
	if err == nil {
		return 0
	}
	var he *HTTPError
	if errors.As(err, &he) {
		if he.IsClientError() {
			return 2
		}
		if he.IsServerError() {
			return 3
		}
	}
	return 1
}
