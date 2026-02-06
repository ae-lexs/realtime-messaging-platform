# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_name" {
  description = "Project name used as prefix for repository names"
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "service_names" {
  description = "List of service names for ECR repositories"
  type        = list(string)
  default     = ["gateway", "ingest", "fanout", "chatmgmt"]
}

variable "image_tag_mutability" {
  description = "Tag mutability setting for ECR repositories (IMMUTABLE per ADR-014 §8.1)"
  type        = string
  default     = "IMMUTABLE"

  validation {
    condition     = contains(["IMMUTABLE", "MUTABLE"], var.image_tag_mutability)
    error_message = "Image tag mutability must be IMMUTABLE or MUTABLE."
  }
}

variable "max_tagged_image_count" {
  description = "Maximum number of tagged images to retain per repository"
  type        = number
  default     = 10
}

variable "untagged_image_expiry_days" {
  description = "Days before untagged images are expired"
  type        = number
  default     = 7
}

variable "enable_scan_on_push" {
  description = "Enable image vulnerability scanning on push"
  type        = bool
  default     = true
}
