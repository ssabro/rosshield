output "primary_record_fqdn" {
  value       = aws_route53_record.primary.fqdn
  description = "Primary record FQDN — production traffic 진입점."
}

output "secondary_record_fqdn" {
  value       = aws_route53_record.secondary.fqdn
  description = "Secondary record FQDN — Primary 다운 시 자동 cutover 대상."
}

output "primary_health_check_id" {
  value       = aws_route53_health_check.primary.id
  description = "Primary health check ID — aws route53 get-health-check로 monitor."
}

output "secondary_health_check_id" {
  value       = aws_route53_health_check.secondary.id
  description = "Secondary health check ID."
}

output "primary_health_check_arn" {
  value       = aws_route53_health_check.primary.arn
  description = "Primary health check ARN — CloudWatch alarm 결선용."
}

output "secondary_health_check_arn" {
  value       = aws_route53_health_check.secondary.arn
  description = "Secondary health check ARN."
}
