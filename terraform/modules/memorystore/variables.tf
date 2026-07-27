# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns the Redis instance."
  type        = string
}

variable "region" {
  description = "Region for the Redis instance. Must match the GKE cluster's region."
  type        = string
}

variable "name_prefix" {
  description = "Resource name prefix, {project}-{environment}; the instance is named {prefix}-redis."
  type        = string
}

variable "network_id" {
  description = "VPC network the instance is authorized on, as projects/{project}/global/networks/{name}."
  type        = string
}

variable "reserved_ip_range" {
  description = "Name of the Private Services Access reserved range the instance allocates from (networking module output)."
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "memory_size_gb" {
  description = "Instance memory in GiB. 1 is the BASIC-tier minimum and ample: the keyspace is rate-limit counters and revoked JTIs, all TTL-bounded to at most an hour."
  type        = number
  default     = 1

  validation {
    condition     = var.memory_size_gb >= 1 && var.memory_size_gb <= 300
    error_message = "memory_size_gb must be between 1 and 300."
  }
}

variable "redis_version" {
  description = "Redis engine version. The rate-limit script uses only INCR/EXPIRE and needs nothing newer than 6.x, but 7.x is the current default."
  type        = string
  default     = "REDIS_7_0"
}

variable "auth_enabled" {
  description = "Whether to require an AUTH string. Keep this true: without it, anything with a route into the VPC can read and clear the revocation list."
  type        = bool
  default     = true
}

variable "labels" {
  description = "Resource labels for cost allocation."
  type        = map(string)
  default     = {}
}
