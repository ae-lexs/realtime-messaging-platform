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

variable "private_service_access_prefix_length" {
  description = "Prefix length of the range reserved for Private Services Access. Google allocates the address itself; /16 is their recommended size and leaves room for Memorystore (M1.2) and Cloud SQL (M2.1) to coexist without a second reservation."
  type        = number
  default     = 16

  validation {
    condition     = var.private_service_access_prefix_length >= 16 && var.private_service_access_prefix_length <= 24
    error_message = "Private Services Access requires a prefix length between /16 and /24."
  }
}
