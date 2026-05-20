# Route53 failover record + health check module.
#
# customer가 plan/apply 즉시 가능한 self-contained module. ALB·EC2 같은 region별
# 인프라는 customer 책임 — 본 module은 endpoint FQDN만 입력 받는다.
#
# 동작:
#   1. Primary endpoint를 HTTPS /healthz로 폴링하는 health check 생성
#   2. Secondary endpoint도 동일 health check 생성
#   3. Route53 Failover record 2건 (Primary + Secondary) 생성
#   4. Primary health check fail 시 Route53이 자동으로 Secondary로 cutover

resource "aws_route53_health_check" "primary" {
  fqdn              = var.primary_endpoint
  port              = 443
  type              = "HTTPS"
  resource_path     = var.health_check_path
  failure_threshold = var.failure_threshold
  request_interval  = var.request_interval
  measure_latency   = true

  tags = merge(var.tags, {
    Name = "${var.domain}-primary-health"
    Role = "primary"
  })
}

resource "aws_route53_health_check" "secondary" {
  fqdn              = var.secondary_endpoint
  port              = 443
  type              = "HTTPS"
  resource_path     = var.health_check_path
  failure_threshold = var.failure_threshold
  request_interval  = var.request_interval
  measure_latency   = true

  tags = merge(var.tags, {
    Name = "${var.domain}-secondary-health"
    Role = "secondary"
  })
}

resource "aws_route53_record" "primary" {
  zone_id = var.hosted_zone_id
  name    = var.domain
  type    = "CNAME"
  ttl     = var.record_ttl

  set_identifier  = "primary"
  health_check_id = aws_route53_health_check.primary.id

  failover_routing_policy {
    type = "PRIMARY"
  }

  records = [var.primary_endpoint]
}

resource "aws_route53_record" "secondary" {
  zone_id = var.hosted_zone_id
  name    = var.domain
  type    = "CNAME"
  ttl     = var.record_ttl

  set_identifier  = "secondary"
  health_check_id = aws_route53_health_check.secondary.id

  failover_routing_policy {
    type = "SECONDARY"
  }

  records = [var.secondary_endpoint]
}
