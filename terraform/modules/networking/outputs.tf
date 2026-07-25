output "network_id" {
  description = "The VPC network self-link/ID, for attaching the GKE cluster."
  value       = google_compute_network.main.id
}

output "network_name" {
  description = "The VPC network name."
  value       = google_compute_network.main.name
}

output "subnet_id" {
  description = "The subnet self-link/ID, for attaching the GKE cluster."
  value       = google_compute_subnetwork.main.id
}

output "subnet_name" {
  description = "The subnet name."
  value       = google_compute_subnetwork.main.name
}

# The secondary_ip_range block is a set (no stable ordering), so these expose
# the configured names directly. The GKE module still gets an implicit
# dependency on the subnet via subnet_id, so ordering is preserved.
output "pods_range_name" {
  description = "Secondary range name for GKE pod IPs (for ip_allocation_policy)."
  value       = var.pods_range_name
}

output "services_range_name" {
  description = "Secondary range name for GKE service IPs (for ip_allocation_policy)."
  value       = var.services_range_name
}
