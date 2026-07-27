# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns the secrets."
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "signing_key_id" {
  description = "Identifier of the active JWT signing key; it becomes the JWT `kid` header and the suffix of the signing/public secret names. A rotation adds a second key ID rather than replacing this one (ADR-015 §3.2), which nothing exercises before M7."
  type        = string
  default     = "primary"

  validation {
    condition     = can(regex("^[a-zA-Z0-9-_]{1,50}$", var.signing_key_id))
    error_message = "signing_key_id must be 1-50 characters of letters, digits, hyphens or underscores (Secret Manager ID charset)."
  }
}

variable "accessor_members" {
  description = "IAM members granted roles/secretmanager.secretAccessor on the auth secrets, as serviceAccount:{email}."
  type        = list(string)
  default     = []
}

variable "labels" {
  description = "Resource labels for cost allocation."
  type        = map(string)
  default     = {}
}
