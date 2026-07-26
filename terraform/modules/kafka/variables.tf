# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns the Kafka cluster."
  type        = string
}

variable "region" {
  description = "Region (location) for the Kafka cluster. The schema registry must be created in this same region."
  type        = string
}

variable "name_prefix" {
  description = "Resource name prefix, {project}-{environment}; the cluster is named {prefix}-kafka."
  type        = string
}

variable "subnet_id" {
  description = "Subnet the cluster attaches to, as projects/{project}/regions/{region}/subnetworks/{name}."
  type        = string

  validation {
    condition     = can(regex("^projects/[^/]+/regions/[^/]+/subnetworks/[^/]+$", var.subnet_id))
    error_message = "subnet_id must be a fully-qualified subnetwork path: projects/{project}/regions/{region}/subnetworks/{name}."
  }
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "vcpu_count" {
  description = "vCPUs to provision. 3 is the service minimum and what the lab uses — capacity is fixed, not autoscaled."
  type        = number
  default     = 3

  validation {
    condition     = var.vcpu_count >= 3
    error_message = "Managed Service for Apache Kafka requires at least 3 vCPUs."
  }
}

variable "memory_gib" {
  description = "Memory to provision, in GiB. Must be between 1 and 8 GiB per vCPU; the default is the 1 GiB-per-vCPU floor."
  type        = number
  default     = 3

  validation {
    condition     = var.memory_gib >= var.vcpu_count && var.memory_gib <= var.vcpu_count * 8
    error_message = "memory_gib must be between 1 and 8 GiB per vCPU (i.e. between vcpu_count and 8 * vcpu_count)."
  }
}
