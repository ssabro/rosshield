# Multi-region HA Stage 4 — DNS Routing + Failover Records 설계

> **상태**: Design draft (Phase 8 carryover, D-MR-4 의존).
> **작성일**: 2026-05-20
> **범위**: cross-region failover 발생 시 client traffic을 standby region으로 옮기는 DNS-layer 메커니즘. 4 DNS provider option 비교 + Route53 권장 setup + health check + TTL 정책 + on-prem 자체 DNS 가이드 + IaC scaffolding.
> **참조**: `notes/multi-region-ha-design.md` §5 Stage 4·5, `notes/e25-ha-design.md` (single-region leader-election), v0.7.x carryover.
> **비목표**: 자동 failover 트리거 코드(Stage 6 carryover), application 계층 routing(LB·anycast, 별 epic), DDoS 보호 통합(WAF·Cloudflare Magic Transit 별 epic).
> **코드 변경**: 0건. 본 round는 docs only — Stage 4 진입은 D-MR-4 확정 후 별 PR(IaC + ops doc 분리).

---

## 1. 상태 / 배경

### 1.1 v0.7.x까지 진척

- **Stage 1·2** (v0.6.6) — replication metadata + standby middleware (PG logical replication 가정, sqlite/PG dual-write 어댑터).
- **Stage 3** (v0.6.8) — PUBLICATION/SUBSCRIPTION 자동 setup (idempotent, FOR ALL TABLES default).
- **Stage 3 후속** (v0.7.0) — publication tables 자동 sync + slot cleanup helper + bootstrap cron 결선.

**현재 한계**: replication은 동작하지만 region 장애 시 client traffic을 standby region으로 옮기는 **routing 계층 자동화 0**. 운영자가 수동 DNS 변경하지 않으면 client는 down된 primary region을 계속 호출.

### 1.2 enterprise customer 요구 시나리오

`multi-region-ha-design.md` §1.3에서 정의:
- **RPO ≤ 1분** — replication lag 정상 범위
- **RTO ≤ 5분** — region 장애 감지 + standby promote + DNS cutover 완료
- 한국 region 장애 시 일본·미국 region에서 read 계속

Stage 4의 책임: **DNS RTO ≤ 60초** 보장 (전체 RTO 5분 안에서 DNS layer 부담분).

### 1.3 RPO·RTO 분해

| 단계 | 책임 | 목표 |
|---|---|---|
| 감지 | Health check (DNS provider) | ≤ 30초 (3회 연속 실패) |
| 운영자 통지 | PagerDuty/Opsgenie | ≤ 60초 |
| standby promote 결정 | runbook (Stage 5) | ≤ 2분 |
| `SELECT pg_promote()` 실행 | 운영자 manual | ≤ 30초 |
| application 재시작 + leader-elect | bootstrap | ≤ 60초 |
| DNS record update | DNS provider API | ≤ TTL (60초 권장) |
| client cache 만료 | OS resolver | ≤ TTL (60초 권장) |
| **총 RTO** | | **≤ 5분** |

---

## 2. 위협 모델 / 요구사항

### 2.1 위협

| 위협 | 발생 | 영향 | Stage 4 대응 |
|---|---|---|---|
| regional 장애 (region 전체 다운) | 연 1~2회 | client → primary endpoint NXDOMAIN/timeout | health check + failover record |
| DNS provider 자체 장애 | 연 0~1회 | 모든 region client routing 마비 | multi-DNS (Route53 primary + Cloudflare secondary, 별 epic) |
| split-brain (primary 살아있지만 health check 실패) | network partition 시 | client 일부는 primary로, 일부는 standby로 → 데이터 일관성 깨짐 | 운영자 manual cutover + fence token (E25) |
| stale DNS cache (resolver TTL 무시) | 일부 client (모바일·게이트웨이) | 5분~24h 동안 primary 호출 유지 | low TTL (60초) + customer 측 retry 정책 권장 |
| health check 자체 false positive | 분기 0~3회 | 정상 region을 down으로 오인 → 불필요 cutover | 3회 연속 + 임계치 + 운영자 manual 승인 (자동 cutover 안 함) |
| customer 측 자체 DNS (on-prem · 사설망) | 상시 | Route53 무효 | BIND/PowerDNS gateway 가이드 별첨 |

### 2.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| **R1** | DNS RTO ≤ 60초 (record update 후 글로벌 전파) | TTL = 60초 강제 |
| **R2** | health check 임계치 = 3회 연속 (30초 간격) — false positive 회피 | 명시 설정 |
| **R3** | failover는 manual approval (자동 cutover 없음) | runbook (Stage 5) |
| **R4** | on-prem customer 자체 DNS 지원 | BIND zone file 예시 + 운영 가이드 |
| **R5** | DNS provider lock-in 최소화 | Route53·Cloudflare 양쪽 IaC sample + abstract terraform module |
| **R6** | health check endpoint 무인증 | `/healthz` 사용 (이미 인증 무관 — E27) |

---

## 3. 옵션 비교 (DNS provider 4종)

### 3.1 매트릭스

| Provider | health check | failover record | latency record | weighted record | TTL min | 비용 (월) | 강점 | 약점 |
|---|---|---|---|---|---|---|---|---|
| **A) Route53** | ✅ HTTPS + endpoint | ✅ Primary/Secondary | ✅ region-based | ✅ | 60s | $0.50/health + $0.40/M queries | AWS 통합 + IaC 성숙 | AWS lock-in |
| B) Cloudflare | ✅ HTTPS + custom path | ✅ Load Balancer 기능 | ✅ Geo Steering | ✅ | 60s | $5+ Pro (Load Balancer +$5) | global anycast + DDoS 무료 | Pro 구독 |
| C) NS1 | ✅ multi-protocol + advanced | ✅ Filter Chain | ✅ Geofencing | ✅ | 60s | $100+ enterprise | filter chain 강력 + filtered | 비용 + 학습 곡선 |
| D) 자체 DNS (BIND/PowerDNS) | ❌ (외부 monitoring 필요) | 수동 zone reload | 수동 view | 수동 SRV | 60s+ | infra cost | 완전 통제 + air-gap 가능 | 운영 부담 |

### 3.2 권장

**A) Route53** (D-MR-4 default). 근거:
- AWS multi-region 가정 (rosshield-server는 EC2/ECS/EKS 위에 배포 — `multi-region-ha-design.md` §1.3)
- Health check가 HTTPS endpoint를 30초 간격으로 폴링 + 3회 실패 시 Secondary로 자동 cutover
- IaC (Terraform `aws_route53_health_check` + `aws_route53_record` Primary/Secondary)이 표준
- on-prem customer는 D 자체 DNS로 fallback (별 가이드 §6)

**B Cloudflare**는 enterprise customer가 이미 Cloudflare 사용 중인 경우 대안 (별 가이드 §7).

---

## 4. Route53 권장 setup (D-MR-4 = A)

### 4.1 아키텍처

```
Client (audit.acme.com)
    │
    ▼
Route53 Hosted Zone (acme.com)
    │
    ├─ Record: audit.acme.com (Primary)
    │     Routing policy: Failover
    │     Set ID: primary
    │     Health check: HC-primary (→ https://audit-seoul.acme.com/healthz)
    │     Value: audit-seoul.acme.com (ALIAS to ALB/NLB primary)
    │     TTL: 60s
    │
    └─ Record: audit.acme.com (Secondary)
          Routing policy: Failover
          Set ID: secondary
          Health check: HC-secondary (→ https://audit-tokyo.acme.com/healthz)
          Value: audit-tokyo.acme.com (ALIAS to ALB/NLB standby)
          TTL: 60s

Primary 정상 → audit.acme.com → audit-seoul.acme.com
Primary 다운 (HC-primary 3회 연속 fail) → audit.acme.com → audit-tokyo.acme.com
```

### 4.2 Health check 명세

```hcl
resource "aws_route53_health_check" "primary" {
  fqdn              = "audit-seoul.acme.com"
  port              = 443
  type              = "HTTPS"
  resource_path     = "/healthz"
  failure_threshold = 3
  request_interval  = 30
  measure_latency   = true

  tags = {
    Name        = "rosshield-primary-health"
    Region      = "ap-northeast-2"
    Environment = "prod"
  }
}
```

**`/healthz` 응답 가정** (E27 표준):
```json
{
  "status": "ok",
  "components": { "storage": "ok", ... },
  "audit": { "status": "ok", ... },
  "ha": { "enabled": true, "role": "leader", "epoch": 3 }
}
```

Route53은 `200 OK` 응답을 health pass로 간주. `503` 또는 timeout 3회 → fail. **단, 본 health check는 region 전체 endpoint를 검사** — HA Stage 3의 `RequireLeaderForWrites` middleware가 follower instance에 `503`을 반환하므로, ALB가 follower instance를 health check에서 제외(503 → unhealthy target → out-of-rotation)하도록 ALB target health 설정 분리.

### 4.3 ALB target health (region 내부)

ALB가 region 내 다중 인스턴스를 가짐. 각 인스턴스의 `/healthz`를 ALB target group health check가 폴링:

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

leader 인스턴스만 `200`, follower는 `503` (E25 RequireLeaderForWrites). ALB는 leader 인스턴스로만 traffic을 보냄.

**중요**: write API(POST/PUT/DELETE)는 leader만 처리. read API(GET)는 follower도 가능하지만 본 Stage 4는 단순화를 위해 leader-only routing 가정. read replica 활용은 D-MR-5에서 Phase 9+로 deferral.

### 4.4 TTL 정책

| Record | TTL | 근거 |
|---|---|---|
| audit.acme.com (failover record) | 60s | RTO ≤ 60초 보장 |
| audit-seoul.acme.com (ALIAS to ALB) | (ALB가 관리) | AWS 내부 |
| audit-tokyo.acme.com (ALIAS to ALB) | (ALB가 관리) | AWS 내부 |

**낮은 TTL 비용**: query 수 증가 (60초마다 resolve). 월 ~$0.40/M queries 가정 + customer base 10,000 endpoint → 월 ~$170 추가. enterprise customer 수용 가능 범위.

### 4.5 운영자 manual cutover 절차

자동 cutover (Route53 health check failover record)는 **첫 단계**만 자동:
1. Route53이 HC-primary 3회 연속 fail 감지 (≤ 90초)
2. Route53이 자동으로 Secondary record로 traffic 전환

**그러나 standby region의 PG가 promote되지 않으면 write API가 동작하지 않음** — 별 작업 필요:
1. 운영자가 PagerDuty 알림 수신
2. standby region SSH: `psql -c "SELECT pg_promote();"`
3. rosshield-server standby instance를 `--standby-mode=false`로 재시작 (또는 ENV 변경 후 reload)
4. leader-election 재시작 (PG advisory lock 새 epoch)
5. ALB target health가 `200` 인식 → traffic 수용 시작

**총 RTO**: Route53 자동 cutover 90초 + 운영자 promote 2분 + application restart 60초 = **약 4분** (RTO 5분 안에서 여유).

---

## 5. Terraform IaC scaffolding

### 5.1 권장 모듈 구조

```
deploy/terraform/multi-region-ha/
├── main.tf                  # provider 설정, locals
├── variables.tf             # region·domain·환경 입력
├── outputs.tf               # Route53 record FQDN
├── modules/
│   ├── route53-failover/    # 본 stage 핵심 (health check + failover record)
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── outputs.tf
│   └── alb-region/          # region별 ALB + target group
│       ├── main.tf
│       └── variables.tf
└── envs/
    ├── prod.tfvars          # prod 환경 변수
    └── staging.tfvars       # staging 환경 변수
```

### 5.2 핵심 변수

```hcl
variable "domain" {
  type        = string
  description = "Lodestar customer domain (예: audit.acme.com)"
}

variable "primary_region" {
  type        = string
  description = "AWS region (예: ap-northeast-2)"
}

variable "secondary_region" {
  type        = string
  description = "AWS region (예: ap-northeast-1)"
}

variable "ttl_seconds" {
  type        = number
  default     = 60
  description = "Route53 record TTL (RTO 직결)"
}
```

### 5.3 IaC 산출 (별 Stage 4 후속 commit)

- `deploy/terraform/multi-region-ha/` 신규 디렉터리
- README + customer 측 적용 절차
- staging env로 검증 (별 customer 측 의존 — Lodestar 본 repo는 IaC sample만)

---

## 6. 자체 DNS 가이드 (on-prem customer, D 옵션)

### 6.1 시나리오

- air-gap 환경 (외부 DNS provider 미사용)
- 사설망 (사내 BIND/PowerDNS · Active Directory DNS)
- 정부 customer (외부 cloud DNS 거부)

### 6.2 BIND zone file 예시

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

audit IN  A   10.1.0.10   ; primary (Seoul)
; failover 시 운영자가 수동 변경:
; audit IN  A   10.2.0.10   ; secondary (Tokyo)
```

### 6.3 운영 절차 (자체 DNS)

자동 health check 없음 — 외부 monitoring(Prometheus + Alertmanager 등)이 region 장애 감지 후 운영자 알림. 운영자가 zone file 수동 update:

```bash
# Primary down 확인 후
ssh ns1.acme.local
sudo vim /etc/bind/zones/acme.local
# audit IN A 10.1.0.10 → audit IN A 10.2.0.10
sudo rndc reload acme.local
```

**RTO 영향**: 자동 health check 없어서 운영자 detection 시간 + manual change 시간 = 약 5~15분 (cloud DNS보다 길지만 air-gap customer 수용 가능 범위).

### 6.4 PowerDNS API 사용 (semi-자동)

PowerDNS는 REST API 제공 — runbook script로 cutover 자동화 가능:

```bash
curl -X PATCH http://powerdns:8081/api/v1/servers/localhost/zones/acme.local \
  -H "X-API-Key: $PDNS_KEY" \
  -d '{"rrsets":[{"name":"audit.acme.local.","type":"A","ttl":60,"changetype":"REPLACE","records":[{"content":"10.2.0.10"}]}]}'
```

---

## 7. Cloudflare 대안 (B 옵션, customer-가능)

이미 Cloudflare 사용 중인 customer를 위한 동등 setup. Load Balancer 기능 ($5/month + $0.50/check) 사용.

### 7.1 Cloudflare Load Balancer

- **Pool**: primary (audit-seoul.acme.com) + secondary (audit-tokyo.acme.com)
- **Monitor**: HTTPS check on `/healthz`, 30s interval, 3 retry
- **Steering policy**: Failover (Primary first, Secondary fallback)
- **TTL**: 30s (Cloudflare proxy 직접 control이라 더 짧게 가능)

### 7.2 강점·약점

| 측면 | Cloudflare | Route53 |
|---|---|---|
| Global anycast | ✅ 200+ POP | 부분 (Route53 자체는 anycast) |
| DDoS protection | ✅ 무료 (proxy on) | 별 WAF ($) |
| TTL minimum | 30s | 60s |
| AWS Cloud 통합 | 없음 | ✅ |
| 비용 | $5/month Pro + Load Balancer | usage-based |

---

## 8. 실 cutover 시나리오 walk-through

### 8.1 정상 운영

```
Time T+0   : Primary (Seoul) 정상, audit.acme.com → 10.1.0.10
Time T+0   : Standby (Tokyo) PG replication lag = 0.5s
```

### 8.2 Primary 다운

```
Time T+0:00 : Seoul AWS region 장애 발생
Time T+0:00 : ALB-Seoul 503 (모든 인스턴스 down)
Time T+0:30 : Route53 HC-primary 1차 fail
Time T+1:00 : HC-primary 2차 fail
Time T+1:30 : HC-primary 3차 fail → Failover record가 Secondary로 전환
Time T+1:30 : 클라이언트가 새로 resolve하는 경우 → 10.2.0.10 (Tokyo)
Time T+2:00 : PagerDuty 알림: "Primary down, Route53 failover triggered"
```

### 8.3 운영자 promote (manual)

```
Time T+2:00 : 운영자 SSH 접속 (audit-tokyo.acme.com)
Time T+2:30 : psql -c "SELECT pg_promote();"
Time T+3:00 : rosshield-server standby instance 재시작 (--standby-mode=false)
Time T+3:30 : leader-election 완료 (PG advisory lock new epoch)
Time T+4:00 : ALB target health "OK" → ALB가 신규 leader로 routing
Time T+4:00 : write API 동작 시작
```

### 8.4 Primary 복구

```
Time T+1:00:00 : Seoul region 복구
Time T+1:00:00 : Primary PG 재시작
Time T+1:00:00 : Seoul PG가 logical replication subscription을 Tokyo로부터 fetch
Time T+1:10:00 : replication catch-up 완료
Time T+1:10:00 : 운영자 Primary record를 Seoul로 복구 (현재 Tokyo가 새 primary 역할)
```

**중요**: 복구 후 Seoul을 다시 primary로 만들지, Tokyo를 primary로 유지할지 결정. 일반적으로 **Tokyo가 새 primary로 유지** — Seoul은 새 standby. 다음 region 장애 시 Seoul로 cutover.

---

## 9. Stage 분해 (Stage 4 진입 후)

| Sub-stage | 작업 | 산출 | 추정 |
|---|---|---|---|
| **4.1** | 본 design doc + D-MR-4 확정 | 본 문서 + SESSION_HANDOFF 결정 로그 | (본 round) |
| **4.2** | ops doc 분리 | `docs/operations/multi-region-dns.md` — Route53·Cloudflare·자체 DNS 각 환경 가이드 | 2일 |
| **4.3** | Terraform IaC sample | `deploy/terraform/multi-region-ha/` — Route53-failover module + ALB module + envs/ | 3일 |
| **4.4** | Cloudflare alternative | 같은 ops doc 안에 §7 확장 + Terraform CF module (`deploy/terraform/multi-region-ha/modules/cloudflare-lb/`) | 2일 |
| **4.5** | 자체 DNS BIND/PowerDNS 가이드 | 같은 ops doc 안에 §6 확장 | 1일 |
| **4.6** | staging env 검증 | 별 customer 측 — Lodestar 본 repo는 staging IaC plan 산출만 | (별 외부 트랙) |

Stage 4.2~4.5는 docs 작업이라 1주 안에 완료 가능. Stage 4.6은 외부 트랙.

**Stage 5 (runbook)은 Stage 4 직후 자연 후속** — `docs/operations/multi-region-failover-runbook.md` 단일 doc.

---

## 10. 결정 항목 (D-MR-4 sub-decisions)

### D-MR-4-A: Primary DNS provider

- **A) Route53** (권장 default) — AWS multi-region 가정
- B) Cloudflare — multi-cloud customer (별 customer 측 결정)
- C) 자체 DNS — on-prem · air-gap customer
- D) NS1 — enterprise filter chain 고급 (별 epic)

권장: **A**, customer 환경별로 B 또는 C 대안 명시.

### D-MR-4-B: TTL

- **A) 60s** (권장 default) — RTO ≤ 60초 보장
- B) 30s — 더 빠른 cutover (Cloudflare만 가능)
- C) 300s — 비용 절감 but RTO 5분 영향

권장: **A**.

### D-MR-4-C: Health check threshold

- **A) 3회 연속 fail / 30초 interval** (권장 default) — false positive 회피
- B) 2회 연속 fail / 15초 interval — 더 빠른 detection but flaky network risk
- C) 5회 연속 fail / 60초 interval — 보수적

권장: **A**.

### D-MR-4-D: failover 자동 cutover vs manual

- **A) DNS layer는 자동 (Route53 health check) + application layer는 manual (Stage 5 runbook)** (권장 default) — split-brain 위험 회피
- B) 완전 자동 (Patroni/Stolon + Route53 API) — Phase 9+ deferral
- C) 완전 manual (DNS도 운영자 수동 변경) — RTO 영향 큼

권장: **A**. application promote는 사람 판단 필요(split-brain · network partition 구분).

### D-MR-4-E: Terraform vs CDK vs manual

- **A) Terraform** (권장 default) — multi-cloud 표준 IaC
- B) AWS CDK — AWS lock-in 이미 수용한 customer
- C) Manual Console — 소규모 customer

권장: **A**. Terraform sample 제공 + Console 절차도 ops doc §11에 fallback.

---

## 11. 위험 / open issues

### 11.1 Route53 health check false positive

Route53 health check는 자체 monitoring infrastructure를 사용 — AWS 자체 장애로 health check가 false alarm 가능. 대응:
- 운영자 PagerDuty 알림에 application-level metric (예: `rosshield_request_total`)도 cross-check
- 의심 시 manual fail-back으로 graceful 회피

### 11.2 cross-region 비용 (data transfer)

logical replication 트래픽 (PG primary → standby) 비용:
- Seoul → Tokyo: 약 $0.08/GB
- 일반 audit chain size (월 ~10GB) → 월 $0.80 (무시 가능)
- 대량 evidence binary 포함 시: 월 100GB → $8 (수용 가능)

### 11.3 PG cross-region replication 자체 안정성

PG logical replication의 known 이슈:
- 신규 column 추가 시 publication 자동 sync (v0.7.0에서 처리)
- DDL replication 안 함 — schema migration은 양쪽 region에서 수동 적용 필요 (운영 docs)

### 11.4 standby region read traffic 활용

D-MR-5 Phase 9+ deferral. 본 Stage 4는 standby를 cold standby로 가정 — read load balancing 안 함.

---

## 12. 결론

- **Route53 failover record + ALB target health + manual application promote** 조합으로 RTO ≤ 5분 달성 가능.
- DNS layer는 자동 cutover, application layer는 manual (split-brain 회피).
- on-prem customer는 BIND/PowerDNS 가이드로 fallback.
- 본 design은 docs only — Stage 4 진입은 D-MR-4 확정 후 ops doc + Terraform module 작성 (1주 내 완료).

**다음 round**: D-MR-4 결정 (A·B·C·D·E) → 사용자 확정 후 Stage 4.2~4.5 진입.
