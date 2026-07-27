# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID in which to enable the APIs."
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "services" {
  description = "Google Cloud APIs to enable for the platform."
  type        = list(string)
  default = [
    "compute.googleapis.com",
    "container.googleapis.com",
    "artifactregistry.googleapis.com",
    "managedkafka.googleapis.com",
    "firestore.googleapis.com",
    "redis.googleapis.com",
    "secretmanager.googleapis.com",
    # Required for the Private Services Access peering the networking module
    # reserves and Memorystore (M1.2) and Cloud SQL (M2.1) connect over.
    "servicenetworking.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "billingbudgets.googleapis.com",
    "iam.googleapis.com",
    "logging.googleapis.com",
    "monitoring.googleapis.com",
  ]
}
