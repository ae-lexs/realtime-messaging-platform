# Regional GKE Autopilot cluster running all four services (ADR-021 Decision B).
# Autopilot manages nodes, autoscaling, and security posture; there are no
# node_config / node_pool blocks by design.
# google-beta, not google: managed_opentelemetry_config (GKE Managed
# OpenTelemetry — ADR-012 §amended by ADR-021, the Cloud Trace + Managed
# Service for Prometheus backend) has not been promoted to the GA provider.
resource "google_container_cluster" "main" {
  provider = google-beta

  name     = "${var.name_prefix}-gke"
  project  = var.project_id
  location = var.region

  enable_autopilot = true

  # In-cluster collector at opentelemetry-collector.gke-managed-otel.svc.
  # cluster.local:4318 (OTLP/HTTP only — no gRPC port). Forwards traces to
  # Cloud Trace and metrics to Managed Service for Prometheus; each service's
  # OTEL_ENDPOINT env var (k8s/base/*.yaml) points at that address.
  managed_opentelemetry_config {
    scope = "COLLECTION_AND_INSTRUMENTATION_COMPONENTS"
  }

  network    = var.network_id
  subnetwork = var.subnet_id

  # VPC-native addressing using the subnet's secondary ranges.
  ip_allocation_policy {
    cluster_secondary_range_name  = var.pods_range_name
    services_secondary_range_name = var.services_range_name
  }

  release_channel {
    channel = var.release_channel
  }

  # Restrict control-plane (API server) access to known CIDRs. The apply script
  # injects the operator's current public IP; without an entry, kubectl from the
  # internet is blocked (Google-internal access only).
  master_authorized_networks_config {
    dynamic "cidr_blocks" {
      for_each = var.master_authorized_cidr_blocks
      content {
        cidr_block   = cidr_blocks.value.cidr_block
        display_name = cidr_blocks.value.display_name
      }
    }
  }

  # Deploy-and-destroy lab (ADR-021): `terraform destroy` must never be blocked.
  # The default (true) would make teardown fail — the opposite of what the
  # mandatory end-of-session destroy requires.
  deletion_protection = false
}
