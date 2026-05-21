# Audit Chain Key Rotation — 운영자 가이드

> Phase 10 옵션 D (audit chain signer key rotation 자동화) 운영 docs.
> 대상: rosshield 운영자 (admin · auditor 권한 보유자).
> 설계 배경: [audit-chain-rotation-automation-design.md](../design/notes/audit-chain-rotation-automation-design.md).

---

## 1. 개요

### 1.1 무엇이 자동화되었나

v0.10.0 부터 audit chain signer key 가 quarterly (90 일) cron 으로 자동 rotation 됩니다 (D-P10D-1 = 옵션 C). 운영자 승인 화면 없이 진행되며, 각 rotation 은 단일 `storage.Tx` 안에서 결정론적으로 처리됩니다:

1. 새 Ed25519 key 생성 + keystore 영속 (file 또는 TPM 어댑터).
2. self-sign + verify round-trip (검증 통과 시에만 swap).
3. `audit_chain_keys` 테이블에 새 epoch row append + 이전 epoch revoke.
4. `audit.chain.key_rotated` event emit (audit chain 안에 trace 보존).
5. `SwappableSigner.Swap` — RWMutex queue 패턴으로 in-flight Sign 직렬화 (D-P10D-3 = Queue).
6. Tx commit 성공 시에만 새 signer 활성화 (실패 시 swap 안 함, 이전 key 그대로).

### 1.2 운영자 부담

평시: **0**. quarterly cron 이 자동 진행. 알람도 발생 안 함 (normal rotation은 INFO 로그 + Prometheus counter 증가만).

비상시: `rosshield audit rotation abort --reason "<text>"` 한 줄 (또는 동등 admin endpoint).

### 1.3 외부 감사 호환성

- 모든 rotation 은 `audit.chain.key_rotated` event 로 chain 안에 trace.
- `audit_chain_keys` 는 append-only — epoch 별 public key 영구 보존.
- 외부 검증 도구 `rosshield-audit-verify` v2 는 bundle `chainKeyEpochs` 메타로 epoch 별 public key 를 입력 받아 정확히 검증.
- 운영자 emergency override 도 별도 `audit.chain.rotation_aborted` event 로 trace.

→ SOC2 / ISMS-P / NIST 800-53 SC-12 통제 baseline 만족 (설계 §12.2).

---

## 2. 동작 메커니즘

### 2.1 Signer hot-swap timeline

```
T0  scheduler tick                          (모든 instance — HA leader 만 진행)
T1  leader-only gate 통과 (cronsched + KeyRotator 2 단계 defense-in-depth)
T2  새 ed25519 key 생성 + keystore 영속 (handle = "audit-chain-{epoch}")
T3  self-sign + Verify round-trip (검증 fail 시 abort + 이전 key 유지)
T4  storage.Tx 진입:
     - audit_chain_keys append (epoch=N+1, status=active)
     - audit_chain_keys 의 epoch=N row revoked_at set
     - audit.Append(action="audit.chain.key_rotated", payload=fromEpoch/toEpoch/keyId/pubHex/trigger)
T5  Tx commit 성공
T6  SwappableSigner.Swap(newSigner, N+1) — RWMutex queue, in-flight Sign 완료까지 대기
T7  metrics emit (counter rosshield_audit_rotation_total{status=success} + gauge rosshield_audit_key_epoch{tenant})
T8  로그: "audit chain key rotated" INFO 한 줄.
```

### 2.2 Epoch transition

```
이전 epoch (N) 로 서명된 audit entry      → public_key_epoch_N 로 검증
audit.chain.key_rotated event             → epoch=N 으로 INSERT (swap 직전이라 epoch=N)
이후 entry (N+1 이후)                     → public_key_epoch_(N+1) 로 검증
```

> 본 timeline 은 fg-verify v2 가 bundle `chainKeyEpochs` 메타로 자동 처리합니다 (Stage 10.D-5).

---

## 3. 모니터링

### 3.1 Prometheus metrics

| metric | type | label | 의미 |
|---|---|---|---|
| `rosshield_audit_rotation_total` | counter | `status=success\|failed\|skipped` | rotation 호출 누적 결과 |
| `rosshield_audit_key_epoch` | gauge | `tenant_id` | tenant 별 현재 활성 epoch (audit_chain_keys 활성 row epoch) |

### 3.2 Grafana panel 예시

```promql
# 최근 rotation 결과
sum by (status) (increase(rosshield_audit_rotation_total[7d]))

# tenant 별 현재 epoch
rosshield_audit_key_epoch
```

### 3.3 check-health hook

snap 배포 환경에서는 `rosshield-server` daemon healthz polling 이 audit chain head 와 epoch 가 정상 진척 중인지 함께 확인합니다 (`/healthz` 응답에 `audit.epoch` 포함). 자세한 절차는 [snap-deployment.md](snap-deployment.md) §7 참조.

---

## 4. 정상 rotation 절차

운영자 개입 0. quarterly cron 이 자동 진행. 첫 부팅 후 `lastRotated` 미보유 상태에서 cron tick 도달하면 즉시 첫 rotation. 이후 90 일 간격.

### 4.1 첫 부팅 후 첫 rotation 확인

```bash
# 활성 epoch 확인 (Prometheus 미배포 환경)
curl -s -H "Authorization: Bearer $TOKEN" \
  https://lodestar.example.com/api/v1/audit/head \
  | jq '.epoch'
# expected: 1 (부팅 직후 bootstrap row).

# cron tick 후 (90일 후 또는 ROSSHIELD_AUDIT_CHAIN_KEY_ROTATION_SCHEDULE="@every 1h" 테스트)
# expected: 2.

# Prometheus counter 확인.
curl -s http://lodestar.example.com:9100/metrics \
  | grep rosshield_audit_rotation_total
# expected:
# rosshield_audit_rotation_total{status="success"} 1
```

### 4.2 audit chain trace 확인

```bash
# audit.chain.key_rotated event 가 chain 안에 보존되는지 확인.
rosshield-audit-verify --bundle ./audit-bundle.tar.gz --verbose
# expected output:
# epoch transitions:
#   1 → 2 at seq=42 (audit.chain.key_rotated)
# chain OK (all signatures verified against per-epoch public keys)
```

---

## 5. Emergency override 절차

### 5.1 사용 시나리오

다음 케이스 중 하나라도 의심되면 즉시 abort:

- **잘못된 rotation 시점** — cron schedule misconfig 등으로 의도하지 않은 rotation tick.
- **keystore 손상** — TPM PCR seal 실패 또는 file 권한 변경 가능성.
- **broken rotation** — 새 key 의 self-verify 가 통과했지만 외부 verify 도구가 검증 실패 (이론상 cosmic ray 등 hw 결함).

### 5.2 CLI 절차

```bash
# 1. abort 호출 — admin token 필요.
rosshield audit rotation abort \
  --reason "TPM PCR seal 의심 — incident #2026-05-21-001" \
  -o json

# expected output:
# {
#   "aborted": true,
#   "auditEntryId": 12345,
#   "abortedAt": "2026-05-21T12:34:56.789Z",
#   "previousEpoch": 3,
#   "reason": "TPM PCR seal 의심 — incident #2026-05-21-001"
# }

# 2. audit chain 안에서 trace 확인.
rosshield-audit-verify --bundle ./latest-audit.tar.gz --filter audit.chain.rotation_aborted
# expected: entry seq=12345 + payload.reason + actor.
```

### 5.3 admin endpoint 직접 호출 (자동화)

```bash
curl -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"reason":"automated abort by PagerDuty incident #2026-05-21-001"}' \
  https://lodestar.example.com/api/v1/audit/rotation/abort
```

권한: `tenant_admin.admin` (admin 또는 동등 role).

### 5.4 동작 보장

- **Idempotent**: 진행 중 rotation 이 없어도 audit emit + abort flag set. 다음 자동 rotation 1 회를 건너뜀 (정상 보수적 동작).
- **Leader-only**: follower 인스턴스에서 호출 시 503 `instance is not leader` + audit emit 안 함.
- **Trace 보존**: 항상 `audit.chain.rotation_aborted` event 가 chain 안에 INSERT (audit emit Tx 실패 시 abort flag 만 set + 운영자에게 500 error).

---

## 6. Broken rotation 대응 절차

### 6.1 증상

- `rosshield_audit_rotation_total{status="failed"}` counter 증가.
- 로그에 `audit chain key rotation failed` ERROR 라인 (cause 포함).
- 또는 `rosshield-audit-verify` v2 가 epoch 전환 직후 entry signature 검증 실패.

### 6.2 절차

```bash
# 1. 즉시 abort — 추가 rotation 차단 + audit trace.
rosshield audit rotation abort --reason "broken rotation detected at $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# 2. 현 활성 epoch 확인.
curl -s -H "Authorization: Bearer $TOKEN" \
  https://lodestar.example.com/api/v1/audit/head | jq

# 3. audit_chain_keys 직접 조회 (PG/sqlite DBA query).
psql -U rosshield -c "SELECT epoch, key_id, public_key_hex, created_at, revoked_at, created_by FROM audit_chain_keys WHERE tenant_id = 'system' ORDER BY epoch DESC LIMIT 5;"

# 4. fg-verify v2 로 chain 검증.
rosshield-audit-verify --bundle ./audit.tar.gz --verbose

# 5. 결과에 따라:
#    - chain OK 였지만 일부 entry 검증 실패 → audit_chain_keys 의 revoked epoch public key 가
#      bundle 에 누락. fg-verify --extra-pubkey <hex> 옵션으로 추가 검증 (별 epic — 본 round
#      미구현. 우선 audit_chain_keys 직접 export 후 manual 검증).
#    - chain BROKEN at seq=N → audit trace 가 손상. Tx rollback 정책상 partial 손상 불가능,
#      직접 DB 손상 가능성. incident 보고 + DB backup 복원.
```

### 6.3 재시도

abort 후 안전이 확인되면 manual rotation 재시도 가능 (별 endpoint — 본 round 미구현). 그동안에는 자동 cron 이 다음 quarterly cycle 에서 재시도. 즉시 강제 rotation 이 필요하면 `ROSSHIELD_AUDIT_CHAIN_KEY_ROTATION_SCHEDULE="@every 1h"` 일시 변경 후 restart.

### 6.4 audit chain 검증

```bash
# fg-verify v2 — 모든 epoch 공개키 자동 신뢰.
rosshield-audit-verify --bundle ./latest-audit.tar.gz
# expected: "chain OK (X entries, Y epochs verified)"
```

---

## 7. 외부 감사인 호환성

### 7.1 fg-verify v2 bundle 구조

```json
{
  "head": { "seq": 4242, "hash": "0xabcd..." },
  "chainKeyEpochs": [
    { "epoch": 1, "publicKeyHex": "f074...", "createdAt": "2026-01-01T00:00:00Z", "revokedAt": "2026-04-01T00:00:00Z" },
    { "epoch": 2, "publicKeyHex": "48959...", "createdAt": "2026-04-01T00:00:00Z", "revokedAt": null }
  ],
  "entries": [ ... ]
}
```

### 7.2 신뢰 절차

외부 감사인이 다음 4 점을 확인:

1. **bundle 서명** — 본 bundle 자체가 release signer 로 서명되어 있는지 (cosign sign-blob).
2. **chainKeyEpochs 일관성** — `audit.chain.key_rotated` event 의 `publicKeyHex` 와 일치하는지 (chain-internal cross-check).
3. **chainKeyEpochs append-only** — bundle 간 diff 에서 epoch 가 단조 증가 + 기존 epoch 의 publicKeyHex 가 변경 없는지.
4. **entry 별 signature** — `entry.epoch` 에 해당하는 `chainKeyEpochs[entry.epoch].publicKeyHex` 로 verify.

→ rosshield 의 결정론적 chain 보존 정책 (P9 불변성 원칙) 상 위 4 점이 모두 만족되어야 chain 이 무손상.

---

## 8. 채널 정책

### 8.1 환경 변수

| env | default | 의미 |
|---|---|---|
| `ROSSHIELD_AUDIT_CHAIN_KEY_ROTATION_SCHEDULE` | `""` (자동 비활성) | robfig/cron spec. 권장 `@every 2160h` (90 일) |
| `ROSSHIELD_AUDIT_CHAIN_KEY_ROTATION_MIN_INTERVAL` | `1h` | RotateNow idempotency guard. 짧은 재호출 차단 |

### 8.2 권장 customer 별 schedule

| customer | schedule | 근거 |
|---|---|---|
| SOC2 / ISMS-P | `@every 2160h` (quarterly) | 표준 baseline |
| 데스크톱·on-prem 단일 인스턴스 | `""` (manual only) | 자동 rotation 부담 회피 |
| 어플라이언스 / HA cluster | `@every 2160h` | leader 단일 인스턴스만 수행 |

### 8.3 cron spec 변경 절차

```bash
# 1. 환경 변수 변경 후 restart.
sudo snap set rosshield audit-chain-key-rotation-schedule="@every 2160h"
sudo snap restart rosshield

# 2. 변경 적용 확인 (로그).
journalctl -u snap.rosshield.daemon | grep "audit chain key rotation auto-schedule registered"
# expected: spec="@every 2160h" jobId="system.audit.keyrotation.auto"
```

---

## 9. 한계 + carryover

### 9.1 본 round 한계

- **audit hash chain input 미포함**: `key_epoch` + `leader_epoch` 는 `audit_entries` 컬럼으로만 보존. hash chain 자체 input 에는 포함되지 않음 (외부 검증 도구 호환성 유지를 위해 v0.x 와 동일한 hash 계산 보존). 향후 SC-12 강화 필요 시 별 epic.
- **bootstrap epoch=1 placeholder**: 0037 마이그레이션이 bootstrap epoch=1 row 를 INSERT 할 때 keystore handle 은 빈 문자열 / public key 는 placeholder. 첫 부팅 시 실제 signer 정보로 자동 갱신 (KeyRotator 가 read-current → 실 signer 정보로 재구성).
- **manual rotation endpoint 미구현**: emergency abort 만 노출. 즉시 rotation 트리거가 필요하면 quarterly schedule 을 일시 단축 후 restart.
- **multi-tenant epoch 분리 미구현**: 본 round 는 system tenant 단일 인스턴스 가정. customer 도메인 tenant 별 epoch 는 별 round.

### 9.2 Phase 10 잔여

- 옵션 E — ros2-humble pack (Ubuntu 22.04 baseline 확장 + DDS/SROS2). 별 epic.

---

## 10. 참조

- [audit-chain-rotation-automation-design.md](../design/notes/audit-chain-rotation-automation-design.md) §6.1~§6.7 + §12.1 — 옵션 C 채택 근거 + Stage 분해.
- [docs/releases/v0.10.0.md](../releases/v0.10.0.md) — 본 기능 release notes.
- [audit-verify-cli.md](audit-verify-cli.md) — fg-verify v2 사용법.
- [snap-deployment.md](snap-deployment.md) §7 — snap 배포 환경에서 daemon healthz polling.
