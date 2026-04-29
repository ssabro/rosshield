package main

// config.go — rosshield CLI 설정 파일 모델 (E9 Stage A, R11-4).
//
// 위치: ~/.rosshield/config.yaml (디렉터리 0700, 파일 0600)
// 포맷: YAML — 이미 의존 중인 go.yaml.in/yaml/v3 사용 (R11 합의: 외부 라이브러리 추가 금지)
//
// 본 Stage A는 offline 명령(version/config/report verify)만 지원하므로 Token 필드들은
// 채워지지 않은 채 zero로 남습니다 — Stage C(login/whoami) 합류 시 채워짐. 미리 모델만
// 정의해두는 이유는 init이 만든 파일 스키마가 Stage C 합류 후에도 그대로 유효해야 하기
// 때문입니다 (한 번 init한 사용자가 다시 init하지 않아도 login만으로 진입 가능).
//
// 보안 노트: Windows에서는 os.Chmod의 mode bit이 read-only flag만 의미가 있어 0600/0700
// best-effort. Stage C가 실제 토큰을 다루기 시작할 때 ACL 강화 검토 필요(현재 범위 밖).

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Config는 ~/.rosshield/config.yaml의 와이어 형식입니다.
//
// 빈 값 필드는 omitempty로 직렬화에서 누락시켜 init 직후 파일을 깔끔하게 유지.
type Config struct {
	ServerURL    string `yaml:"serverUrl"`
	AccessToken  string `yaml:"accessToken,omitempty"`
	RefreshToken string `yaml:"refreshToken,omitempty"`
	Email        string `yaml:"email,omitempty"`
}

// 상수: spec R11-4·기본값.
const (
	DefaultConfigName = "config.yaml"
	DefaultServerURL  = "http://127.0.0.1:8080"
	defaultDirPerm    = 0o700
	defaultFilePerm   = 0o600
)

// DefaultConfigDir은 ~/.rosshield 또는 임시 fallback을 반환합니다.
//
// rosshield-server의 defaultDataDir과 동일 규칙 — UserHomeDir 실패 시 OS temp.
// 두 바이너리가 같은 디렉터리를 사용하므로 사용자 입장에서 단일 secrets 위치.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "rosshield")
	}
	return filepath.Join(home, ".rosshield")
}

// DefaultConfigPath는 ~/.rosshield/config.yaml을 반환합니다.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), DefaultConfigName)
}

// LoadConfig는 path에서 config를 로드합니다.
//
// 파일이 없으면 zero Config + os.IsNotExist 에러를 반환 — caller가 init을 권유할 수 있게.
// YAML 파싱 실패는 그대로 에러 전파(corrupted file은 사용자가 직접 수정 또는 재 init).
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

// SaveConfig는 path에 chmod 600으로 config를 저장합니다.
//
// 디렉터리는 0700으로 자동 생성. write는 atomic하지 않음(temp+rename 미구현) — Stage A
// 단순화. 동시 init 시 last-writer-wins 가정.
func SaveConfig(path string, cfg Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, defaultDirPerm); err != nil {
		return fmt.Errorf("mkdir %q: %w", dir, err)
	}
	// best-effort: dir이 이미 있는 경우 chmod 강제(Unix만 의미 있음).
	_ = os.Chmod(dir, defaultDirPerm)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, defaultFilePerm); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	// WriteFile의 perm은 새 파일에만 적용 — 기존 파일 덮어쓸 때를 위해 명시적 chmod.
	_ = os.Chmod(path, defaultFilePerm)
	return nil
}

// MaskToken은 토큰을 첫 8자만 보이게 마스킹합니다 (config show 출력용).
//
// 8자 미만이면 전체를 별표 처리 — partial 노출도 위험 가정.
// 빈 문자열은 빈 문자열 그대로(없는 필드 표시).
func MaskToken(t string) string {
	if t == "" {
		return ""
	}
	const visible = 8
	if len(t) <= visible {
		return "********"
	}
	return t[:visible] + "..."
}
