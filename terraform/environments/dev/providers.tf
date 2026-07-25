# Provider configured only in the root module (never in child modules), with
# default_labels as the GCP analogue of the AWS provider's default_tags.
#
# user_project_override + billing_project make the provider send the project as
# the X-Goog-User-Project header. APIs that require a quota/billing project on
# user-credential ADC — notably billingbudgets.googleapis.com (the budget
# module) — return 403 SERVICE_DISABLED without it, attributed to Google's
# shared default project instead of ours.
provider "google" {
  project               = var.project_id
  region                = var.region
  billing_project       = var.project_id
  user_project_override = true

  default_labels = {
    project     = var.project_name
    environment = var.environment
    managed-by  = "terraform"
  }
}
