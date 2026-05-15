package sqliterepo_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// timeNowRFC는 테스트용 timestamp 생성 헬퍼입니다.
func timeNowRFC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// host_key_test.go — TOFU host key 단위 테스트 (scanrun SSH 통합 Stage 1).
//
// design doc `docs/design/notes/scanrun-ssh-integration-design.md` §6 Stage 1 검증:
//   - tenant 격리(원칙 §4)
//   - idempotent first-touch (같은 fingerprint 중복 호출은 같은 row 반환, audit emit 1회)
//   - fingerprint UNIQUE (다른 fingerprint는 별 row)
//   - GetTrustedKey 미존재 시 ErrNotFound
//   - ResetTrust 후 트랜잭션 격리
//   - revoked → trusted 복구 시 audit emit
//   - audit emitter 호출 카운트 검증

// recordingHostKeyAudit는 단위 테스트용 audit emitter입니다 — emit 호출을 in-memory에 기록합니다.
type recordingHostKeyAudit struct {
	mu      sync.Mutex
	emitted []hostKeyAuditEvent
	emitErr error
}

type hostKeyAuditEvent struct {
	Action       string
	TenantID     storage.TenantID
	RobotID      string
	Fingerprint  string
	RevokedCount int
}

func (r *recordingHostKeyAudit) EmitHostKeyFirstTouched(_ context.Context, _ storage.Tx, k robot.RobotHostKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.emitErr != nil {
		return r.emitErr
	}
	r.emitted = append(r.emitted, hostKeyAuditEvent{
		Action: "robot.host_key.first_touched", TenantID: k.TenantID,
		RobotID: k.RobotID, Fingerprint: k.FingerprintSHA256,
	})
	return nil
}

func (r *recordingHostKeyAudit) EmitHostKeyChanged(_ context.Context, _ storage.Tx, robotID string, tenantID storage.TenantID, oldFp, newFp string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.emitErr != nil {
		return r.emitErr
	}
	r.emitted = append(r.emitted, hostKeyAuditEvent{
		Action: "robot.host_key.changed", TenantID: tenantID,
		RobotID: robotID, Fingerprint: oldFp + "->" + newFp,
	})
	return nil
}

func (r *recordingHostKeyAudit) EmitHostKeyReset(_ context.Context, _ storage.Tx, robotID string, tenantID storage.TenantID, count int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.emitErr != nil {
		return r.emitErr
	}
	r.emitted = append(r.emitted, hostKeyAuditEvent{
		Action: "robot.host_key.reset", TenantID: tenantID,
		RobotID: robotID, RevokedCount: count,
	})
	return nil
}

func (r *recordingHostKeyAudit) events() []hostKeyAuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]hostKeyAuditEvent, len(r.emitted))
	copy(out, r.emitted)
	return out
}

// newHostKeyTestRepo는 host_key 테스트 전용 Repo입니다 — HostKeyAudit 결선 + recording emitter 반환.
func newHostKeyTestRepo(t *testing.T) (*sqliterepo.Repo, audit.Service, storage.Storage, *recordingHostKeyAudit) {
	t.Helper()
	_, auditSvc, store, dbPath := newTestRepoFull(t)
	_ = dbPath
	rec := &recordingHostKeyAudit{}
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock:        clock.System(),
		IDGen:        idgen.NewULID(),
		Audit:        &auditAdapter{svc: auditSvc},
		HostKeyAudit: rec,
	})
	return repo, auditSvc, store, rec
}

// fingerprint는 "SHA256:<base64>" placeholder 생성 헬퍼입니다 (단위 테스트는 형식 검증만).
func fingerprint(suffix string) string {
	return "SHA256:" + suffix
}

// seedRobotForHostKey는 robot_host_keys.robot_id FK를 만족시키기 위해 fleet + credential + robot
// 최소 row를 raw INSERT합니다 — 도메인 호출 회피로 테스트 단순화. robot 도메인 변경 영향 0.
//
// robot_id는 호출자가 지정한 그대로 사용 — host_key 테스트에서 robot_id로 row를 조회하기 때문.
// 다중 tenant에서 같은 robotID를 쓰는 경우(cross-tenant 격리 테스트)는 호출자가 tenant 별로 다른
// robotID를 쓰면 됨 — 본 함수는 raw INSERT의 robots.id PK 충돌 회피를 위해 그대로 사용.
func seedRobotForHostKey(t *testing.T, store storage.Storage, tenantID, robotID string) {
	t.Helper()
	now := timeNowRFC()
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		fleetID := "fl_seed_" + tenantID + "_" + robotID
		credID := "cr_seed_" + tenantID + "_" + robotID
		if _, err := tx.Exec(ctx, `
INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at)
VALUES (?, ?, ?, ?, '{}', ?, ?)`,
			fleetID, tenantID, "fleet-"+robotID, "", now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO credentials (id, tenant_id, type, encrypted_payload, encryption_meta, created_at, updated_at)
VALUES (?, ?, 'password', X'00', '{}', ?, ?)`,
			credID, tenantID, now, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
INSERT INTO robots (id, tenant_id, fleet_id, credential_id, name, host, port, auth_type,
                    os_distro, ros_distro, tags, role, criticality, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 22, 'password', '', '', '[]', '', 'medium', ?, ?)`,
			robotID, tenantID, fleetID, credID, "robot-"+robotID, "127.0.0.1", now, now)
		return err
	}); err != nil {
		t.Fatalf("seedRobotForHostKey %s/%s: %v", tenantID, robotID, err)
	}
}

// keyBlob는 "ssh-ed25519 ..." marshalled bytes placeholder입니다 (실 marshal은 Stage 2에서).
func keyBlob(seed string) []byte {
	return []byte("blob:" + seed)
}

func sampleFirstTouch(robotID, fpSuffix string) robot.RecordFirstTouchRequest {
	return robot.RecordFirstTouchRequest{
		RobotID:           robotID,
		FingerprintSHA256: fingerprint(fpSuffix),
		KeyType:           "ssh-ed25519",
		KeyBlob:           keyBlob(fpSuffix),
	}
}

func TestHostKey_RecordFirstTouch_Insert(t *testing.T) {
	repo, _, store, rec := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)
	seedRobotForHostKey(t, store, tenantID, "ro_1")

	var got robot.RobotHostKey
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		got, err = repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("RecordFirstTouch: %v", err)
	}
	if got.ID == "" || got.TenantID != storage.TenantID(tenantID) || got.RobotID != "ro_1" {
		t.Errorf("returned row missing fields: %+v", got)
	}
	if got.TrustState != robot.HostKeyTrustStateTrusted {
		t.Errorf("trust state = %q, want trusted", got.TrustState)
	}
	if got.FirstSeenAt.IsZero() || got.LastVerifiedAt.IsZero() {
		t.Errorf("timestamps not set: %+v", got)
	}
	events := rec.events()
	if len(events) != 1 || events[0].Action != "robot.host_key.first_touched" {
		t.Errorf("audit events = %+v, want exactly 1 first_touched", events)
	}
}

func TestHostKey_RecordFirstTouch_IdempotentSameFingerprint(t *testing.T) {
	repo, _, store, rec := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)
	seedRobotForHostKey(t, store, tenantID, "ro_1")

	var first, second robot.RobotHostKey
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		first, err = repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		second, err = repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("idempotent ID mismatch: first=%q second=%q", first.ID, second.ID)
	}
	// audit는 신규 INSERT 시 1회만 — 중복 호출은 emit 안 함 (noise 회피).
	events := rec.events()
	if len(events) != 1 {
		t.Errorf("audit events = %d, want 1 (idempotent emit suppression)", len(events))
	}
}

func TestHostKey_RecordFirstTouch_DifferentFingerprintsSeparateRows(t *testing.T) {
	repo, _, store, _ := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)
	seedRobotForHostKey(t, store, tenantID, "ro_1")

	var first, second robot.RobotHostKey
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		first, err = repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("alpha: %v", err)
	}
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		second, err = repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "beta"))
		return err
	}); err != nil {
		t.Fatalf("beta: %v", err)
	}
	if first.ID == second.ID {
		t.Errorf("expected separate rows for different fingerprints, got same ID %q", first.ID)
	}
	if first.FingerprintSHA256 == second.FingerprintSHA256 {
		t.Errorf("expected different fingerprints, got both %q", first.FingerprintSHA256)
	}
}

func TestHostKey_RecordFirstTouch_RevokedToTrustedRecoveryEmitsAudit(t *testing.T) {
	repo, _, store, rec := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)
	seedRobotForHostKey(t, store, tenantID, "ro_1")

	// 1. first-touch.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("first touch: %v", err)
	}

	// 2. ResetTrust → revoked.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.ResetTrust(ctx, tx, "ro_1")
		return err
	}); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// 3. 같은 fingerprint로 재 first-touch — 'revoked' → 'trusted' 복구 + audit emit 추가.
	var recovered robot.RobotHostKey
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		recovered, err = repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered.TrustState != robot.HostKeyTrustStateTrusted {
		t.Errorf("recovered trust state = %q, want trusted", recovered.TrustState)
	}

	events := rec.events()
	// 1차 first_touched + reset + 복구 first_touched = 3건.
	if len(events) != 3 {
		t.Fatalf("audit events = %d, want 3 (first + reset + recover)", len(events))
	}
	if events[0].Action != "robot.host_key.first_touched" ||
		events[1].Action != "robot.host_key.reset" ||
		events[2].Action != "robot.host_key.first_touched" {
		t.Errorf("event sequence = [%s, %s, %s], want [first, reset, first]",
			events[0].Action, events[1].Action, events[2].Action)
	}
}

func TestHostKey_GetTrustedKey_Found(t *testing.T) {
	repo, _, store, _ := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)
	seedRobotForHostKey(t, store, tenantID, "ro_1")

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var got robot.RobotHostKey
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		got, err = repo.GetTrustedKey(ctx, tx, "ro_1")
		return err
	}); err != nil {
		t.Fatalf("GetTrustedKey: %v", err)
	}
	if got.RobotID != "ro_1" || got.FingerprintSHA256 != fingerprint("alpha") {
		t.Errorf("got = %+v, want robot ro_1 fp alpha", got)
	}
}

func TestHostKey_GetTrustedKey_NotFound(t *testing.T) {
	repo, _, store, _ := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetTrustedKey(ctx, tx, "ro_unknown")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

func TestHostKey_GetTrustedKey_RevokedNotReturned(t *testing.T) {
	repo, _, store, _ := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)
	seedRobotForHostKey(t, store, tenantID, "ro_1")

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.ResetTrust(ctx, tx, "ro_1")
		return err
	}); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetTrustedKey(ctx, tx, "ro_1")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("after reset, err = %v, want ErrNotFound (revoked row excluded)", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}
}

func TestHostKey_ResetTrust_RevokesAndReturnsCount(t *testing.T) {
	repo, _, store, rec := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)
	seedRobotForHostKey(t, store, tenantID, "ro_1")

	// 두 fingerprint 각각 first-touch — robot당 다중 trusted row(이론적으로 RecordFirstTouch는
	// 같은 fingerprint UNIQUE이지만 다른 fingerprint는 추가 가능).
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		if _, err := repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "alpha")); err != nil {
			return err
		}
		_, err := repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_1", "beta"))
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var revoked int
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		revoked, err = repo.ResetTrust(ctx, tx, "ro_1")
		return err
	}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if revoked != 2 {
		t.Errorf("revoked count = %d, want 2", revoked)
	}

	events := rec.events()
	// 2 first_touched + 1 reset = 3건.
	if len(events) != 3 {
		t.Fatalf("audit events = %d, want 3", len(events))
	}
	last := events[len(events)-1]
	if last.Action != "robot.host_key.reset" || last.RevokedCount != 2 {
		t.Errorf("last event = %+v, want reset with RevokedCount=2", last)
	}
}

func TestHostKey_ResetTrust_NoOpEmitsNoAudit(t *testing.T) {
	repo, _, store, rec := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)

	// trusted row 없음 — reset은 0 반환 + audit emit 0.
	var revoked int
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		revoked, err = repo.ResetTrust(ctx, tx, "ro_unknown")
		return err
	}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if revoked != 0 {
		t.Errorf("revoked count = %d, want 0", revoked)
	}
	if events := rec.events(); len(events) != 0 {
		t.Errorf("audit events = %d, want 0 (no-op suppresses emit)", len(events))
	}
}

func TestHostKey_TenantIsolation(t *testing.T) {
	repo, _, store, _ := newHostKeyTestRepo(t)
	seedTenant(t, store, "ten_a")
	seedTenant(t, store, "ten_b")
	// robot.id는 글로벌 PK라 tenant 별로 다른 ID 사용 — cross-tenant 격리 자체는
	// (tenant_id, robot_id) WHERE 절에서 검증.
	seedRobotForHostKey(t, store, "ten_a", "ro_a")
	seedRobotForHostKey(t, store, "ten_b", "ro_b")

	// ten_a에 first-touch.
	if err := store.Tx(tenantCtx("ten_a"), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_a", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("seed ten_a: %v", err)
	}

	// ten_b 다른 tenant scope로 ten_a의 robot에 대해 GetTrustedKey → ErrNotFound (cross-tenant 격리).
	if err := store.Tx(tenantCtx("ten_b"), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetTrustedKey(ctx, tx, "ro_a")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("ten_b GetTrustedKey on ten_a's robot = %v, want ErrNotFound (cross-tenant leak)", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx ten_b: %v", err)
	}

	// ten_b first-touch — 별 row 생성.
	if err := store.Tx(tenantCtx("ten_b"), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordFirstTouch(ctx, tx, sampleFirstTouch("ro_b", "alpha"))
		return err
	}); err != nil {
		t.Fatalf("ten_b first-touch: %v", err)
	}

	// ten_b reset → 본 tenant row만 영향. ten_a는 변동 없음.
	if err := store.Tx(tenantCtx("ten_b"), func(ctx context.Context, tx storage.Tx) error {
		revoked, err := repo.ResetTrust(ctx, tx, "ro_b")
		if err != nil {
			t.Errorf("ten_b reset: %v", err)
		}
		if revoked != 1 {
			t.Errorf("ten_b reset revoked = %d, want 1 (only ten_b row affected)", revoked)
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx ten_b reset: %v", err)
	}
	// ten_a row는 그대로 trusted.
	if err := store.Tx(tenantCtx("ten_a"), func(ctx context.Context, tx storage.Tx) error {
		got, err := repo.GetTrustedKey(ctx, tx, "ro_a")
		if err != nil {
			t.Errorf("ten_a row affected by ten_b reset: %v", err)
		}
		if got.TenantID != "ten_a" {
			t.Errorf("ten_a query returned row from %q (cross-tenant leak)", got.TenantID)
		}
		return nil
	}); err != nil {
		t.Fatalf("Tx ten_a verify after reset: %v", err)
	}
}

func TestHostKey_RecordFirstTouch_ValidationErrors(t *testing.T) {
	repo, _, store, _ := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)

	cases := []struct {
		name    string
		req     robot.RecordFirstTouchRequest
		wantErr error
	}{
		{
			name:    "empty robot id",
			req:     robot.RecordFirstTouchRequest{FingerprintSHA256: "fp", KeyType: "t", KeyBlob: []byte("b")},
			wantErr: robot.ErrHostKeyEmptyRobotID,
		},
		{
			name:    "empty fingerprint",
			req:     robot.RecordFirstTouchRequest{RobotID: "ro", KeyType: "t", KeyBlob: []byte("b")},
			wantErr: robot.ErrHostKeyEmptyFingerprint,
		},
		{
			name:    "empty key type",
			req:     robot.RecordFirstTouchRequest{RobotID: "ro", FingerprintSHA256: "fp", KeyBlob: []byte("b")},
			wantErr: robot.ErrHostKeyEmptyKeyType,
		},
		{
			name:    "empty key blob",
			req:     robot.RecordFirstTouchRequest{RobotID: "ro", FingerprintSHA256: "fp", KeyType: "t"},
			wantErr: robot.ErrHostKeyEmptyKeyBlob,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.RecordFirstTouch(ctx, tx, tc.req)
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
				return nil
			}); err != nil {
				t.Fatalf("Tx: %v", err)
			}
		})
	}
}

func TestHostKey_KeyBlobIsCopiedNotShared(t *testing.T) {
	repo, _, store, _ := newHostKeyTestRepo(t)
	tenantID := "ten_a"
	seedTenant(t, store, tenantID)
	seedRobotForHostKey(t, store, tenantID, "ro_1")

	caller := []byte("original-blob")
	req := robot.RecordFirstTouchRequest{
		RobotID:           "ro_1",
		FingerprintSHA256: fingerprint("alpha"),
		KeyType:           "ssh-ed25519",
		KeyBlob:           caller,
	}

	var stored robot.RobotHostKey
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		var err error
		stored, err = repo.RecordFirstTouch(ctx, tx, req)
		return err
	}); err != nil {
		t.Fatalf("first touch: %v", err)
	}

	// 호출자가 원본 슬라이스 mutation해도 저장된 row 영향 없어야 함.
	caller[0] = 'X'
	if string(stored.KeyBlob) == string(caller) {
		t.Errorf("KeyBlob shared with caller — mutation leaked into stored row")
	}
}
