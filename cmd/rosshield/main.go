// Command rosshield는 rosshield 플랫폼의 얇은 CLI 클라이언트입니다 (E9 Stage A).
//
// 기존 `rosshield-server` 바이너리는 서버 전용; `rosshield`는 §09 spec의 클라이언트 진입점.
// Stage A 범위는 **offline 명령**만:
//
//	version           CLI 버전·런타임 정보
//	config init|show  ~/.rosshield/config.yaml 관리 (R11-4)
//	report verify     서명된 tar.gz 번들 외부 검증 (R11-8)
//
// HTTP 클라이언트(login·whoami·robot·scan·report list)는 Stage C에서 추가.
// WebSocket·실시간 스트림은 Stage D.
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run은 args를 받아 exit code를 반환합니다 (테스트 친화 분리).
func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "version":
		return runVersion(args[1:])
	case "config":
		return runConfig(args[1:])
	case "report":
		return runReport(args[1:])
	case "login":
		return runLogin(args[1:])
	case "whoami":
		return runWhoami(args[1:])
	case "robot":
		return runRobot(args[1:])
	case "scan":
		return runScan(args[1:])
	case "license":
		return runLicense(args[1:])
	case "invite":
		return runInvite(args[1:])
	case "webhook":
		return runWebhook(args[1:])
	case "ha":
		return runHA(args[1:])
	case "backup":
		return runBackup(args[1:])
	case "audit":
		return runAudit(args[1:])
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "rosshield: unknown command %q\n\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `rosshield — rosshield 플랫폼 CLI 클라이언트 (Phase 1 Stage A)

사용법:
  rosshield <subcommand> [args]

Offline 서브커맨드:
  version                                      CLI 버전·런타임 정보
  config init [--server URL] [--force]         ~/.rosshield/config.yaml 생성
  config show [-o table|json]                  현재 config (token 마스킹) 표시
  report verify <bundle.tar.gz> [-o ...]       서명된 PDF 번들 검증 (오프라인)
  help | --help | -h                           본 사용법

Online 서브커맨드 (Stage C — config 토큰 사용):
  login --email E [--password P|--password-stdin] [--server URL]
  whoami [-o ...]
  robot list [--fleet ID] [-o ...]
  scan run --fleet ID --pack ID [--trigger T] [--total N] [-o ...]
  report list [--session ID] [-o ...]
  license info [-o ...]                        Open-core 라이선스 메타 (E24)
  invite create --email E --role R [...]       사용자 초대 생성 (token + accept URL 1회 노출, E29)
  invite list [-o ...]                         테넌트 초대 목록
  invite revoke <id>                           초대 즉시 만료
  webhook list [-o ...]                        Webhook endpoint 조회 (E29)
  ha status [-o ...] [--server URL]            HA leader/follower 상태 (E25, /healthz fetch)
  backup list [-o ...]                         자동 백업 목록 (B7)
  backup download <filename> [--output P]      자동 백업 다운로드 (B7)
  audit rotation abort --reason "<text>"       audit chain key rotation 차단 (Phase 10.D-6)

Online 서브커맨드 (Stage D — 합류 예정):
  scan status <id> / audit verify

글로벌 옵션:
  -o table|json    출력 포맷 (기본 table)
  --config <path>  config 파일 경로 (기본 ~/.rosshield/config.yaml)

exit code:
  0  성공
  1  I/O·파일 부재·기존 config 충돌·번들 parse 실패
  2  invalid CLI args (필수 옵션 누락 또는 알 수 없는 subcommand)
  3  signature·verify 실패 (report verify 한정)`)
}
