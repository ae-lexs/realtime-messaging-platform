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

output "router_name" {
  description = "The Cloud Router name backing egress NAT."
  value       = google_compute_router.main.name
}

output "nat_name" {
  description = "The Cloud NAT name providing pod egress."
  value       = google_compute_router_nat.main.name
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

output "private_service_access_range_name" {
  description = "Name of the reserved range for Private Services Access; managed services take reserved_ip_range from it."
  value       = google_compute_global_address.private_service_access.name
}

# Taking reserved_ip_range alone is not enough to order a consumer correctly:
# that is a dependency on the address, which exists before the peering does, so
# an instance created against the range alone fails with "no matching peering".
# depends_on cannot reference an output, so consumers draw the edge on the
# module — depends_on = [module.networking] — and this output stands as the
# connection's identifier rather than as the thing depended upon.
output "private_service_access_connection_id" {
  description = "The service networking connection ID. Consumers order themselves after the peering with depends_on = [module.networking], since depends_on cannot reference an output."
  value       = google_service_networking_connection.private_service_access.id
}
