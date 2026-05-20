# Multi-region HA — DNS Routing 운영 가이드

> **대상**: Lodestar 운영자 (cross-region cutover 담당).
> **선행**: `multi-region-ha-setup.md` (PG logical replication + standby instance 부팅).
> **목표**: region 장애 시 client traffic을 standby region으로 자동 cutover (RTO ≤ 5분).
> **결정**: D-MR-4 = Route53 default + Cloudflare/자체 DNS alternative.
>
> design 근거: [`docs/design/notes/multi-region-ha-stage4-design.md`](../design/notes/multi-region-ha-stage4-design.md).

---

## 1. 사전 준비

- AWS 계정 + Route53 Hosted Zone (예: `acme.com`)
- Primary region (예: ap-northeast-2 Seoul) + Standby region (예: ap-northeast-1 Tokyo)
- 양 region에 rosshield-server 배포 + PG logical replication 활성 (`multi-region-ha-setup.md` 완료)
- 각 region ALB 엔드포인트 (예: `audit-seoul.acme.com`, `audit-tokyo.acme.com`)

---

## 2. Route53 setup (권장 default)

### 2.1 Health check 생성

AWS Console → Route53 → Health checks → Create:

| 필드 | Primary | Secondary |
|---|---|---|
| Name | `rosshield-primary-health` | `rosshield-secondary-health` |
| Endpoint | `audit-seoul.acme.com` | `audit-tokyo.acme.com` |
| Protocol | HTTPS | HTTPS |
| Path | `/healthz` | `/healthz` |
| Port | 443 | 443 |
| Request interval | 30s | 30s |
| Failure threshold | 3 | 3 |
| Measure latency | ☑ | ☑ |

**핵심**: `failure_threshold=3 + interval=30s` → 90초 안에 3회 연속 fail 시 unhealthy 판정. false positive 회피.

### 2.2 Failover record 생성

Route53 → Hosted Zones → acme.com → Create record:

#### Primary record

```
Record name:        audit.acme.com
Routing policy:     Failover
Failover type:      Primary
Record ID:          primary
Health check ID:    rosshield-primary-health (위에서 생성)
Value:              audit-seoul.acme.com (ALIAS to ALB primary)
TTL:                60
```

#### Secondary record

```
Record name:        audit.acme.com
Routing policy:     Failover
Failover type:      Secondary
Record ID:          secondary
Health check ID:    rosshield-secondary-health
Value:              audit-tokyo.acme.com (ALIAS to ALB secondary)
TTL:                60
```

**TTL 60초 강제** — RTO ≤ 60초 보장. customer base 10K endpoint 가정 시 월 ~$170 query 비용 추가 (수용 가능).

### 2.3 ALB target health (region 내부)

각 region ALB target group에 인스턴스 등록 + health check:

```hcl
resource "aws_lb_target_group" "rosshield" {
  name     = "rosshield-tg"
  port     = 8080
  protocol = "HTTPS"
  vpc_id   = aws_vpc.main.id

  health_check {
    enabled             = true
    path                = "/healthz"
    port                = "traffic-port"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    timeout             = 5
    interval            = 15
    matcher             = "200"
  }
}
```

**중요**: leader 인스턴스만 `/healthz` `200`, follower는 `503` (E25 `RequireLeaderForWrites`). ALB는 자동으로 leader 인스턴스로만 traffic을 보냄.

---

## 3. 운영자 cutover 절차 (failover 시)

### 3.1 자동 단계 (Route53)

1. **T+0:00** Primary region 장애 발생
2. **T+0:00** ALB-Seoul 503 (모든 인스턴스 down 또는 unhealthy)
3. **T+0:30** Route53 HC-primary 1차 fail
4. **T+1:00** HC-primary 2차 fail
5. **T+1:30** HC-primary 3차 fail → **Failover record가 Secondary로 자동 전환**
6. **T+1:30** 클라이언트 신규 DNS resolve → 10.2.0.10 (Tokyo)
7. **T+2:00** PagerDuty 알림 수신: "Primary down, Route53 failover triggered"

### 3.2 Manual 단계 (운영자, application promote)

자동 cutover는 DNS layer만 — application은 standby region에서 promote 필요:

```bash
# 1. Standby region SSH
ssh ec2-user@audit-tokyo.acme.com

# 2. PG promote (standby → primary)
sudo -u postgres psql -c "SELECT pg_promote();"
# 응답: pg_promote returns 't' (success)

# 3. rosshield-server 재시작 (--standby-mode=false + 또는 ROSSHIELD_REPLICATION_ROLE=primary)
sudo systemctl edit rosshield-server.service
# Environment="ROSSHIELD_REPLICATION_ROLE=primary"
sudo systemctl daemon-reload
sudo systemctl restart rosshield-server

# 4. leader-election 재시작 (PG advisory lock new epoch)
curl https://audit-tokyo.acme.com/healthz
# 응답에 "role": "leader" + "epoch" 증가 확인

# 5. ALB target health "OK" 전환 확인 (AWS Console → EC2 → Target Groups)
```

**총 RTO**: 자동 cutover 90초 + 운영자 promote 2분 + restart 60초 = **약 4분** (5분 안에서 여유).

### 3.3 사후 검증

```bash
# Client 측에서 새 DNS resolve 확인
dig audit.acme.com +short
# 출력: audit-tokyo.acme.com (또는 Tokyo ALB IP)

# write API 동작 확인
curl -X POST https://audit.acme.com/api/v1/audit/checkpoint \
  -H "Authorization: Bearer $TOKEN"
# 200 OK 응답 + 새 checkpoint hash
```

---

## 4. Primary 복구 후 절차

Primary region 복구 시 자동으로 traffic을 Primary로 되돌리지 **않음**. 안전 모델:

1. **Primary 복구 확인**
   ```bash
   # Seoul region SSH
   ssh ec2-user@audit-seoul.acme.com
   curl https://audit-seoul.acme.com/healthz
   # ha.role = "follower" 확인 (Tokyo가 leader)
   ```

2. **logical replication 방향 reverse**
   ```bash
   # Tokyo (new primary)에 publication 자동 sync — v0.7.0 ensurePublication 처리
   # Seoul (new standby)에 subscription 생성
   sudo -u postgres psql -c "
     CREATE SUBSCRIPTION rosshield_sub_from_tokyo
     CONNECTION 'host=audit-tokyo.acme.com user=replication ...'
     PUBLICATION rosshield_pub;
   "
   ```

3. **replication catch-up 대기** (수 분 ~ 수십 분 — Tokyo가 새로 받은 데이터 양에 비례)
   ```bash
   sudo -u postgres psql -c "
     SELECT subname, latest_end_lsn FROM pg_stat_subscription;
   "
   ```

4. **선택 1**: Tokyo 유지 (Tokyo가 새 primary로 영구 유지)
   - Route53에서 Primary record를 Tokyo로 변경
   - Seoul은 새 standby

5. **선택 2**: Seoul로 복귀
   - Tokyo에서 graceful demote + Seoul promote (정상 cutover 절차 반복)

**일반 권장**: 선택 1 (Tokyo 유지). 다음 region 장애 시 Seoul로 cutover. 운영 단순 + 빈번한 변경 회피.

---

## 5. Cloudflare alternative (B 옵션)

이미 Cloudflare 사용 중인 customer를 위한 동등 setup. Load Balancer 기능 ($5/month + $0.50/check) 사용.

### 5.1 Pool 생성

Cloudflare Dashboard → Traffic → Load Balancing → Create Pool:

| 필드 | Primary Pool | Secondary Pool |
|---|---|---|
| Name | `rosshield-primary` | `rosshield-secondary` |
| Origin | `audit-seoul.acme.com` | `audit-tokyo.acme.com` |
| Health Monitor | (아래 생성) | (아래 생성) |

### 5.2 Monitor 생성

```
Name:           rosshield-health-monitor
Type:           HTTPS
Method:         GET
Path:           /healthz
Interval:       30 seconds
Retries:        3
Timeout:        5 seconds
Expected Codes: 200
```

### 5.3 Load Balancer 생성

```
Hostname:       audit.acme.com
Default Pool:   rosshield-primary
Fallback Pool:  rosshield-secondary
TTL:            30 (Cloudflare proxy 직접 control이라 더 짧게 가능)
Steering policy: Random (다중 healthy pool 시) / Failover (Primary first)
```

**Cloudflare 강점**: TTL 30s (Route53 60s보다 빠른 cutover) + Global Anycast 200+ POP + DDoS 무료.

---

## 6. 자체 DNS guide (on-prem · air-gap)

### 6.1 BIND zone file

`/etc/bind/zones/acme.local`:

```bind
$TTL 60
@   IN  SOA  ns1.acme.local. admin.acme.local. (
            2026052001  ; serial (변경 시 +1)
            300         ; refresh
            60          ; retry
            604800      ; expire
            60 )        ; minimum
    IN  NS   ns1.acme.local.
    IN  NS   ns2.acme.local.

audit  IN  A   10.1.0.10   ; primary (Seoul)
```

### 6.2 Manual cutover

자동 health check 없음 — 외부 monitoring (Prometheus + Alertmanager)이 region 장애 감지 후 운영자 알림:

```bash
# Primary down 확인 후
ssh ns1.acme.local
sudo vim /etc/bind/zones/acme.local

# audit IN A 10.1.0.10 → audit IN A 10.2.0.10
# serial 증가: 2026052001 → 2026052002

sudo rndc reload acme.local

# 검증
dig @ns1.acme.local audit.acme.local +short
# 출력: 10.2.0.10
```

**RTO**: 자동 health check 없어서 운영자 detection + manual change = 약 5~15분 (air-gap customer 수용 가능 범위).

### 6.3 PowerDNS API (semi-automation)

PowerDNS는 REST API 제공 — runbook script로 cutover 자동화:

```bash
#!/usr/bin/env bash
# scripts/dns-cutover-pdns.sh
set -e

ZONE="acme.local"
NEW_IP="10.2.0.10"

curl -X PATCH "http://${PDNS_HOST}:8081/api/v1/servers/localhost/zones/${ZONE}." \
  -H "X-API-Key: $PDNS_KEY" \
  -d @- <<EOF
{
  "rrsets": [
    {
      "name": "audit.${ZONE}.",
      "type": "A",
      "ttl": 60,
      "changetype": "REPLACE",
      "records": [{"content": "${NEW_IP}", "disabled": false}]
    }
  ]
}
EOF

echo "DNS updated: audit.${ZONE} → ${NEW_IP}"
```

**RTO**: 운영자 detection + script 실행 = 약 2~5분 (수동 BIND 편집보다 빠름).

---

## 7. 검증 & 모니터링

### 7.1 정상 운영 확인

```bash
# Primary 정상
dig audit.acme.com +short
# 출력: audit-seoul.acme.com (또는 Seoul ALB IP)

curl https://audit.acme.com/healthz
# 200 OK + role=leader

# Route53 health check 상태
aws route53 get-health-check --health-check-id $HC_PRIMARY_ID
# 출력에 HealthCheckStatus: Healthy
```

### 7.2 Prometheus 메트릭 (E27)

`/metrics` endpoint에서 다음 시계열 모니터링:

- `rosshield_ha_role` (0=follower, 1=leader) — region별 cumulative
- `rosshield_ha_leader_epoch` — promote 카운트
- `rosshield_ha_failover_total` — failover 발생 수 (자동 증가)
- `rosshield_request_duration_seconds` (region label) — region별 latency

### 7.3 Alertmanager rule 예시

```yaml
groups:
- name: rosshield-multi-region
  rules:
  - alert: PrimaryRegionDown
    expr: up{job="rosshield-server",region="primary"} == 0
    for: 2m
    annotations:
      summary: "Primary region down — Route53 should auto-failover"

  - alert: ReplicationLagHigh
    expr: pg_stat_subscription_lag_seconds > 60
    for: 5m
    annotations:
      summary: "PG replication lag > 60s — RPO at risk"
```

---

## 8. 한계 / open issues

- **Route53 health check false positive**: AWS 자체 monitoring infrastructure 의존. 의심 시 application metric으로 cross-check.
- **자동 application promote 없음**: D-MR-3 = manual (split-brain 회피). Patroni/Stolon 통합은 Phase 9+.
- **read replica 활용 없음**: D-MR-5 = standby read 비활성. Phase 9+ 평가.
- **DNS provider 자체 장애**: 단일 provider 의존. multi-DNS는 별 epic (Route53 + Cloudflare 이중화).
- **stale DNS cache**: 일부 mobile/gateway 클라이언트가 TTL 무시. customer 측 retry 정책 가이드 필요.

---

## 9. 참조

- design [`multi-region-ha-stage4-design.md`](../design/notes/multi-region-ha-stage4-design.md) — 옵션 비교 + 결정 항목
- setup [`multi-region-ha-setup.md`](multi-region-ha-setup.md) — PG replication 전제 조건
- HA leader-election [`ha-deployment.md`](ha-deployment.md) — E25 single-region 결선
- Stage 5 failover runbook (예정) — `multi-region-failover-runbook.md`
