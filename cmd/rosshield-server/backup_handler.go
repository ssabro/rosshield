package main

// backup_handler.go — B7 후속: GET /api/v1/backups list endpoint.
//
// chi 직접 mount (openapi spec 표면 추가는 후속 — Stage 2). 인증 필요.
// download endpoint(GET /api/v1/backups/{filename}) + UI 통합은 Stage 2.
//
// 응답 형식:
//
//	{ "ok": true, "value": { "backups": [BackupMeta, ...] } }
//
// BackupDir 미설정 시 빈 배열 반환 (서버 자동 백업 비활성 운영도 정상 응답).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// BackupListResponse는 GET /api/v1/backups 응답 envelope입니다.
type BackupListResponse struct {
	Backups []BackupMeta `json:"backups"`
}

// downloadBackupHandler는 BackupDir 안의 단일 백업 .tar.gz를 다운로드합니다.
//
// 인증된 모든 사용자가 호출 가능 (Stage 2-C에서 RBAC role check 추가 — admin/auditor만).
// chi URLParam "filename"이 단순 파일명이어야 함 — path traversal은 listBackups와
// 동등 패턴(filepath.Base 검증 + .tar.gz suffix)으로 차단.
//
// http.ServeContent 사용 — Range request·If-Modified-Since·Last-Modified 자동 처리.
func downloadBackupHandler(p *Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(p.BackupDir) == "" {
			writeBackupErr(w, http.StatusNotFound, "BACKUP_DIR_UNSET", "automatic backup directory is not configured")
			return
		}
		filename := chi.URLParam(r, "filename")
		if filename == "" {
			writeBackupErr(w, http.StatusBadRequest, "BACKUP_FILENAME_REQUIRED", "filename path parameter is required")
			return
		}
		// path traversal 방어 — Base가 자기 자신과 같아야 함 + .tar.gz suffix.
		if filepath.Base(filename) != filename {
			writeBackupErr(w, http.StatusBadRequest, "BACKUP_FILENAME_INVALID", "filename must be a simple base name")
			return
		}
		if !strings.HasSuffix(filename, ".tar.gz") {
			writeBackupErr(w, http.StatusBadRequest, "BACKUP_FILENAME_INVALID", "filename must end with .tar.gz")
			return
		}

		full := filepath.Join(p.BackupDir, filename)
		f, err := os.Open(full) // #nosec G304 — Base+suffix 검증으로 BackupDir scope 제한
		if err != nil {
			if os.IsNotExist(err) {
				writeBackupErr(w, http.StatusNotFound, "BACKUP_NOT_FOUND", fmt.Sprintf("backup not found: %s", filename))
				return
			}
			writeBackupErr(w, http.StatusInternalServerError, "BACKUP_OPEN_FAILED", err.Error())
			return
		}
		defer func() { _ = f.Close() }()

		info, err := f.Stat()
		if err != nil {
			writeBackupErr(w, http.StatusInternalServerError, "BACKUP_STAT_FAILED", err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
		// http.ServeContent가 Content-Length·Last-Modified·Range 자동 처리.
		http.ServeContent(w, r, filename, info.ModTime(), f)
	}
}

// writeBackupErr는 envelope 형식 오류 응답을 송출합니다.
func writeBackupErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    false,
		"error": map[string]string{"code": code, "message": msg},
	})
}

// listBackupsHandler는 BackupDir에서 .tar.gz 파일을 list합니다.
//
// 인증된 모든 사용자가 호출 가능 (운영자가 web /system 페이지에서 백업 목록 확인용).
// 다운로드 권한 분리는 Stage 2 +/- RBAC role check.
func listBackupsHandler(p *Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		backups, err := listBackups(p.BackupDir)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": map[string]string{"code": "BACKUP_LIST_FAILED", "message": err.Error()},
			})
			return
		}
		if backups == nil {
			backups = []BackupMeta{}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    true,
			"value": BackupListResponse{Backups: backups},
		})
	}
}
