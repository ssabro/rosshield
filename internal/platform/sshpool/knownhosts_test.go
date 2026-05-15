package sshpool_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/sshpool/sshpooltest"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// knownhosts_test.go — KnownHostsManager TOFU 단위 테스트 (scanrun SSH 통합 Stage 2).
//
// design doc `docs/design/notes/scanrun-ssh-integration-design.md` §6 Stage 2 검증:
//   - first-touch trust (FakeSSHD A 첫 접속 → DB INSERT + 파일 append + connection 성공)
//   - 일치 (FakeSSHD A 두 번째 접속 → fingerprint 일치 → 통과)
//   - 불일치 (FakeSSHD B 접속 → ErrHostKeyMismatch 차단)
//   - 운영자 ResetTrust 후 재 first-touch 정상 동작

// fakeHostKeyService는 단위 테스트용 in-memory robot.HostKeyService입니다.
//
// 실 sqliterepo는 robot 도메인 의존이 큰 fixture를 요구(fleet+credential+robot raw INSERT)
// — sshpool 단위 테스트는 도메인 격리 위해 in-memory stub만 사용. sqliterepo round-trip 검증은
// robot/sqliterepo 단위 테스트에서 별도 cover (Stage 1 11 단위).
type fakeHostKeyService struct {
	mu      sync.Mutex
	trusted map[string]robot.RobotHostKey // key: tenantID + ":" + robotID
	emitted []string                      // first_touched/reset 호출 기록
	idCount int
}

func newFakeHostKeyService() *fakeHostKeyService {
	return &fakeHostKeyService{trusted: map[string]robot.RobotHostKey{}}
}

func keyOf(tenantID storage.TenantID, robotID string) string {
	return string(tenantID) + ":" + robotID
}

func (f *fakeHostKeyService) RecordFirstTouch(ctx context.Context, tx storage.Tx, req robot.RecordFirstTouchRequest) (robot.RobotHostKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.RobotHostKey{}, storage.ErrTenantMissing
	}
	k := keyOf(tenantID, req.RobotID)
	now := time.Now().UTC()
	if existing, ok := f.trusted[k]; ok && existing.FingerprintSHA256 == req.FingerprintSHA256 {
		// 멱등 — LastVerifiedAt 갱신.
		existing.LastVerifiedAt = now
		existing.TrustState = robot.HostKeyTrustStateTrusted
		f.trusted[k] = existing
		return existing, nil
	}
	f.idCount++
	hk := robot.RobotHostKey{
		ID:                "hk_fake_" + strings.Repeat("0", f.idCount),
		TenantID:          tenantID,
		RobotID:           req.RobotID,
		FingerprintSHA256: req.FingerprintSHA256,
		KeyType:           req.KeyType,
		KeyBlob:           append([]byte(nil), req.KeyBlob...),
		FirstSeenAt:       now,
		LastVerifiedAt:    now,
		TrustState:        robot.HostKeyTrustStateTrusted,
	}
	f.trusted[k] = hk
	f.emitted = append(f.emitted, "first_touched:"+k)
	return hk, nil
}

func (f *fakeHostKeyService) GetTrustedKey(ctx context.Context, tx storage.Tx, robotID string) (robot.RobotHostKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.RobotHostKey{}, storage.ErrTenantMissing
	}
	k := keyOf(tenantID, robotID)
	hk, ok := f.trusted[k]
	if !ok || hk.TrustState != robot.HostKeyTrustStateTrusted {
		return robot.RobotHostKey{}, storage.ErrNotFound
	}
	return hk, nil
}

func (f *fakeHostKeyService) ResetTrust(ctx context.Context, tx storage.Tx, robotID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := tx.TenantID()
	if tenantID == "" {
		return 0, storage.ErrTenantMissing
	}
	k := keyOf(tenantID, robotID)
	if hk, ok := f.trusted[k]; ok && hk.TrustState == robot.HostKeyTrustStateTrusted {
		hk.TrustState = robot.HostKeyTrustStateRevoked
		f.trusted[k] = hk
		f.emitted = append(f.emitted, "reset:"+k)
		return 1, nil
	}
	return 0, nil
}

func (f *fakeHostKeyService) emittedEvents() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.emitted))
	copy(out, f.emitted)
	return out
}

// newKnownHostsTestStore는 in-memory sqlite store + fake host key service를 반환합니다.
//
// store는 callback 안에서 Tx 시작용으로만 사용 — 실 robot_host_keys 테이블 SELECT/INSERT는
// fakeHostKeyService에 위임. Tx의 TenantID 추출은 storage.Tx 표면 통해 동작 보장.
func newKnownHostsTestStore(t *testing.T) (*sshpool.KnownHostsManager, *fakeHostKeyService, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kh.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	svc := newFakeHostKeyService()
	mgr, err := sshpool.NewKnownHostsManager(svc, store, dir)
	if err != nil {
		t.Fatalf("NewKnownHostsManager: %v", err)
	}
	return mgr, svc, dir
}

// dialWithCallback는 fakeSSHD에 dial하고 callback을 통해 host key 검증을 수행합니다.
//
// callback이 거부하면 NewClientConn이 에러 반환 — 그 에러를 그대로 반환 (호출자가 에러 분류).
func dialWithCallback(t *testing.T, srv *sshpooltest.FakeSSHD, cb ssh.HostKeyCallback) error {
	t.Helper()
	addr := srv.Host + ":" + intToStr(srv.Port)
	cfg := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.Password("ignored-by-fakesshd")},
		HostKeyCallback: cb,
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return err
	}
	_ = client.Close()
	return nil
}

func intToStr(n int) string {
	// strconv.Itoa 회피 — 단순 변환.
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestKnownHosts_FirstTouchTrust(t *testing.T) {
	t.Parallel()
	mgr, svc, _ := newKnownHostsTestStore(t)
	srv := sshpooltest.New(t, nil)

	cb := mgr.HostKeyCallback(context.Background(), "ten_a", "ro_1")
	if err := dialWithCallback(t, srv, cb); err != nil {
		t.Fatalf("first-touch dial: %v", err)
	}

	// fakeHostKeyService에 trusted row 생성됨.
	events := svc.emittedEvents()
	if len(events) != 1 || events[0] != "first_touched:ten_a:ro_1" {
		t.Errorf("events = %v, want exactly 1 first_touched", events)
	}
}

func TestKnownHosts_SecondCallSameKeyPasses(t *testing.T) {
	t.Parallel()
	mgr, svc, _ := newKnownHostsTestStore(t)
	srv := sshpooltest.New(t, nil)

	cb := mgr.HostKeyCallback(context.Background(), "ten_a", "ro_1")
	if err := dialWithCallback(t, srv, cb); err != nil {
		t.Fatalf("first-touch dial: %v", err)
	}
	// 두 번째 dial — 같은 key, callback 통과.
	if err := dialWithCallback(t, srv, cb); err != nil {
		t.Fatalf("second dial (same key): %v", err)
	}

	// audit emit은 first_touch 1회만 (idempotent).
	events := svc.emittedEvents()
	if len(events) != 1 {
		t.Errorf("events = %v, want 1 (idempotent emit suppression)", events)
	}
}

func TestKnownHosts_DifferentKeyMismatchBlocks(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newKnownHostsTestStore(t)
	srvA := sshpooltest.New(t, nil)
	srvB := sshpooltest.New(t, nil) // 다른 host key (각 fakesshd가 무작위 ed25519 생성).

	cb := mgr.HostKeyCallback(context.Background(), "ten_a", "ro_1")

	// 1. srvA first-touch — 성공.
	if err := dialWithCallback(t, srvA, cb); err != nil {
		t.Fatalf("first-touch srvA: %v", err)
	}

	// 2. srvB로 dial — fingerprint 불일치, callback이 ErrHostKeyMismatch 반환 → ssh.Dial 에러.
	err := dialWithCallback(t, srvB, cb)
	if err == nil {
		t.Fatal("dial srvB should fail (host key mismatch), got nil")
	}
	if !strings.Contains(err.Error(), "host key") && !errors.Is(err, robot.ErrHostKeyMismatch) {
		// ssh.Dial은 callback 에러를 wrap해서 반환 — 메시지에 host key 포함 또는 wrap된 sentinel.
		// 둘 중 하나는 만족해야 함.
		t.Errorf("err = %v, want host key mismatch in message or sentinel", err)
	}
}

func TestKnownHosts_ResetTrustAllowsNewKey(t *testing.T) {
	t.Parallel()
	mgr, svc, _ := newKnownHostsTestStore(t)
	srvA := sshpooltest.New(t, nil)
	srvB := sshpooltest.New(t, nil)

	cb := mgr.HostKeyCallback(context.Background(), "ten_a", "ro_1")

	// srvA first-touch.
	if err := dialWithCallback(t, srvA, cb); err != nil {
		t.Fatalf("first-touch srvA: %v", err)
	}
	// srvB dial — mismatch (차단 예상).
	if err := dialWithCallback(t, srvB, cb); err == nil {
		t.Fatal("srvB dial should fail before reset")
	}

	// 운영자 ResetTrust — 다음 first-touch가 새 키를 trusted로 등록.
	if err := svc_resetTrust(context.Background(), svc, "ten_a", "ro_1"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// 같은 callback으로 srvB dial — 재 first-touch로 통과.
	if err := dialWithCallback(t, srvB, cb); err != nil {
		t.Fatalf("srvB dial after reset: %v", err)
	}

	events := svc.emittedEvents()
	// 1차 first_touched(srvA) + reset + 2차 first_touched(srvB) = 3건.
	if len(events) != 3 {
		t.Errorf("events = %v, want 3 (first + reset + first)", events)
	}
}

// svc_resetTrust는 fakeHostKeyService에 직접 호출하는 편의 함수입니다 (Tx 결선 단순화).
func svc_resetTrust(ctx context.Context, svc *fakeHostKeyService, tenantID storage.TenantID, robotID string) error {
	// fake는 Tx tenantID만 추출 — minimal Tx 구현은 곤란하므로 직접 mu lock + 상태 변경.
	svc.mu.Lock()
	defer svc.mu.Unlock()
	k := keyOf(tenantID, robotID)
	if hk, ok := svc.trusted[k]; ok && hk.TrustState == robot.HostKeyTrustStateTrusted {
		hk.TrustState = robot.HostKeyTrustStateRevoked
		svc.trusted[k] = hk
		svc.emitted = append(svc.emitted, "reset:"+k)
	}
	return nil
}

func TestKnownHosts_FilePathExposed(t *testing.T) {
	t.Parallel()
	mgr, _, dir := newKnownHostsTestStore(t)
	want := filepath.Join(dir, "keys", "known_hosts")
	if mgr.FilePath() != want {
		t.Errorf("FilePath = %q, want %q", mgr.FilePath(), want)
	}
}
