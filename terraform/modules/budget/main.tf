# Looks up the project number, which the budget filter requires in
# "projects/{number}" form (the project ID is not accepted here).
data "google_project" "this" {
  project_id = var.project_id
}

# Cumulative-spend budget scoped to the project. With no all_updates_rule,
# threshold breaches email the billing account's admins by default — no
# notification channel wiring needed (ADR-021 Deployment Req 4).
resource "google_billing_budget" "main" {
  billing_account = var.billing_account_id
  display_name    = "${var.name_prefix}-budget"

  budget_filter {
    projects = ["projects/${data.google_project.this.number}"]
  }

  amount {
    specified_amount {
      currency_code = var.currency_code
      units         = tostring(var.amount_units)
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
