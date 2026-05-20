# Multi-region HA Terraform root config.
#
# customer 측 ALB·EC2·PG 인프라는 별 module — 본 root는 Route53 layer만 다룬다.
# Stage 4.3 산출물 (Phase 8 multi-region-ha-stage4-design.md §5).

terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
}

# Route53 health check는 us-east-1에서 생성·관리됨 (AWS 글로벌 service 표준).
# customer가 다른 region을 default로 쓰더라도 health check만은 us-east-1 provider 필요.
provider "aws" {
  region = "us-east-1"
}

module "route53_failover" {
  source = "./modules/route53-failover"

  domain             = var.domain
  hosted_zone_id     = var.hosted_zone_id
  primary_endpoint   = var.primary_endpoint
  secondary_endpoint = var.secondary_endpoint
  health_check_path  = var.health_check_path
  record_ttl         = var.record_ttl
  failure_threshold  = var.failure_threshold
  request_interval   = var.request_interval
  tags               = var.tags
}
