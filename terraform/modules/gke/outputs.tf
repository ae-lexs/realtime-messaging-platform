output "cluster_name" {
  description = "The Autopilot cluster name."
  value       = google_container_cluster.main.name
}

output "cluster_id" {
  description = "The fully-qualified cluster resource ID."
  value       = google_container_cluster.main.id
}

output "location" {
  description = "The cluster location (region)."
  value       = google_container_cluster.main.location
}

output "endpoint" {
  description = "The cluster control-plane endpoint IP."
  value       = google_container_cluster.main.endpoint
  sensitive   = true
}

output "ca_certificate" {
  description = "Base64-encoded cluster CA certificate (for kubeconfig)."
  value       = google_container_cluster.main.master_auth[0].cluster_ca_certificate
  sensitive   = true
}
