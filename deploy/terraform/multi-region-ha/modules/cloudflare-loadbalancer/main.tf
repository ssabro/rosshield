# Cloudflare Load Balancer module — Pool + Monitor + Load Balancer.
#
# customer가 이미 Cloudflare 사용 중인 enterprise 옵션. Route53 module과 동등 동작:
# Primary endpoint health check + Secondary fallback.
#
# Cloudflare 강점: Global anycast 200+ POP + DDoS 무료 + TTL 30s 가능.
# 비용: $5/month Pro plan + Load Balancer 옵션 ($5+$0.50/check).

resource "cloudflare_load_balancer_monitor" "primary" {
  account_id     = var.account_id
  type           = "https"
  method         = "GET"
  path           = var.health_check_path
  port           = 443
  interval       = var.monitor_interval
  retries        = var.failure_threshold
  timeout        = 5
  expected_codes = "200"
  follow_redirects = false
  allow_insecure   = false
  description      = "rosshield primary health monitor (HTTPS GET ${var.health_check_path})"
}

resource "cloudflare_load_balancer_pool" "primary" {
  account_id = var.account_id
  name       = "rosshield-primary"
  origins {
    name    = "primary-origin"
    address = var.primary_endpoint
    enabled = true
    weight  = 1
  }
  monitor             = cloudflare_load_balancer_monitor.primary.id
  enabled             = true
  minimum_origins     = 1
  notification_email  = var.notification_email
  description         = "rosshield primary region pool"
}

resource "cloudflare_load_balancer_pool" "secondary" {
  account_id = var.account_id
  name       = "rosshield-secondary"
  origins {
    name    = "secondary-origin"
    address = var.secondary_endpoint
    enabled = true
    weight  = 1
  }
  monitor             = cloudflare_load_balancer_monitor.primary.id
  enabled             = true
  minimum_origins     = 1
  notification_email  = var.notification_email
  description         = "rosshield secondary region pool (failover)"
}

resource "cloudflare_load_balancer" "this" {
  zone_id          = var.zone_id
  name             = var.hostname
  ttl              = var.record_ttl
  proxied          = var.proxied
  steering_policy  = "off" # Failover (default_pools first, fallback if all unhealthy)
  default_pool_ids = [cloudflare_load_balancer_pool.primary.id]
  fallback_pool_id = cloudflare_load_balancer_pool.secondary.id
  description      = "rosshield multi-region failover load balancer"
  enabled          = true
}
