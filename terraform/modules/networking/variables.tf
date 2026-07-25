# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns the network resources."
  type        = string
}

variable "region" {
  description = "Region for the subnet, Cloud Router, and Cloud NAT."
  type        = string
}

variable "name_prefix" {
  description = "Prefix for cloud resource names (e.g. messaging-dev)."
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "subnet_cidr" {
  description = "Primary IPv4 CIDR range for the GKE node subnet."
  type        = string
  default     = "10.10.0.0/20"
}

variable "pods_range_name" {
  description = "Name of the secondary range used for GKE pod IPs (VPC-native)."
  type        = string
  default     = "pods"
}

variable "pods_cidr" {
  description = "Secondary IPv4 CIDR range for GKE pod IPs."
  type        = string
  default     = "10.20.0.0/16"
}

variable "services_range_name" {
  description = "Name of the secondary range used for GKE service (ClusterIP) IPs."
  type        = string
  default     = "services"
}

variable "services_cidr" {
  description = "Secondary IPv4 CIDR range for GKE service (ClusterIP) IPs."
  type        = string
  default     = "10.30.0.0/20"
}
