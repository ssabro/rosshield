variable "domain" {
  type        = string
  description = "Customer-facing domain — Route53 record name. 예: audit.acme.com"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route53 Hosted Zone ID — aws route53 list-hosted-zones."
}

variable "primary_endpoint" {
  type        = string
  description = "Primary region endpoint FQDN (보통 ALB DNS name)."
}

variable "secondary_endpoint" {
  type        = string
  description = "Secondary region endpoint FQDN."
}

variable "health_check_path" {
  type        = string
  default     = "/healthz"
  description = "HTTP path for health check probe (rosshield E27 standard)."
}

variable "record_ttl" {
  type        = number
  default     = 60
  description = "Failover record TTL (sec). low TTL = fast cutover + higher query cost."
}

variable "failure_threshold" {
  type        = number
  default     = 3
  description = "Health check failure threshold — consecutive fail count before unhealthy."
}

variable "request_interval" {
  type        = number
  default     = 30
  description = "Health check interval (sec) — Route53는 10 또는 30만 허용."
}

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Resource tags."
}
