package main

// main_test.go — CLI 라우터·version·config·help 핸들러 테스트 (E9 Stage A).
//
// 본 파일은 stdout/stderr 캡처 헬퍼와 라우터 단위 테스트를 묶습니다.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdio는 fn 실행 동안의 stdout·stderr를 string으로 반환합니다.
//
// goroutine 누설 방지 위해 fn 종료 후 pipe close 보장.
func captureStdio(t *testing.T, fn func()) (string, string) {
	t.Helper()
	origOut := os.Stdout
	origErr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	done := make(chan struct {
		out, err string
	}, 1)
	go func() {
		var bufOut, bufErr bytes.Buffer
		_, _ = io.Copy(&bufOut, rOut)
		_, _ = io.Copy(&bufErr, rErr)
		done <- struct{ out, err string }{bufOut.String(), bufErr.String()}
	}()

	fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = origOut
	os.Stderr = origErr

	res := <-done
	return res.out, res.err
}

func TestRunNoArgsExitsTwo(t *testing.T) {
	_, stderr := captureStdio(t, func() {
		if code := run(nil); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "rosshield") {
		t.Fatalf("stderr missing usage: %q", stderr)
	}
}

func TestRunUnknownSubcommandExitsTwo(t *testing.T) {
	_, stderr := captureStdio(t, func() {
		if code := run([]string{"frobnicate"}); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("stderr missing 'unknown command': %q", stderr)
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		_, stderr := captureStdio(t, func() {
			if code := run([]string{arg}); code != 0 {
				t.Errorf("arg=%q exit=%d, want 0", arg, code)
			}
		})
		if !strings.Contains(stderr, "rosshield") {
			t.Errorf("arg=%q stderr missing usage: %q", arg, stderr)
		}
	}
}

func TestRunVersionPrintsVersionLine(t *testing.T) {
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"version"}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(stdout, "rosshield") {
		t.Fatalf("stdout missing 'rosshield': %q", stdout)
	}
	if !strings.Contains(stdout, Version) {
		t.Fatalf("stdout missing version %q: %q", Version, stdout)
	}
}

func TestConfigInitCreatesFileWithDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")
	var exitCode int
	stdout, stderr := captureStdio(t, func() {
		exitCode = run([]string{"config", "init", "--config", path})
	})
	if exitCode != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%q", exitCode, stderr)
	}
	if !strings.Contains(stdout, path) {
		t.Fatalf("stdout missing path %q: %q", path, stdout)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ServerURL != DefaultServerURL {
		t.Fatalf("ServerURL=%q, want %q", cfg.ServerURL, DefaultServerURL)
	}
}

func TestConfigInitRejectsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := SaveConfig(path, Config{ServerURL: "http://existing"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, stderr := captureStdio(t, func() {
		if code := run([]string{"config", "init", "--config", path}); code != 1 {
			t.Errorf("exit=%d, want 1 (existing without --force)", code)
		}
	})
	if !strings.Contains(stderr, "already exists") {
		t.Fatalf("stderr missing 'already exists': %q", stderr)
	}
	// 기존 값이 보존되었는지 확인.
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ServerURL != "http://existing" {
		t.Fatalf("ServerURL changed to %q, want preserved", cfg.ServerURL)
	}
}

func TestConfigInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := SaveConfig(path, Config{ServerURL: "http://existing"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var exitCode int
	_, stderr := captureStdio(t, func() {
		exitCode = run([]string{"config", "init", "--config", path, "--force",
			"--server", "http://new"})
	})
	if exitCode != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%q", exitCode, stderr)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ServerURL != "http://new" {
		t.Fatalf("ServerURL=%q, want overwritten http://new", cfg.ServerURL)
	}
}

func TestConfigInitWithCustomServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	captureStdio(t, func() {
		if code := run([]string{"config", "init", "--config", path,
			"--server", "http://example.com:9000"}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ServerURL != "http://example.com:9000" {
		t.Fatalf("ServerURL=%q", cfg.ServerURL)
	}
}

func TestConfigShowMissingFileExitsOne(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	_, stderr := captureStdio(t, func() {
		if code := run([]string{"config", "show", "--config", missing}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("stderr missing 'not found': %q", stderr)
	}
}

func TestConfigShowMasksTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const longToken = "0123456789abcdef0123456789abcdef0123456789abcdef" // 48자
	if err := SaveConfig(path, Config{
		ServerURL:    "http://x",
		AccessToken:  longToken,
		RefreshToken: longToken,
		Email:        "u@example.com",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"config", "show", "--config", path}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if strings.Contains(stdout, longToken) {
		t.Fatalf("stdout leaks raw token: %q", stdout)
	}
	if !strings.Contains(stdout, "01234567") {
		t.Fatalf("stdout missing token prefix: %q", stdout)
	}
}

func TestConfigShowJSONEmitsParseable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := SaveConfig(path, Config{ServerURL: "http://x", Email: "u@x"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"config", "show", "--config", path, "-o", "json"}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	var parsed map[string]string
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%q", err, stdout)
	}
	if parsed["serverUrl"] != "http://x" {
		t.Fatalf("serverUrl=%q", parsed["serverUrl"])
	}
}
