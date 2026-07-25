locals {
  # Cloud resource name prefix: {project}-{environment} (TERRAFORM.md naming).
  name_prefix = "messaging-${var.environment}"

  # Default compute service account = the Autopilot node identity that pulls images.
  compute_sa = "serviceAccount:${data.google_project.this.number}-compute@developer.gserviceaccount.com"
}

data "google_project" "this" {
  project_id = var.project_id
}

# Enable required APIs first; every other module depends on this.
module "project_services" {
  source     = "../../modules/project-services"
  project_id = var.project_id
}

module "networking" {
  source      = "../../modules/networking"
  project_id  = var.project_id
  region      = var.region
  name_prefix = local.name_prefix

  depends_on = [module.project_services]
}

module "gke" {
  source                        = "../../modules/gke"
  project_id                    = var.project_id
  region                        = var.region
  name_prefix                   = local.name_prefix
  network_id                    = module.networking.network_id
  subnet_id                     = module.networking.subnet_id
  pods_range_name               = module.networking.pods_range_name
  services_range_name           = module.networking.services_range_name
  master_authorized_cidr_blocks = var.master_authorized_cidr_blocks

  depends_on = [module.project_services]
}

module "artifact_registry" {
  source         = "../../modules/artifact-registry"
  project_id     = var.project_id
  region         = var.region
  repository_id  = var.artifact_repository_id
  reader_members = [local.compute_sa]

  depends_on = [module.project_services]
}

module "budget" {
  source             = "../../modules/budget"
  project_id         = var.project_id
  billing_account_id = var.billing_account_id
  name_prefix        = local.name_prefix
  amount_units       = var.budget_amount_units

  depends_on = [module.project_services]
}
