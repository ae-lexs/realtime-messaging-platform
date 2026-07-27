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
  description = "IAM members granted roles/secretmanager.secretAccessor, keyed by secret: signing_key, public_key, current_key_id, otp_pepper. Members are serviceAccount:{email}. Keyed rather than flat because a flat list is a cross product — every member would hold every secret, and a service that only validates tokens would be handed the key that signs them (ADR-015 §3.2)."
  type        = map(list(string))
  default     = {}

  validation {
    condition = alltrue([
      for secret in keys(var.accessor_members) :
      contains(["signing_key", "public_key", "current_key_id", "otp_pepper"], secret)
    ])
    error_message = "accessor_members keys must be signing_key, public_key, current_key_id or otp_pepper."
  }
}

variable "labels" {
  description = "Resource labels for cost allocation."
  type        = map(string)
  default     = {}
}
