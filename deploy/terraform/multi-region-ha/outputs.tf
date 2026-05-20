output "primary_record_fqdn" {
  value       = module.route53_failover.primary_record_fqdn
  description = "Primary failover record FQDN (Route53)."
}

output "secondary_record_fqdn" {
  value       = module.route53_failover.secondary_record_fqdn
  description = "Secondary failover record FQDN."
}

output "primary_health_check_id" {
  value       = module.route53_failover.primary_health_check_id
  description = "Route53 health check ID — `aws route53 get-health-check --health-check-id <ID>`로 monitor."
}

output "secondary_health_check_id" {
  value       = module.route53_failover.secondary_health_check_id
  description = "Secondary region health check ID."
}
