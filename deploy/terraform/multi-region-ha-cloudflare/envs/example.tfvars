# Multi-region HA Cloudflare 환경 변수 예시 (Stage 4.4).
#
# 본 파일을 envs/prod.tfvars로 복사 후 customer 환경 값으로 수정.

account_id  = "0123456789abcdef0123456789abcdef"
zone_id     = "abcdef0123456789abcdef0123456789"
hostname    = "audit.acme.com"

primary_endpoint   = "audit-seoul.acme.com"
secondary_endpoint = "audit-tokyo.acme.com"

# Cloudflare proxy 직접 control이라 TTL 30s 가능 (Route53 60s 대비 단축).
record_ttl = 30

# Cloudflare Pro plan minimum interval = 60s.
failure_threshold = 3
monitor_interval  = 60

# Cloudflare proxy 활성 — DDoS 보호 + global anycast. false면 DNS only.
proxied = true

# Pool 상태 변경 알림 (옵션).
notification_email = "ops@acme.com"

health_check_path = "/healthz"
