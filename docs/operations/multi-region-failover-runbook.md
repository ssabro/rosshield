# Multi-region Failover Runbook

> **대상**: Lodestar 운영자 / SRE (on-call).
> **목적**: Primary region 장애 발생 시 step-by-step 대응 절차. RTO ≤ 5분 달성.
> **선행**: [`multi-region-dns.md`](multi-region-dns.md) Route53 setup 완료, [`multi-region-ha-setup.md`](multi-region-ha-setup.md) PG replication 활성.
> **결정**: D-MR-3 = manual application promote (자동 cutover는 DNS layer만). split-brain 회피.

---

## 0. 핵심 원칙

1. **DNS layer는 자동, application layer는 manual** — Route53 health check가 90초 안에 자동 cutover. PG promote와 application restart는 운영자 판단 (split-brain 회피).
2. **Time-boxed escalation** — 2분 안에 promote 결정 못하면 secondary on-call 호출.
3. **모든 단계 timestamp 기록** — 사후 분석 + RTO 측정 + audit trail.
4. **Communication first** — Step 1은 always "내부 통지" — 운영자 다수가 같은 절차를 동시 실행하는 race condition 방지.

---

## 1. 사전 준비 (incident 발생 전)

### 1.1 운영자 환경 체크리스트

- [ ] Standby region SSH 키 + bastion 접속 검증 (월 1회 drill)
- [ ] `psql` + `aws cli` + `kubectl` (or `systemctl`) PATH 확인
- [ ] PagerDuty/Opsgenie 알림 수신 + escalation policy 검증
- [ ] Slack/Teams `#incident-response` 채널 접근 권한
- [ ] Route53 console + Terraform repo 접근 권한

### 1.2 사전 정보 수집

운영자가 미리 알아둬야 할 값 (`secrets manager` 또는 1Password vault에 저장):

| 항목 | 예시 값 | 출처 |
|---|---|---|
| Primary region | `ap-northeast-2` (Seoul) | infrastructure docs |
| Secondary region | `ap-northeast-1` (Tokyo) | infrastructure docs |
| Primary PG endpoint | `audit-pg-seoul.acme.internal:5432` | Terraform output |
| Secondary PG endpoint | `audit-pg-tokyo.acme.internal:5432` | Terraform output |
| Secondary rosshield-server hosts | `audit-tokyo-{1,2,3}.acme.internal` | ec2 inventory |
| Route53 Hosted Zone ID | `Z0123456789ABCDEFGHIJ` | Terraform output |
| Primary health check ID | `abc123def456` | Terraform output `primary_health_check_id` |
| customer notification list | `customers@acme.com`, status page | CRM |

### 1.3 정기 drill (월 1회)

production에서는 위험하니 **staging environment**에서 월 1회 drill:
- staging Primary 강제 종료 → Route53 cutover 발생 확인
- staging Secondary promote → application restart → 정상 동작 확인
- drill 결과를 audit trail에 기록 (`audit-drill-{YYYY-MM}.md`)

---

## 2. Incident 감지 (T+0:00 ~ T+2:00)

### 2.1 알림 수신

PagerDuty 알림 예시:
```
[CRITICAL] rosshield-primary-down
Service: rosshield-server
Region: ap-northeast-2
Alert: HC-primary 3회 연속 fail
Time: 2026-05-20T07:32:00Z
Health check ID: abc123def456
```

### 2.2 즉시 확인 (1분 안)

**1단계 — 통지 (Slack)**:
```
@here Primary region (Seoul) down — runbook 진입.
Acked by: <your-name>
Time: 2026-05-20T07:33:00Z
```

**2단계 — DNS cutover 자동 진행 확인**:
```bash
dig audit.acme.com +short
# Expected (자동 cutover 진행 중): audit-tokyo.acme.com 또는 Tokyo ALB IP

# Route53 health check 상태
aws route53 get-health-check-status --health-check-id abc123def456
# Expected: { "HealthCheckObservations": [{"StatusReport": {"Status": "Failure: ..."}}] }
```

**3단계 — false positive 확인**:
```bash
# Primary ALB 직접 호출
curl -m 5 https://audit-seoul.acme.com/healthz
# 200 OK면 false positive — STOP, escalate

# 다른 region에서 Primary 호출 (network partition 확인)
ssh ec2-user@audit-tokyo.acme.com "curl -m 5 https://audit-seoul.acme.com/healthz"
# 양쪽에서 timeout이면 진짜 region 장애
```

### 2.3 분기

| 상황 | 다음 단계 |
|---|---|
| Primary 진짜 down (양쪽 region에서 timeout) | §3 promote 진행 |
| Primary 정상이지만 health check만 fail (network partition) | §6 escalation (split-brain 위험, runbook 중단) |
| Primary 정상 + health check 정상 (false alarm) | §6 escalation (Route53 false positive 의심) |

---

## 3. Application promote (T+2:00 ~ T+4:00)

### 3.1 Standby region SSH

```bash
ssh ec2-user@audit-tokyo.acme.com
# 또는 SSM session manager
aws ssm start-session --target i-tokyo-1 --region ap-northeast-1
```

### 3.2 PG promote

```bash
sudo -u postgres psql -c "SELECT pg_promote();"
# Expected: pg_promote returns 't' (success)

# 검증
sudo -u postgres psql -c "SELECT pg_is_in_recovery();"
# Expected: f (false = primary mode)
```

**주의**: `pg_promote()` 호출은 **immediate**. 한 번 promote하면 다시 standby로 되돌리려면 base backup부터 다시 만들어야. 신중 결정.

### 3.3 rosshield-server 재시작

systemd 배포:
```bash
# 환경 변수 변경 — standby → primary role
sudo systemctl edit rosshield-server.service
# 아래 추가:
# [Service]
# Environment="ROSSHIELD_REPLICATION_ROLE=primary"

sudo systemctl daemon-reload
sudo systemctl restart rosshield-server

# 검증
curl https://localhost:8080/healthz | jq '.ha'
# Expected: { "enabled": true, "role": "leader", "epoch": <N+1> }
```

Kubernetes/ECS 배포:
```bash
# k8s
kubectl set env deployment/rosshield-server ROSSHIELD_REPLICATION_ROLE=primary -n rosshield
kubectl rollout restart deployment/rosshield-server -n rosshield
kubectl rollout status deployment/rosshield-server -n rosshield

# ECS
aws ecs update-service \
  --cluster rosshield-tokyo \
  --service rosshield-server \
  --force-new-deployment
```

### 3.4 leader-election 확인

```bash
# 모든 instance가 leader-election에 참여하는지
for host in audit-tokyo-{1,2,3}.acme.internal; do
  ssh ec2-user@$host "curl -s https://localhost:8080/healthz | jq '.ha'"
done
# Expected: 정확히 1개 instance가 role="leader", 나머지는 role="follower"
# epoch는 모두 동일한 새 값
```

### 3.5 ALB target health

```bash
aws elbv2 describe-target-health \
  --target-group-arn $(terraform output -raw tokyo_target_group_arn) \
  --region ap-northeast-1
# Expected: leader instance가 healthy, follower는 unhealthy (503 응답 — 정상)
```

---

## 4. 검증 (T+4:00)

### 4.1 Client-facing 검증

```bash
# DNS resolve
dig audit.acme.com +short
# Expected: audit-tokyo.acme.com

# write API 동작
curl -X POST https://audit.acme.com/api/v1/audit/checkpoint \
  -H "Authorization: Bearer $ADMIN_TOKEN"
# Expected: 200 OK + 새 checkpoint hash

# read API
curl https://audit.acme.com/api/v1/audit/head \
  -H "Authorization: Bearer $TOKEN"
# Expected: 정상 응답, 최근 entry seq
```

### 4.2 Monitoring 검증

- Grafana dashboard `rosshield-ops` 확인:
  - `rosshield_ha_role` Tokyo region = 1, Seoul = 0
  - `rosshield_ha_leader_epoch` 증가 확인
  - `rosshield_ha_failover_total` +1
  - `rosshield_request_total` Tokyo region 증가
- Alertmanager에서 `PrimaryRegionDown` alert 활성 상태 확인 (자연 종료까지 유지)

---

## 5. Customer 통지 (T+5:00)

### 5.1 status page 업데이트

```
[INCIDENT — Investigating → Identified → Monitoring]

Title: Primary region failover
Time: 2026-05-20 07:32 UTC
Impact: ~3 minutes write API unavailability
Status: Resolved — traffic on Tokyo region

Cause: Seoul region infrastructure incident
Mitigation: Automatic DNS failover + manual application promote completed
Customer action: No action required — DNS resolve will automatically point to new endpoint
```

### 5.2 enterprise customer 직접 통지

SLA 99.99% 계약 customer는 직접 이메일/전화:
- 영향 시간 (T+0:00 ~ T+4:00 = ~4분)
- 손실 데이터 — RPO 1분 안 (replication lag 시점에 미반영 write 가능성)
- 후속 조치 — 손실 추적 가능한 client는 retry 권장

---

## 6. Escalation

다음 상황에서 즉시 secondary on-call 호출:

| 트리거 | 조치 |
|---|---|
| 진단 시 Primary 정상이지만 health check fail | network partition 의심 — split-brain 위험. promote 보류 + secondary on-call 호출 |
| `pg_promote()` 실패 (return = false) | DB engineer 호출 — PG state corruption 의심 |
| `rosshield-server` restart 후 leader-election 실패 | application engineer 호출 — fence token conflict 의심 |
| 5분 안에 promote 완료 못함 | secondary on-call 호출 + customer communications team 호출 |

---

## 7. Roll-back (necessary)

promote가 **잘못 진행**된 경우 (Primary가 살아있었는데 false positive로 promote):

### 7.1 즉시 secondary 격리

```bash
# Tokyo의 application을 standby 모드로 다시 변경 + 정지
sudo systemctl stop rosshield-server

# 또는 standby-mode middleware 활성화 (write 차단)
sudo systemctl edit rosshield-server.service
# Environment="ROSSHIELD_REPLICATION_ROLE=standby"
sudo systemctl daemon-reload
sudo systemctl start rosshield-server
```

### 7.2 DNS 강제 cutback

Route53 record를 수동으로 Seoul로 변경:

```bash
aws route53 change-resource-record-sets \
  --hosted-zone-id Z0123456789ABCDEFGHIJ \
  --change-batch '{
    "Changes": [{
      "Action": "UPSERT",
      "ResourceRecordSet": {
        "Name": "audit.acme.com",
        "Type": "CNAME",
        "SetIdentifier": "primary",
        "Failover": "PRIMARY",
        "TTL": 60,
        "ResourceRecords": [{"Value": "audit-seoul.acme.com"}],
        "HealthCheckId": "abc123def456"
      }
    }]
  }'
```

### 7.3 Tokyo PG는 어떻게?

`pg_promote()`는 immediate라 cleanup 복잡:
1. Tokyo PG는 이제 "stale primary" — write 받았으면 안 됨 (위 §7.1로 차단)
2. Seoul PG는 여전히 진짜 primary
3. Tokyo PG를 standby로 다시 만들려면:
   - Tokyo PG stop
   - Seoul base backup → Tokyo PG data directory restore
   - subscription 재생성
   - logical replication 재시작

**시간 큰 작업** — DB engineer + 추가 시간 (~ 수시간) 필요.

### 7.4 lesson learned

roll-back은 항상 큰 비용. **§2.3에서 false positive 의심 시 §6 escalate가 항상 우선** — promote 결정 보수적.

---

## 8. Primary 복구 후 절차

Seoul region이 복구되면:

### 8.1 옵션 A — Tokyo를 새 primary로 유지 (권장)

운영 단순. 다음 region 장애 시 Seoul로 cutover.

```bash
# Seoul PG를 standby로 만들기 (Seoul이 fresh start)
sudo systemctl stop rosshield-server
sudo systemctl stop postgresql

# Seoul PG base backup from Tokyo
sudo -u postgres pg_basebackup \
  -h audit-pg-tokyo.acme.internal -U replication \
  -D /var/lib/postgresql/16/main -P -X stream

# Seoul PG를 standby로 부팅 + subscription 생성
# (multi-region-ha-setup.md 참조)

# rosshield-server를 standby로 부팅
sudo systemctl edit rosshield-server.service
# Environment="ROSSHIELD_REPLICATION_ROLE=standby"
sudo systemctl start rosshield-server

# Route53 Primary record는 Tokyo로 swap (Stage 4 ops doc §3.2 절차)
```

### 8.2 옵션 B — Seoul로 복귀

다음 incident 시 같은 cutover 절차 반복.

권장 옵션 A. 단 customer가 Seoul region preferred (latency 등) 명시 시 옵션 B.

---

## 9. 사후 분석 (incident 종료 후 1주일 안)

### 9.1 RTO 측정

| 단계 | 측정 시간 | 목표 |
|---|---|---|
| 알림 수신 → §2 진단 완료 | (실측) | ≤ 2분 |
| §3 promote 시작 → §3.4 leader 확인 | (실측) | ≤ 2분 |
| §3.5 ALB target healthy | (실측) | ≤ 30초 |
| §4 client 검증 완료 | (실측) | ≤ 30초 |
| **총 RTO** | (실측) | **≤ 5분** |

### 9.2 RPO 측정

```bash
# Tokyo PG의 첫 entry 시점
sudo -u postgres psql -c "SELECT MIN(occurred_at) FROM audit_entries WHERE id > (SELECT MAX(id) - 1000 FROM audit_entries);"

# Seoul PG의 마지막 entry 시점 (복구 후 비교)
# 차이가 RPO 손실
```

### 9.3 Postmortem template

`postmortems/YYYY-MM-DD-multi-region-failover.md`:
- 발생 시각·detection 시간·resolution 시간
- root cause (Primary region 장애 사유)
- RTO·RPO 실측
- 운영자 행동 timeline
- 잘된 점·개선점
- action items (runbook 갱신, drill 추가, monitoring 강화 등)

### 9.4 runbook 갱신

본 runbook 자체를 PR로 갱신:
- 발견된 missing step 추가
- 시간 측정 결과로 목표 조정
- 운영자 confusing 부분 명확화

---

## 10. Quick reference (printable)

```
┌─ INCIDENT: Primary region down ──────────────────────┐
│                                                       │
│  T+0:00  PagerDuty 알림 수신                          │
│  T+0:30  Slack #incident-response 통지                │
│  T+1:00  §2.2 진단 (DNS·health check·false positive)  │
│  T+2:00  §3 promote 결정                              │
│          ↓ (DB engineer escalate 안 함)               │
│  T+2:30  §3.2 pg_promote()                            │
│  T+3:00  §3.3 rosshield-server restart                │
│  T+3:30  §3.4 leader-election 확인                    │
│  T+4:00  §4 client 검증                               │
│  T+5:00  §5 customer 통지                             │
│                                                       │
│  Total RTO: ≤ 5분                                     │
│                                                       │
│  Escalate if:                                         │
│  - false positive 의심 → §6                            │
│  - pg_promote fail → DB engineer                      │
│  - 5분 안에 promote 못함 → secondary on-call          │
│                                                       │
└──────────────────────────────────────────────────────┘
```

---

## 11. Patroni 자동 cutover 시나리오 (Phase 9 — `--ha-rp=patroni`)

`--ha-rp=patroni` 환경(v0.8.0+ Kubernetes customer)은 application promote가 **자동** —
운영자는 검증 + 사후 분석만 담당. 본 절차는 §3 manual promote를 대체합니다.

### 11.1 자동 단계 (Patroni 표준)

```
T+0:00  PG primary pod 죽음 (kubelet 또는 PG crash)
T+0:05  Patroni leader lease 만료 감지 (etcd TTL 30s, lease 갱신 실패)
T+0:10  Patroni standby × 2가 promote 시도 → etcd quorum 확인 → 1개 promote
T+0:15  새 PG primary가 wal stream 수용 시작
T+0:20  rosshield-server pod 3개가 Patroni REST polling → 새 leader 식별
        (PollInterval 1s default라 직전 leader poll 직후 ≤ 1s)
T+0:21  application-level RoleProvider epoch 증가 → write API 수용 시작
T+0:21  Kubernetes Service가 새 leader pod로 routing (readiness probe 200)
```

**RTO**: ~30초 (etcd quorum + Patroni lease + rosshield poll 합).

### 11.2 운영자 검증 절차 (T+0:30 ~ T+2:00)

#### Step 1 — PagerDuty 알림 + Slack 통지

```
@here Patroni 자동 cutover 발생 — runbook §11 검증 진입.
Acked by: <your-name>
Time: 2026-05-20T12:34:00Z
```

#### Step 2 — Patroni 자체 검증

```bash
# Patroni cluster 상태
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- patronictl list
# Expected: 정확히 1개 Leader + 2개 Replica
#   Member                                  | Cluster                                 | Host           | Role    | State     | TL | Lag in MB
# ----------------------------------------------------------------------------------------------------------------------
#   rosshield-patroni-postgresql-ha-postgresql-0  | rosshield-patroni-postgresql-ha-postgresql  | 10.0.0.10      | Leader  | running   |  3 |
#   rosshield-patroni-postgresql-ha-postgresql-1  | rosshield-patroni-postgresql-ha-postgresql  | 10.0.0.11      | Replica | streaming |  3 |   0
#   rosshield-patroni-postgresql-ha-postgresql-2  | rosshield-patroni-postgresql-ha-postgresql  | 10.0.0.12      | Replica | streaming |  3 |   0

# TL(timeline)이 증가했는지 (이전 incident 대비 +1)
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- \
  patronictl history
# Expected: 최근 timeline switch 1건
```

#### Step 3 — rosshield-server leader 추종 확인

```bash
# 모든 pod의 RoleProvider 상태
for pod in $(kubectl get pods -n rosshield-ha -l app=rosshield-server -o name); do
  kubectl exec -n rosshield-ha $pod -- curl -s localhost:8080/healthz | jq '.ha'
done
# Expected: 1 pod role=leader (Patroni leader 일치), 나머지 follower
# 모든 pod의 epoch가 동일한 새 timeline 값
```

#### Step 4 — write API 동작

```bash
curl -X POST https://audit.acme.com/api/v1/audit/checkpoint \
  -H "Authorization: Bearer $ADMIN_TOKEN"
# Expected: 200 OK
```

#### Step 5 — Customer 통지

§5 customer 통지 절차 그대로. 단 자동 cutover라 영향 시간이 짧음 (~30초):

```
[INCIDENT — Resolved]
Title: PG primary auto-failover (Patroni)
Time: 2026-05-20 12:34 UTC
Impact: ~30 seconds write API unavailability
Status: Auto-resolved by Patroni — Lodestar RoleProvider 자동 추종
Customer action: No action required
```

### 11.3 false positive 의심 시 (Patroni pause)

Patroni가 false positive로 자동 cutover를 시도하는 패턴 (분기 0~3회):

```bash
# 자동 failover 즉시 정지
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- patronictl pause

# 원인 분석
kubectl logs -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 --tail=100
# network partition / etcd flapping / PG load 등 식별

# 안정 확인 후 재활성
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- patronictl resume
```

### 11.4 Manual promote (자동 거부 시)

Patroni가 자동 cutover에 실패 (network partition · etcd quorum 부족 등):

```bash
# 강제 promote — etcd quorum 우회 (위험: split-brain risk)
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-1 -- \
  patronictl failover --candidate rosshield-patroni-postgresql-ha-postgresql-1 --force

# 또는 등록된 후보 list 확인
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- \
  patronictl list
```

### 11.5 Patroni 환경에서 E25 fallback

Patroni 또는 etcd 자체 장애 시 임시로 E25 fallback (D-AF-4):

```bash
# 모든 rosshield-server pod 환경 변수 변경
kubectl set env deployment/rosshield-server -n rosshield-ha \
  ROSSHIELD_HA_RP=e25

# Rolling restart
kubectl rollout restart deployment/rosshield-server -n rosshield-ha

# 검증
kubectl exec -n rosshield-ha deploy/rosshield-server -- \
  curl -s localhost:8080/healthz | jq '.ha'
# role이 e25 PG advisory lock 기반으로 결정됨
```

복구 후 다시 `ROSSHIELD_HA_RP=patroni`로 환원.

### 11.6 RTO 비교 (E25 manual vs Patroni 자동)

| 단계 | E25 manual (§3) | Patroni 자동 (§11) |
|---|---|---|
| 감지 | T+0:00 ~ T+2:00 | T+0:00 ~ T+0:10 |
| PG promote | T+2:30 (수동) | T+0:10 (자동) |
| Application restart | T+3:00 (수동) | T+0:20 (자동, polling) |
| Client 검증 | T+4:00 (수동) | T+0:21 (자동) |
| **총 RTO** | **≤ 5분** | **≤ 30초** |

Patroni 환경은 운영자가 사후 검증만 — RTO 자체는 application 측 무관.

---

## 12. 참조

- design [`multi-region-ha-stage4-design.md`](../design/notes/multi-region-ha-stage4-design.md) — Stage 4 DNS routing 결정
- design [`auto-failover-research.md`](../design/notes/auto-failover-research.md) — Phase 9 Patroni 통합 결정 (D-AF-1~4)
- ops [`multi-region-dns.md`](multi-region-dns.md) — Route53 setup + Cloudflare + 자체 DNS
- ops [`multi-region-ha-setup.md`](multi-region-ha-setup.md) — PG logical replication 전제 조건
- ops [`patroni-deployment.md`](patroni-deployment.md) — Phase 9 Kubernetes Patroni 운영 가이드
- HA [`ha-deployment.md`](ha-deployment.md) — E25 single-region leader-election
- audit chain [`audit-rotation-verify.md`](audit-rotation-verify.md) — failover 시 segment 무결성 검증
- Kubernetes manifest [`deploy/k8s/patroni/`](../../deploy/k8s/patroni/) — values + Deployment sample
