# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
}

variable "environment" {
  description = "Deployment environment (dev, staging, prod)"
  type        = string

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Environment must be dev, staging, or prod."
  }
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "enable_fargate_spot" {
  description = "Enable FARGATE_SPOT capacity provider (dev only per TBD-TF0-7)"
  type        = bool
  default     = false
}

variable "fargate_base" {
  description = "Base count for FARGATE capacity provider (minimum on-demand tasks)"
  type        = number
  default     = 0
}

variable "fargate_weight" {
  description = "Weight for FARGATE capacity provider"
  type        = number
  default     = 1
}

variable "fargate_spot_weight" {
  description = "Weight for FARGATE_SPOT capacity provider (only used when enable_fargate_spot = true)"
  type        = number
  default     = 3
}

variable "enable_container_insights" {
  description = "Enable CloudWatch Container Insights for the cluster"
  type        = bool
  default     = true
}

variable "service_connect_namespace" {
  description = "Service Connect namespace name (per ADR-014 §5.3)"
  type        = string
  default     = "messaging.local"
}

variable "log_retention_days" {
  description = "CloudWatch log group retention in days"
  type        = number
  default     = 30
}
