# Multi-region HA Cloudflare root config — Stage 4.4 산출물.
#
# Route53 대안 — customer가 이미 Cloudflare 사용 중인 enterprise 옵션.
# `multi-region-ha/modules/cloudflare-loadbalancer/`를 호출.
#
# Cloudflare API token이 환경 변수 `CLOUDFLARE_API_TOKEN`에 set되어야 합니다.

terraform {
  required_version = ">= 1.6"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.40"
    }
  }
}

provider "cloudflare" {
  # api_token은 CLOUDFLARE_API_TOKEN env에서 자동 로드.
}

module "cloudflare_lb" {
  source = "../multi-region-ha/modules/cloudflare-loadbalancer"

  account_id         = var.account_id
  zone_id            = var.zone_id
  hostname           = var.hostname
  primary_endpoint   = var.primary_endpoint
  secondary_endpoint = var.secondary_endpoint
  health_check_path  = var.health_check_path
  record_ttl         = var.record_ttl
  failure_threshold  = var.failure_threshold
  monitor_interval   = var.monitor_interval
  proxied            = var.proxied
  notification_email = var.notification_email
}
