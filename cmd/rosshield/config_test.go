package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadConfigReturnsZeroOnMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	cfg, err := LoadConfig(missing)
	if err == nil {
		t.Fatalf("err=nil, want os.IsNotExist")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err=%v, want os.ErrNotExist", err)
	}
	if cfg != (Config{}) {
		t.Fatalf("cfg not zero: %+v", cfg)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")
	want := Config{
		ServerURL:   "http://example.com:9000",
		AccessToken: "abc",
		Email:       "u@example.com",
	}
	if err := SaveConfig(path, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestSaveConfigEnforces0600Permission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod is best-effort")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := SaveConfig(path, Config{ServerURL: "http://x"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm=%o, want 0600", perm)
	}
	// 디렉터리도 0700인지 확인.
	dirSt, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirSt.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir perm=%o, want 0700", perm)
	}
}

func TestMaskTokenEmpty(t *testing.T) {
	if got := MaskToken(""); got != "" {
		t.Fatalf("empty token mask=%q, want empty", got)
	}
}

func TestMaskTokenShort(t *testing.T) {
	if got := MaskToken("abc"); got != "********" {
		t.Fatalf("short mask=%q, want all stars", got)
	}
}

func TestMaskTokenLong(t *testing.T) {
	tok := "0123456789abcdef0123456789abcdef0123456789abcdef" // 48자
	got := MaskToken(tok)
	if !strings.HasPrefix(got, "01234567") {
		t.Fatalf("mask=%q, want prefix 01234567", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("mask=%q, want suffix ...", got)
	}
	if strings.Contains(got, "abcdef") {
		t.Fatalf("mask=%q must not contain raw token tail", got)
	}
}

func TestDefaultConfigPathContainsRosshield(t *testing.T) {
	p := DefaultConfigPath()
	if !strings.Contains(p, "rosshield") {
		t.Fatalf("default path %q missing 'rosshield'", p)
	}
	if filepath.Base(p) != DefaultConfigName {
		t.Fatalf("base=%q, want %q", filepath.Base(p), DefaultConfigName)
	}
}
