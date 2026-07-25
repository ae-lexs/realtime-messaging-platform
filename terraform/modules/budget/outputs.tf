output "budget_id" {
  description = "The resource ID of the billing budget."
  value       = google_billing_budget.main.id
}

output "budget_name" {
  description = "The display name of the billing budget."
  value       = google_billing_budget.main.display_name
}
