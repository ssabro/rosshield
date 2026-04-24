package audit

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// SerializeCheckpointPayload는 Ed25519 서명 input을 결정적으로 구성합니다 (§10.5).
//
// 형식: hash[32] ‖ uint64BigEndian(seq) ‖ utf8(tenantId)
//
// 외부 검증 도구는 동일한 함수로 payload를 재구성하여 signer.Verify를 호출합니다.
// 따라서 이 함수의 출력 형식은 외부 호환 약속이며, 변경 시 외부 도구도 함께 갱신해야 합니다.
func SerializeCheckpointPayload(tenantID storage.TenantID, seq int64, hash Hash) []byte {
	tid := []byte(tenantID)
	out := make([]byte, 0, HashSize+8+len(tid))
	out = append(out, hash[:]...)
	var seqBuf [8]byte
	binary.BigEndian.PutUint64(seqBuf[:], uint64(seq))
	out = append(out, seqBuf[:]...)
	out = append(out, tid...)
	return out
}

// RegisterCheckpointJob은 Scheduler에 정기 checkpoint job을 등록합니다.
//
// job 본문: tenant scope Tx 안에서 svc.WriteCheckpoint 호출.
// no-op 종류는 로그만 남기고 에러로 전파하지 않음:
//   - ErrNoEntries: 빈 체인 (정시 발화지만 새 entry 없음)
//   - ErrCheckpointExists: 이미 같은 head로 checkpoint 작성됨 (정시 두 번 fire 방지)
//
// 그 외 에러는 logger.Error 후 nil 반환 — Scheduler 어댑터가 에러를 받으면 다음 발화는
// 진행하지만 worker recovery 패턴과 정합 (cronsched는 error를 단순 warn 로그).
func RegisterCheckpointJob(
	sch scheduler.Scheduler,
	store storage.Storage,
	svc Service,
	logger *slog.Logger,
	jobID, spec string,
	tenantID storage.TenantID,
	sgn signer.Signer,
) error {
	if sch == nil || store == nil || svc == nil || logger == nil || sgn == nil {
		return fmt.Errorf("audit: RegisterCheckpointJob: all dependencies required")
	}
	if tenantID == "" {
		return fmt.Errorf("audit: RegisterCheckpointJob: tenantID required")
	}

	job := func(ctx context.Context) error {
		ctx = storage.WithTenantID(ctx, tenantID)
		return store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			cp, err := svc.WriteCheckpoint(ctx, tx, tenantID, sgn)
			switch {
			case errors.Is(err, ErrNoEntries):
				logger.Debug("audit: checkpoint skipped (empty chain)", "tenant", string(tenantID))
				return nil
			case errors.Is(err, ErrCheckpointExists):
				logger.Debug("audit: checkpoint skipped (head unchanged)", "tenant", string(tenantID))
				return nil
			case err != nil:
				logger.Error("audit: checkpoint failed", "tenant", string(tenantID), "err", err.Error())
				return err
			default:
				logger.Info("audit: checkpoint written",
					"tenant", string(tenantID), "seq", cp.Seq, "keyId", cp.SignerKeyID)
				return nil
			}
		})
	}
	return sch.Schedule(jobID, spec, job)
}
