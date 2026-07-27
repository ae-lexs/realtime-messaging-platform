# Custom-mode VPC for the platform. Auto-created subnets are disabled so the
# single regional subnet below is the only one, with explicit secondary ranges
# for VPC-native GKE (pods + services).
resource "google_compute_network" "main" {
  name                    = "${var.name_prefix}-vpc"
  project                 = var.project_id
  auto_create_subnetworks = false
  routing_mode            = "REGIONAL"
}

resource "google_compute_subnetwork" "main" {
  name          = "${var.name_prefix}-subnet"
  project       = var.project_id
  region        = var.region
  network       = google_compute_network.main.id
  ip_cidr_range = var.subnet_cidr

  # Secondary ranges consumed by the Autopilot cluster (ip_allocation_policy).
  secondary_ip_range {
    range_name    = var.pods_range_name
    ip_cidr_range = var.pods_cidr
  }

  secondary_ip_range {
    range_name    = var.services_range_name
    ip_cidr_range = var.services_cidr
  }

  # Lets nodes reach Google APIs over internal IPs without external addresses.
  private_ip_google_access = true
}

# Cloud Router + NAT give pods egress to the internet (image pulls, module
# downloads) without assigning external IPs to nodes.
resource "google_compute_router" "main" {
  name    = "${var.name_prefix}-router"
  project = var.project_id
  region  = var.region
  network = google_compute_network.main.id
}

resource "google_compute_router_nat" "main" {
  name                               = "${var.name_prefix}-nat"
  project                            = var.project_id
  region                             = var.region
  router                             = google_compute_router.main.name
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"

  log_config {
    enable = false
    filter = "ERRORS_ONLY"
  }
}

# Private Services Access — the address range Google's managed services peer
# their own VPC into. Memorystore uses it (M1.2) and Cloud SQL will (M2.1), so
# it lives here rather than in either consumer's module.
#
# Why this rather than Memorystore's default DIRECT_PEERING: with direct
# peering, the GKE *pod* secondary range is not advertised across the peering,
# so traffic reaches Redis and the reply has nowhere to go. The documented fix
# is to masquerade pod IPs behind node IPs with a custom ip-masq-agent
# config — which Autopilot does not let us install. Private Services Access
# does not have the problem, and Cloud SQL needs the range regardless.
resource "google_compute_global_address" "private_service_access" {
  name          = "${var.name_prefix}-psa"
  project       = var.project_id
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = var.private_service_access_prefix_length
  network       = google_compute_network.main.id
}

# The peering itself. This resource is the one to watch on the first teardown:
# the connection cannot be deleted while a producer instance still holds an
# address in the range, and Memorystore's deletion is asynchronous, so the
# range can stay held for a short while after the instance reports gone.
# Terraform destroys the Redis instance first (it depends on this connection),
# which is the ordering that makes the delete possible at all.
resource "google_service_networking_connection" "private_service_access" {
  network                 = google_compute_network.main.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_service_access.name]
}
