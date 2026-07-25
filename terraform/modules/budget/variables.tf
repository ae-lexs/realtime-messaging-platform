# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "billing_account_id" {
  description = "Billing account ID (XXXXXX-XXXXXX-XXXXXX) that owns the budget."
  type        = string
}

variable "project_id" {
  description = "GCP project ID the budget is scoped to."
  type        = string
}

variable "name_prefix" {
  description = "Prefix for the budget display name (e.g. messaging-dev)."
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "amount_units" {
  description = "Budget amount in whole currency units (ADR-021 Deployment Req 4: 500)."
  type        = number
  default     = 500
}

variable "currency_code" {
  description = "ISO 4217 currency code for the budget amount."
  type        = string
  default     = "USD"
}

variable "threshold_percents" {
  description = "Fractions of the budget at which to alert billing admins (0.0-1.0)."
  type        = list(number)
  default     = [0.5, 0.9, 1.0]
}
