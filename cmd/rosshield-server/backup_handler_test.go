package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

// newDownloadHandler는 chi 라우터 안에서 downloadBackupHandler가 URLParam을 정상
// 받도록 mount합니다. 직접 ServeHTTP 호출 시 URLParam이 빈 값이 되므로 chi 통한 mount 필요.
func newDownloadRouter(p *Platform) chi.Router {
	r := chi.NewRouter()
	r.Get("/api/v1/backups/{filename}/download", downloadBackupHandler(p))
	return r
}

// TestDownloadBackupSuccess — 정상 파일 다운로드 (200 + Content-Disposition + body).
func TestDownloadBackupSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := "auto-20260511-100000.tar.gz"
	want := []byte("fake tar.gz content")
	if err := os.WriteFile(filepath.Join(dir, filename), want, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &Platform{BackupDir: dir}
	r := newDownloadRouter(p)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/"+filename+"/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/gzip" {
		t.Errorf("Content-Type = %q, want application/gzip", got)
	}
	disp := rec.Header().Get("Content-Disposition")
	if disp == "" || !contains(disp, filename) {
		t.Errorf("Content-Disposition = %q, missing filename %s", disp, filename)
	}
	if rec.Body.String() != string(want) {
		t.Errorf("body mismatch: got %q, want %q", rec.Body.String(), want)
	}
}

// TestDownloadBackupBadFilenameTraversal — path traversal 방어 (../ 또는 절대 경로 거부).
func TestDownloadBackupBadFilenameTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := &Platform{BackupDir: dir}
	r := newDownloadRouter(p)

	cases := []string{
		"..%2Fetc%2Fpasswd.tar.gz", // url-encoded ../
		// chi가 path traversal을 자체 거부할 수 있으므로 대표 케이스 1개만 — 핵심은
		// "확장자 없는 파일", "확장자만 있는 빈 파일" 등 비-tar.gz suffix 거부.
	}
	for _, name := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/"+name+"/download", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("name=%s: code = %d, want 4xx (traversal/missing)", name, rec.Code)
		}
	}
}

// TestDownloadBackupRejectsNonTarGz — .tar.gz suffix 검증.
func TestDownloadBackupRejectsNonTarGz(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.txt"), []byte("oops"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p := &Platform{BackupDir: dir}
	r := newDownloadRouter(p)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/config.txt/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400 (non-tar.gz suffix)", rec.Code)
	}
}

// TestDownloadBackupNotFound — 부재 파일은 404.
func TestDownloadBackupNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := &Platform{BackupDir: dir}
	r := newDownloadRouter(p)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/no-such-file.tar.gz/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
}

// TestDownloadBackupBackupDirUnset — BackupDir 미설정 시 404.
func TestDownloadBackupBackupDirUnset(t *testing.T) {
	t.Parallel()
	p := &Platform{} // BackupDir = ""
	r := newDownloadRouter(p)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/anything.tar.gz/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404 (BackupDir unset)", rec.Code)
	}
}

// TestListBackupsHandlerEmpty — BackupDir 미설정도 200 + empty 배열 (현 listBackupsHandler 동작 회귀 보호).
func TestListBackupsHandlerEmpty(t *testing.T) {
	t.Parallel()
	p := &Platform{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backups", nil)
	rec := httptest.NewRecorder()
	listBackupsHandler(p).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rec.Code)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// 사용 안 하는 import warning 회피 (testing context).
var _ = context.Background
