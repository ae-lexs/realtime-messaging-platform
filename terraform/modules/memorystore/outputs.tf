output "host" {
  description = "Private IP of the Redis instance."
  value       = google_redis_instance.main.host
}

output "port" {
  description = "Port the Redis instance listens on."
  value       = google_redis_instance.main.port
}

output "address" {
  description = "host:port, the form internal/redis takes as REDIS_ADDR."
  value       = "${google_redis_instance.main.host}:${google_redis_instance.main.port}"
}

output "auth_string" {
  description = "Generated AUTH string, empty when auth_enabled is false. The deploy script reads it into a Kubernetes secret; it is never baked into an image or a manifest."
  value       = google_redis_instance.main.auth_string
  sensitive   = true
}

output "instance_name" {
  description = "The Redis instance name."
  value       = google_redis_instance.main.name
}
