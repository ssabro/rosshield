package main

// version.go — `rosshield version` 서브커맨드.
//
// E26 (Phase 4): release pipeline이 -ldflags로 빌드 메타 주입.
//
//	-ldflags "-X main.Version=v0.2.0 -X main.BuildCommit=<sha> -X main.BuildTime=<iso>"
//
// 미주입 시 dev 기본값. README에 release 검증 절차(cosign verify-blob) 참조.

import (
	"fmt"
	"runtime"
)

// Version은 CLI 빌드 버전입니다 (release 시 ldflags 주입).
var Version = "0.1.0-dev"

// BuildCommit은 빌드 git SHA 짧은 형식입니다 (release 시 ldflags 주입).
var BuildCommit = "unknown"

// BuildTime은 빌드 timestamp ISO 8601입니다 (release 시 ldflags 주입).
var BuildTime = "unknown"

// runVersion은 빌드 메타와 runtime 정보를 stdout에 출력합니다.
func runVersion(_ []string) int {
	fmt.Printf("rosshield %s\n", Version)
	fmt.Printf("  commit: %s\n", BuildCommit)
	fmt.Printf("  built:  %s\n", BuildTime)
	fmt.Printf("  go:     %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return 0
}
