# Multi-region HA Cloudflare Terraform IaC

> **상태**: Stage 4.4 산출물 (Phase 8 Multi-region HA carryover).
> **선행**: `docs/operations/multi-region-dns.md` §5 Cloudflare alternative + Pro plan + Load Balancer 옵션 가입.
> **목표**: Cloudflare 사용 중인 enterprise customer를 위한 Route53 대안 — Pool + Monitor + Load Balancer Terraform 자동화.
> **비목표**: Cloudflare Workers · WAF custom rule (별 epic).

---

## 구조

```
deploy/terraform/multi-region-ha-cloudflare/
├── README.md                 # 본 문서
├── main.tf                   # provider 설정 + module 호출
├── variables.tf              # root input
├── outputs.tf                # root output
├── .gitignore                # envs/*.tfvars 보호
└── envs/
    └── example.tfvars        # customer 복제용
```

본 root는 `multi-region-ha/modules/cloudflare-loadbalancer/` module을 호출 (Route53 module과 같은 부모 디렉터리 공유).

---

## 사용법

### 1. Cloudflare prerequisite

- Cloudflare 계정 + Pro plan (Load Balancer 옵션 필요, $5/month + $0.50/check)
- API token 생성 (Cloudflare Dashboard → My Profile → API Tokens):
  - 권한: `Zone:DNS:Edit`, `Zone:Load Balancer:Edit`, `Account:Load Balancer:Edit`
- account_id + zone_id 조회 (Dashboard 우측 사이드바)
- 양 region origin endpoint (보통 ALB DNS name 또는 EC2 IP)

### 2. 변수 설정

```bash
cd deploy/terraform/multi-region-ha-cloudflare
cp envs/example.tfvars envs/prod.tfvars
# prod.tfvars 편집 (account_id·zone_id·hostname·endpoint)
```

### 3. Apply

```bash
export CLOUDFLARE_API_TOKEN=<your-api-token>

terraform init
terraform plan -var-file=envs/prod.tfvars -out=tfplan
terraform apply tfplan
```

### 4. 검증

```bash
# Cloudflare API로 LB 상태 확인
curl -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/accounts/$(terraform output -raw load_balancer_id)/load_balancers" \
  | jq

# DNS resolve 확인
dig audit.acme.com +short
# Expected: Cloudflare proxy IP (proxied=true) 또는 primary_endpoint (proxied=false)
```

### 5. Destroy

```bash
terraform destroy -var-file=envs/prod.tfvars
```

---

## Cloudflare vs Route53 비교

| 측면 | Cloudflare | Route53 |
|---|---|---|
| Global anycast | ✅ 200+ POP | 부분 (Route53 자체는 anycast) |
| DDoS protection | ✅ 무료 (proxy on) | 별 WAF ($) |
| TTL minimum | 30s | 60s |
| AWS Cloud 통합 | 없음 | ✅ |
| 월 비용 | $5 Pro + Load Balancer ($5+$0.5/check) | usage-based |
| Monitor interval | 60s+ (Pro) | 30s+ |

**Cloudflare 권장 시나리오**: customer가 이미 Cloudflare proxy 사용 + DDoS 보호 필요 + AWS 외 multi-cloud 환경.

**Route53 권장 시나리오**: AWS multi-region 가정 + IaC 표준 (deploy/terraform/multi-region-ha/ 사용).

---

## staging 환경 분기

```bash
cp envs/example.tfvars envs/staging.tfvars
# staging.tfvars 변수 변경 (hostname·endpoint)
terraform workspace new staging
terraform plan -var-file=envs/staging.tfvars
```

---

## Failover 절차

기본 Cloudflare Load Balancer는 자동 cutover:
- Monitor가 Primary pool 3회 연속 fail 감지 (180초)
- traffic이 Secondary pool로 자동 전환
- TTL 30s 후 모든 client에 적용 → 총 RTO ≤ 4분

Application promote (PG `pg_promote()` + rosshield-server restart)는 운영자 manual — Route53과 동일 절차 (`docs/operations/multi-region-failover-runbook.md` §3 참조).

---

## 참조

- design [`multi-region-ha-stage4-design.md`](../../../docs/design/notes/multi-region-ha-stage4-design.md) §7 Cloudflare alternative
- ops [`multi-region-dns.md`](../../../docs/operations/multi-region-dns.md) §5 Cloudflare manual setup
- Route53 alternative [`multi-region-ha/README.md`](../multi-region-ha/README.md)
- Cloudflare Terraform Provider docs: https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs
