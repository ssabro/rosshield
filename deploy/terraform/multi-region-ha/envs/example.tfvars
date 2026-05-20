# Multi-region HA Terraform 환경 변수 예시.
#
# 본 파일을 envs/prod.tfvars로 복사 후 customer 환경 값으로 수정.
# envs/*.tfvars는 .gitignore 대상 — secret 또는 customer-specific config 보호.

domain             = "audit.acme.com"
hosted_zone_id     = "Z0123456789ABCDEFGHIJ"

primary_endpoint   = "audit-seoul.acme.com"
secondary_endpoint = "audit-tokyo.acme.com"

# RTO ≤ 60초 보장 default. 비용 절감 시 300 가능 (TTL up 시 RTO 동일 비율 증가).
record_ttl = 60

# 3회 연속 fail / 30초 interval = 90초 detection window. false positive 회피.
failure_threshold = 3
request_interval  = 30

# /healthz는 rosshield E27 standard endpoint — leader 인스턴스만 200 OK.
health_check_path = "/healthz"

tags = {
  Environment = "prod"
  Project     = "rosshield"
  Owner       = "ops-team"
}
