# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_id" {
  description = "GCP project ID that owns the service accounts."
  type        = string
}

variable "name_prefix" {
  description = "Resource name prefix, {project}-{environment}. Service account IDs are capped at 30 characters, so a longer prefix than messaging-{env} will not fit."
  type        = string

  validation {
    condition     = length(var.name_prefix) <= 20
    error_message = "name_prefix must be at most 20 characters: the longest suffix added here is -chatmgmt (9), and a service account account_id may not exceed 30."
  }
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "kubernetes_namespace" {
  description = "Namespace of the Kubernetes service accounts being bound. Must match k8s/base/namespace.yaml."
  type        = string
  default     = "messaging"
}

variable "chatmgmt_ksa_name" {
  description = "Name of the ChatMgmt Kubernetes service account. Must match the KSA in k8s/base/chatmgmt.yaml, including its iam.gke.io/gcp-service-account annotation — a mismatch leaves the pod with no GCP identity and no error explaining why."
  type        = string
  default     = "chatmgmt"
}
