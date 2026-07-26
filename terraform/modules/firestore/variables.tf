# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns the Firestore database."
  type        = string
}

variable "region" {
  description = "Firestore location. Immutable after creation — moving regions means a new database."
  type        = string
}

variable "name_prefix" {
  description = "Resource name prefix, {project}-{environment}; used verbatim as the Firestore database ID."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,61}[a-z0-9]$", var.name_prefix))
    error_message = "name_prefix must be 4-63 chars: lowercase letter first, then lowercase letters, digits, or hyphens."
  }
}
