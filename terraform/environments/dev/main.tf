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

module "firestore" {
  source      = "../../modules/firestore"
  project_id  = var.project_id
  region      = var.region
  name_prefix = local.name_prefix

  depends_on = [module.project_services]
}

module "service_accounts" {
  source      = "../../modules/service-accounts"
  project_id  = var.project_id
  name_prefix = local.name_prefix

  depends_on = [module.project_services]
}

# Secret containers only — scripts/auth-keys.sh adds the versions, so no key
# material reaches Terraform state.
module "secrets" {
  source     = "../../modules/secrets"
  project_id = var.project_id

  # ChatMgmt mints tokens, so it reads all four: the private key, the ID naming
  # the active key, the OTP pepper — and the published public key, which is not
  # redundant with the private one. RemoteKeyStore prefers the published copy so
  # that a mismatch between the two fails at startup instead of minting tokens
  # nothing else can verify, and it only falls back to deriving when the secret
  # is *absent*. A PermissionDenied is not an absence: it takes the error branch
  # and the pod refuses to start.
  #
  # Services that only validate tokens — Gateway (M3.2), Ingest (M2.2) — belong
  # under public_key alone, which is what the keyed input exists to express.
  accessor_members = {
    signing_key    = [module.service_accounts.chatmgmt_member]
    public_key     = [module.service_accounts.chatmgmt_member]
    current_key_id = [module.service_accounts.chatmgmt_member]
    otp_pepper     = [module.service_accounts.chatmgmt_member]
  }

  depends_on = [module.project_services]
}

module "memorystore" {
  source            = "../../modules/memorystore"
  project_id        = var.project_id
  region            = var.region
  name_prefix       = local.name_prefix
  network_id        = module.networking.network_id
  reserved_ip_range = module.networking.private_service_access_range_name

  # reserved_ip_range gives an implicit dependency on the *address* only, and
  # the address exists before the peering does — an instance created against the
  # range alone fails with "no matching peering". The connection is what must
  # exist, but depends_on takes resource and module references, not outputs, so
  # the edge is drawn on the module as a whole. It is coarser than the real
  # requirement and correct for the same reason: everything the connection needs
  # is inside it.
  depends_on = [module.networking]
}

module "kafka" {
  source      = "../../modules/kafka"
  project_id  = var.project_id
  region      = var.region
  name_prefix = local.name_prefix
  subnet_id   = module.networking.subnet_id

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
  project_number     = data.google_project.this.number
  billing_account_id = var.billing_account_id
  name_prefix        = local.name_prefix
  amount_units       = var.budget_amount_units

  depends_on = [module.project_services]
}
