variable "account_id" {
  type        = string
  description = "Cloudflare account ID."
}

variable "zone_id" {
  type        = string
  description = "Cloudflare Zone ID."
}

variable "hostname" {
  type        = string
  description = "Customer-facing hostname (예: audit.acme.com)."
}

variable "primary_endpoint" {
  type        = string
  description = "Primary region origin FQDN."
}

variable "secondary_endpoint" {
  type        = string
  description = "Secondary region origin FQDN."
}

variable "health_check_path" {
  type        = string
  default     = "/healthz"
  description = "Health check path (rosshield E27 표준)."
}

variable "record_ttl" {
  type        = number
  default     = 30
  description = "Load Balancer DNS TTL (sec). Cloudflare proxy 직접 control."
}

variable "failure_threshold" {
  type        = number
  default     = 3
  description = "Monitor retry count."
}

variable "monitor_interval" {
  type        = number
  default     = 60
  description = "Health check interval (sec). Cloudflare Pro plan minimum = 60s."
}

variable "proxied" {
  type        = bool
  default     = true
  description = "Cloudflare proxy 활성 — DDoS 보호 + global anycast."
}

variable "notification_email" {
  type        = string
  default     = ""
  description = "Pool 상태 변경 알림 이메일 (옵션)."
}
