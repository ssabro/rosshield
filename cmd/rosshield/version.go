package main

// version.go — `rosshield version` 서브커맨드 (E9 Stage A).
//
// Phase 1 단순화: 빌드 시점 ldflags 주입은 미구현. 상수 + runtime/Go 정보만 출력.
// Stage C 이후 build pipeline이 git SHA를 ldflags로 채울 예정.

import (
	"fmt"
	"runtime"
)

// Version은 CLI 빌드 버전입니다.
//
// Stage A는 0.1.0으로 고정 — Phase 1 exit 시 0.1.0-rc.X 패턴으로 ldflags 주입.
const Version = "0.1.0-dev"

// runVersion은 단일 라인 버전 정보를 stdout에 출력하고 exit code를 반환합니다.
func runVersion(_ []string) int {
	fmt.Printf("rosshield %s (%s/%s, %s)\n",
		Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	return 0
}
