# root variables — envs/*.tfvars에서 주입.

variable "domain" {
  type        = string
  description = "Customer-facing domain (예: audit.acme.com). Route53 failover record의 record name."
}

variable "hosted_zone_id" {
  type        = string
  description = "Route53 Hosted Zone ID (예: Z0123456789ABCDEFGHIJ). aws route53 list-hosted-zones로 조회."
}

variable "primary_endpoint" {
  type        = string
  description = "Primary region endpoint FQDN (예: audit-seoul.acme.com). 일반적으로 ALB DNS name 또는 alias."
}

variable "secondary_endpoint" {
  type        = string
  description = "Secondary region endpoint FQDN (예: audit-tokyo.acme.com)."
}

variable "health_check_path" {
  type        = string
  description = "Health check 경로 — 기본 /healthz (rosshield E27 표준)."
  default     = "/healthz"
}

variable "record_ttl" {
  type        = number
  description = "Failover record TTL (초). 낮을수록 RTO 단축 + query 비용 증가."
  default     = 60

  validation {
    condition     = var.record_ttl >= 30 && var.record_ttl <= 3600
    error_message = "record_ttl must be between 30 and 3600 seconds."
  }
}

variable "failure_threshold" {
  type        = number
  description = "Health check failure 임계치 — 이 횟수만큼 연속 fail 시 unhealthy."
  default     = 3

  validation {
    condition     = var.failure_threshold >= 1 && var.failure_threshold <= 10
    error_message = "failure_threshold must be between 1 and 10."
  }
}

variable "request_interval" {
  type        = number
  description = "Health check interval (초)."
  default     = 30

  validation {
    condition     = contains([10, 30], var.request_interval)
    error_message = "request_interval must be either 10 or 30 (AWS Route53 제약)."
  }
}

variable "tags" {
  type        = map(string)
  description = "공통 tags — Environment·Project 등."
  default     = {}
}
