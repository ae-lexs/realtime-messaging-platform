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

variable "enable_deletion_protection" {
  description = "Enable deletion protection on DynamoDB tables (off for dev, on for prod)"
  type        = bool
  default     = false
}

variable "secret_recovery_window_days" {
  description = "Number of days Secrets Manager waits before permanently deleting a secret (0 for dev, 30 for prod)"
  type        = number
  default     = 0
}
