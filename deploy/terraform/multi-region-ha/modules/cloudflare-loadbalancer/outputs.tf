output "load_balancer_id" {
  value       = cloudflare_load_balancer.this.id
  description = "Cloudflare Load Balancer resource ID — Cloudflare API 호출용."
}

output "load_balancer_hostname" {
  value       = cloudflare_load_balancer.this.name
  description = "Customer-facing hostname (CNAME 결선 대상)."
}

output "primary_pool_id" {
  value       = cloudflare_load_balancer_pool.primary.id
  description = "Primary region pool ID."
}

output "secondary_pool_id" {
  value       = cloudflare_load_balancer_pool.secondary.id
  description = "Secondary region pool ID."
}

output "monitor_id" {
  value       = cloudflare_load_balancer_monitor.primary.id
  description = "Shared health monitor ID — Primary + Secondary 양쪽 pool이 참조."
}
