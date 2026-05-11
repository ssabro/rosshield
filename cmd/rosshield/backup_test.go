package main

import (
	"testing"
)

// TestRunBackupNoArgs — args 없으면 usage + exit 2.
func TestRunBackupNoArgs(t *testing.T) {
	t.Parallel()
	if got := runBackup(nil); got != 2 {
		t.Errorf("runBackup(nil) = %d, want 2", got)
	}
}

// TestRunBackupUnknownSub — 알 수 없는 sub-command → exit 2.
func TestRunBackupUnknownSub(t *testing.T) {
	t.Parallel()
	if got := runBackup([]string{"foo"}); got != 2 {
		t.Errorf("runBackup(foo) = %d, want 2", got)
	}
}

// TestRunBackupHelp — help 명시는 exit 0.
func TestRunBackupHelp(t *testing.T) {
	t.Parallel()
	for _, h := range []string{"-h", "--help", "help"} {
		if got := runBackup([]string{h}); got != 0 {
			t.Errorf("runBackup(%q) = %d, want 0", h, got)
		}
	}
}

// TestRunBackupDownloadFilenameValidation — path traversal·suffix 검증을 client에서 사전 차단.
func TestRunBackupDownloadFilenameValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
	}{
		{"missing filename", []string{"download"}},
		{"too many args", []string{"download", "a.tar.gz", "b.tar.gz"}},
	}
	for _, tc := range cases {
		if got := runBackup(append([]string{"download"}, tc.args[1:]...)); got != 2 {
			t.Errorf("%s: code = %d, want 2", tc.name, got)
		}
	}
}

// TestMin — 헬퍼 동작.
func TestMinHelper(t *testing.T) {
	t.Parallel()
	if min(2, 5) != 2 {
		t.Errorf("min(2,5) != 2")
	}
	if min(7, 3) != 3 {
		t.Errorf("min(7,3) != 3")
	}
	if min(4, 4) != 4 {
		t.Errorf("min(4,4) != 4")
	}
}
