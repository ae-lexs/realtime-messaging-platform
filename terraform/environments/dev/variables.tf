# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns all dev resources (pre-existing)."
  type        = string
}

variable "billing_account_id" {
  description = "Billing account ID (XXXXXX-XXXXXX-XXXXXX) for the budget alert."
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "region" {
  description = "GCP region for all regional resources."
  type        = string
  default     = "us-central1"
}

variable "project_name" {
  description = "Project label value for cost allocation (default_labels)."
  type        = string
  default     = "messaging-platform"
}

variable "environment" {
  description = "Deployment environment."
  type        = string
  default     = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Environment must be dev, staging, or prod."
  }
}

variable "artifact_repository_id" {
  description = "Artifact Registry repository ID for service images."
  type        = string
  default     = "messaging"
}

variable "budget_amount_units" {
  description = "Budget alert threshold in whole USD (ADR-021 Deployment Req 4)."
  type        = number
  default     = 500
}

variable "master_authorized_cidr_blocks" {
  description = "CIDR blocks allowed to reach the GKE control plane. The apply script auto-fills the operator's public IP/32."
  type = list(object({
    cidr_block   = string
    display_name = string
  }))
  default = []
}
