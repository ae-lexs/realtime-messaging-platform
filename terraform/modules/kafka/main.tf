# Managed Service for Apache Kafka — the event backbone (ADR-011, ADR-021).
#
# The cluster is provisioned in M0.3, ahead of any producer, because GCP will
# not create a schema registry without one: `schema-registries create` in a
# region with no cluster returns
#
#   FAILED_PRECONDITION: At least one Managed Service for Apache Kafka cluster
#   in us-central1 is required to access the Schema Registry service.
#
# The registry itself has no Terraform resource in either Google provider, so
# it is created in scripts/deploy.sh and deleted in scripts/teardown.sh.
#
# Topics are deliberately absent: they belong with the producer that writes
# them (M2.3 messages.persisted, M1.3 entity topics), per the execution plan's
# "nothing deployed without logic behind it" principle.
resource "google_managed_kafka_cluster" "main" {
  project    = var.project_id
  cluster_id = "${var.name_prefix}-kafka"
  location   = var.region

  capacity_config {
    vcpu_count   = var.vcpu_count
    memory_bytes = var.memory_gib * 1024 * 1024 * 1024
  }

  gcp_config {
    access_config {
      network_configs {
        subnet = var.subnet_id
      }
    }
  }

  # Rebalancing only matters on scale-up, and this cluster is fixed-capacity
  # for the lab — an autoscaling substrate would mask the very saturation
  # behaviour the platform exists to observe (the ADR-021 Aurora argument).
  rebalance_config {
    mode = "NO_REBALANCE"
  }
}
