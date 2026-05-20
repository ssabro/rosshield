# Multi-region HA Terraform IaC sample

> **상태**: Stage 4.3 산출물 (v0.7.x 후속, Phase 8 Stage 4).
> **선행**: `docs/operations/multi-region-dns.md` Route53 setup 가이드 정독.
> **목표**: Route53 failover record + health check를 customer가 즉시 plan/apply 가능한 Terraform module 형태로 제공.
> **비목표**: ALB 본체 (customer 측 infra), PG 인프라 (별 customer 책임), Cloudflare alternative (Stage 4.4 별 module).

---

## 구조

```
deploy/terraform/multi-region-ha/
├── README.md                  # 본 문서
├── main.tf                    # root config (provider + module 호출)
├── variables.tf               # root input
├── outputs.tf                 # root output
├── modules/
│   └── route53-failover/      # Route53 failover record + health check (재사용 module)
│       ├── main.tf
│       ├── variables.tf
│       └── outputs.tf
└── envs/
    └── example.tfvars         # 환경 변수 예시 (customer 측 복제)
```

## 사용법

### 1. customer 측 prerequisite

- AWS 계정 + Route53 Hosted Zone (예: `acme.com`)
- 양 region (Seoul + Tokyo) ALB 또는 EC2 endpoint:
  - `audit-seoul.acme.com` (Primary)
  - `audit-tokyo.acme.com` (Secondary)
- ALB target group에 rosshield-server 인스턴스 등록 (별 customer 측 module)

### 2. 변수 설정

```bash
cp envs/example.tfvars envs/prod.tfvars
# prod.tfvars 편집
```

```hcl
domain               = "audit.acme.com"
hosted_zone_id       = "Z0123456789ABCDEFGHIJ"
primary_endpoint     = "audit-seoul.acme.com"
secondary_endpoint   = "audit-tokyo.acme.com"
health_check_path    = "/healthz"
record_ttl           = 60
failure_threshold    = 3
request_interval     = 30
tags = {
  Environment = "prod"
  Project     = "rosshield"
}
```

### 3. Apply

```bash
cd deploy/terraform/multi-region-ha

terraform init
terraform plan -var-file=envs/prod.tfvars -out=tfplan
terraform apply tfplan
```

### 4. 검증

```bash
dig audit.acme.com +short
# 정상: audit-seoul.acme.com (또는 Seoul ALB IP)

# Route53 health check 상태
aws route53 get-health-check --health-check-id $(terraform output -raw primary_health_check_id)
# HealthCheckStatus: Healthy
```

## destroy

```bash
terraform destroy -var-file=envs/prod.tfvars
```

**주의**: destroy는 Route53 record를 제거하므로 production traffic이 NXDOMAIN. 다른 record(예: `*.acme.com` wildcard)로 대체 후 destroy 권장.

## 추가 환경 (staging)

```bash
cp envs/example.tfvars envs/staging.tfvars
# staging.tfvars 변수 변경 (domain·hosted_zone·endpoint)
terraform workspace new staging
terraform plan -var-file=envs/staging.tfvars
```

## Cloudflare alternative

Stage 4.4 후속에서 `modules/cloudflare-loadbalancer/` 추가 예정. 본 round는 Route53만.

## 참조

- design [`multi-region-ha-stage4-design.md`](../../../docs/design/notes/multi-region-ha-stage4-design.md)
- ops doc [`multi-region-dns.md`](../../../docs/operations/multi-region-dns.md)
