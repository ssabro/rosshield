variable "account_id" {
  type        = string
  description = "Cloudflare account ID — Load Balancer 리소스는 account-scoped."
}

variable "zone_id" {
  type        = string
  description = "Cloudflare Zone ID (예: acme.com). aws Route53 Hosted Zone과 동등 개념."
}

variable "hostname" {
  type        = string
  description = "Customer-facing hostname (예: audit.acme.com). Load Balancer의 DNS name."
}

variable "primary_endpoint" {
  type        = string
  description = "Primary region origin (예: audit-seoul.acme.com)."
}

variable "secondary_endpoint" {
  type        = string
  description = "Secondary region origin (예: audit-tokyo.acme.com)."
}

variable "health_check_path" {
  type        = string
  default     = "/healthz"
  description = "Health check path — rosshield E27 표준."
}

variable "record_ttl" {
  type        = number
  default     = 30
  description = "Load Balancer DNS TTL (sec). Cloudflare는 proxy 직접 control이라 30s 가능."

  validation {
    condition     = var.record_ttl >= 30 && var.record_ttl <= 3600
    error_message = "record_ttl must be between 30 and 3600 seconds."
  }
}

variable "failure_threshold" {
  type        = number
  default     = 3
  description = "Monitor retry count (연속 fail 임계치)."

  validation {
    condition     = var.failure_threshold >= 1 && var.failure_threshold <= 10
    error_message = "failure_threshold must be between 1 and 10."
  }
}

variable "monitor_interval" {
  type        = number
  default     = 60
  description = "Health check interval (sec). Cloudflare minimum = 60s (Pro plan)."

  validation {
    condition     = var.monitor_interval >= 60
    error_message = "monitor_interval must be >= 60 (Cloudflare Pro plan limit)."
  }
}

variable "proxied" {
  type        = bool
  default     = true
  description = "Cloudflare proxy 활성 — DDoS 보호 + global anycast 적용. false면 DNS only."
}

variable "notification_email" {
  type        = string
  default     = ""
  description = "Pool 상태 변경 시 알림 받을 이메일 (옵션)."
}
