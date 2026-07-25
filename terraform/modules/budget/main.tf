# Cumulative-spend budget scoped to the project. With no all_updates_rule,
# threshold breaches email the billing account's admins by default — no
# notification channel wiring needed (ADR-021 Deployment Req 4).
resource "google_billing_budget" "main" {
  billing_account = var.billing_account_id
  display_name    = "${var.name_prefix}-budget"

  # The filter requires the project *number* in "projects/{number}" form; the
  # project ID is not accepted here.
  budget_filter {
    projects = ["projects/${var.project_number}"]
  }

  amount {
    specified_amount {
      # currency_code is intentionally omitted so the budget inherits the
      # billing account's currency. Specifying a currency that differs from the
      # account (e.g. USD against an MXN account) is a 400 invalid-argument.
      units = tostring(var.amount_units)
    }
  }

  dynamic "threshold_rules" {
    for_each = var.threshold_percents
    content {
      threshold_percent = threshold_rules.value
      spend_basis       = "CURRENT_SPEND"
    }
  }
}
