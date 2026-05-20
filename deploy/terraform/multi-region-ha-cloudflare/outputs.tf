output "load_balancer_id" {
  value       = module.cloudflare_lb.load_balancer_id
  description = "Cloudflare Load Balancer resource ID."
}

output "load_balancer_hostname" {
  value       = module.cloudflare_lb.load_balancer_hostname
  description = "Customer-facing hostname."
}

output "primary_pool_id" {
  value       = module.cloudflare_lb.primary_pool_id
  description = "Primary region pool ID."
}

output "secondary_pool_id" {
  value       = module.cloudflare_lb.secondary_pool_id
  description = "Secondary region pool ID."
}

output "monitor_id" {
  value       = module.cloudflare_lb.monitor_id
  description = "Shared health monitor ID."
}
