# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns the cluster."
  type        = string
}

variable "region" {
  description = "Region for the regional Autopilot cluster."
  type        = string
}

variable "name_prefix" {
  description = "Prefix for cloud resource names (e.g. messaging-dev)."
  type        = string
}

variable "network_id" {
  description = "VPC network self-link/ID the cluster attaches to."
  type        = string
}

variable "subnet_id" {
  description = "Subnet self-link/ID the cluster nodes run in."
  type        = string
}

variable "pods_range_name" {
  description = "Secondary range name for pod IPs (must exist on the subnet)."
  type        = string
}

variable "services_range_name" {
  description = "Secondary range name for service IPs (must exist on the subnet)."
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "release_channel" {
  description = "GKE release channel for the Autopilot cluster."
  type        = string
  default     = "REGULAR"

  validation {
    condition     = contains(["RAPID", "REGULAR", "STABLE"], var.release_channel)
    error_message = "release_channel must be RAPID, REGULAR, or STABLE."
  }
}

variable "master_authorized_cidr_blocks" {
  description = "CIDR blocks allowed to reach the cluster control plane (kubectl). The apply script auto-fills the operator's public IP; empty locks the control plane to Google-internal only."
  type = list(object({
    cidr_block   = string
    display_name = string
  }))
  default = []
}
