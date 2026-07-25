# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns the Artifact Registry repository."
  type        = string
}

variable "region" {
  description = "Region (location) for the Docker repository."
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "repository_id" {
  description = "Artifact Registry repository ID (the last path segment of image refs)."
  type        = string
  default     = "messaging"
}

variable "description" {
  description = "Human-readable description of the repository."
  type        = string
  default     = "Container images for the realtime messaging platform services."
}
