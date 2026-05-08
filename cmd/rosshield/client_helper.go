package main

// client_helper.go — config 로드 + 인증 토큰 검증 + Client 생성 공통 흐름.
//
// 모든 online 서브커맨드(invite·webhook·license·whoami·robot·scan)가 같은 4 단계 패턴을 반복:
//
//	1. configPath 결정 (DefaultConfigPath fallback)
//	2. LoadConfig → 실패 시 exit 1
//	3. AccessToken 검증 → 빈 값이면 안내 후 exit 2
//	4. NewClient
//
// helper로 추출해 호출 측 보일러플레이트 제거.

import (
	"fmt"
	"os"
)

// newAuthenticatedClient 는 LoadConfig + token 검증 + NewClient 를 한 번에 수행합니다.
//
// 반환: (client, 0) 성공 / (nil, exitCode) 실패.
// who는 stderr 메시지 앞 prefix (예: "rosshield invite create").
func newAuthenticatedClient(configPath, who string) (*Client, int) {
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = DefaultConfigPath()
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load config: %v\n", who, err)
		return nil, 1
	}
	if cfg.AccessToken == "" {
		fmt.Fprintf(os.Stderr, "%s: no access token (run `rosshield login` first)\n", who)
		return nil, 2
	}
	return NewClient(cfg), 0
}
