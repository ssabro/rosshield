package sshpool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// knownhosts.go — TOFU host key callback (scanrun SSH 통합 Stage 2).
//
// design doc `docs/design/notes/scanrun-ssh-integration-design.md` §6 Stage 2 + §5.1.
// `robot.HostKeyService`(Stage 1)를 통해 DB에 fingerprint 영속 + dataDir/keys/known_hosts
// 파일에 OpenSSH 호환 형식으로 이중 기록. Stage 3에서 bootstrap이 본 매니저를 결선해
// `xssh.InsecureIgnoreHostKey()` placeholder를 교체.
//
// 정책 (D-SCAN-2 권장 default = TOFU):
//   - 첫 호출 = `RecordFirstTouch` + 파일 append (trusted 마킹).
//   - 두 번째 이후 = `GetTrustedKey`로 fingerprint 비교, 일치하면 pass / 불일치 즉시 차단.
//   - 운영자 명시 reset 시 다음 호출이 first-touch처럼 동작.
//
// 파일은 부수효과 — DB가 진실의 원천. 파일 부재·손상 시 callback은 DB만으로 동작 보장.

// KnownHostsManager는 robot 별 host key 검증을 수행하는 ssh.HostKeyCallback 팩토리입니다.
type KnownHostsManager struct {
	svc     robot.HostKeyService
	store   storage.Storage
	dataDir string

	mu       sync.Mutex // 파일 append 직렬화 (concurrent dial 시 OS 단위 atomic이어도 race 회피).
	filePath string
}

// NewKnownHostsManager는 새 매니저를 반환합니다.
//
// dataDir 안에 `keys/known_hosts` 파일을 사용. 디렉토리는 부트 시점에 생성.
// svc는 robot.HostKeyService(또는 동등 어댑터)이며 nil 불가.
// store는 callback 안에서 새 Tx를 열기 위해 필요.
func NewKnownHostsManager(svc robot.HostKeyService, store storage.Storage, dataDir string) (*KnownHostsManager, error) {
	if svc == nil {
		return nil, errors.New("sshpool: KnownHostsManager: HostKeyService is required")
	}
	if store == nil {
		return nil, errors.New("sshpool: KnownHostsManager: Storage is required")
	}
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("sshpool: KnownHostsManager: dataDir is required")
	}
	keysDir := filepath.Join(dataDir, "keys")
	if err := os.MkdirAll(keysDir, 0o700); err != nil {
		return nil, fmt.Errorf("sshpool: mkdir %s: %w", keysDir, err)
	}
	filePath := filepath.Join(keysDir, "known_hosts")
	// 파일이 없으면 빈 파일로 생성 — 권한 0600.
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("sshpool: create %s: %w", filePath, err)
		}
		_ = f.Close()
	}
	return &KnownHostsManager{
		svc:      svc,
		store:    store,
		dataDir:  dataDir,
		filePath: filePath,
	}, nil
}

// HostKeyCallback는 (tenantID, robotID) 한정 ssh.HostKeyCallback을 반환합니다.
//
// closure로 ctx + IDs 캡처. ssh.NewClientConn이 dial 시점에 본 callback을 호출.
//
// 절차:
//  1. 새 Tx 열기 (tenantCtx).
//  2. GetTrustedKey(robotID) — fingerprint 조회.
//  3. NotFound → first-touch — RecordFirstTouch + 파일 append + nil 반환(trust).
//  4. fingerprint 일치 → nil(trust).
//  5. fingerprint 불일치 → ErrHostKeyMismatch 반환(connection 차단).
//
// audit emit은 robot.HostKeyService의 RecordFirstTouch가 위임 — 본 매니저는 emit 직접 안 함.
func (m *KnownHostsManager) HostKeyCallback(ctx context.Context, tenantID storage.TenantID, robotID string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)
		blob := key.Marshal()
		keyType := key.Type()

		var firstTouch bool
		txCtx := storage.WithTenantID(ctx, tenantID)
		err := m.store.Tx(txCtx, func(ctx context.Context, tx storage.Tx) error {
			trusted, err := m.svc.GetTrustedKey(ctx, tx, robotID)
			if errors.Is(err, storage.ErrNotFound) {
				firstTouch = true
				_, recErr := m.svc.RecordFirstTouch(ctx, tx, robot.RecordFirstTouchRequest{
					RobotID:           robotID,
					FingerprintSHA256: fp,
					KeyType:           keyType,
					KeyBlob:           blob,
				})
				return recErr
			}
			if err != nil {
				return err
			}
			if trusted.FingerprintSHA256 != fp {
				return robot.ErrHostKeyMismatch
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("sshpool: host key verify %s/%s @ %s: %w",
				tenantID, robotID, hostname, err)
		}

		if firstTouch {
			// 파일 append 부수효과 — DB가 진실의 원천이라 파일 실패는 connection 차단 X.
			if appendErr := m.appendToFile(hostname, remote, key); appendErr != nil {
				// best-effort warn 로그 등은 callback 시점에는 logger 부재 — 무시. Stage 3에서 logger 결선 시 추가.
				_ = appendErr
			}
		}
		return nil
	}
}

// appendToFile은 OpenSSH known_hosts 호환 형식으로 한 라인을 파일에 추가합니다.
//
// 형식: "<hostname-or-ip> <key-type> <base64-blob>\n" — `ssh-keyscan` 출력과 동일.
// `golang.org/x/crypto/ssh/knownhosts.Line()`을 사용해 정확한 형식 보장.
func (m *KnownHostsManager) appendToFile(hostname string, remote net.Addr, key ssh.PublicKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	addrs := []string{hostname}
	if remote != nil {
		if a := remote.String(); a != "" && a != hostname {
			addrs = append(addrs, a)
		}
	}
	line := knownhosts.Line(addrs, key)
	f, err := os.OpenFile(m.filePath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("sshpool: open known_hosts: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("sshpool: append known_hosts: %w", err)
	}
	return nil
}

// FilePath는 known_hosts 파일 경로를 반환합니다 (단위 테스트 편의용).
func (m *KnownHostsManager) FilePath() string {
	return m.filePath
}
