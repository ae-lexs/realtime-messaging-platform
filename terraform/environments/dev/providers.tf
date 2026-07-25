# Provider configured only in the root module (never in child modules), with
# default_labels as the GCP analogue of the AWS provider's default_tags.
provider "google" {
  project = var.project_id
  region  = var.region

  default_labels = {
    project     = var.project_name
    environment = var.environment
    managed-by  = "terraform"
  }
}
