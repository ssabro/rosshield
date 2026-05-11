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
	"net/http"
)

// BackupListResponse는 GET /api/v1/backups 응답 envelope입니다.
type BackupListResponse struct {
	Backups []BackupMeta `json:"backups"`
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
