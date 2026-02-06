# Prod environment root module — composes TF-0 foundation modules.
#
# Prod-specific settings:
# - Per-AZ NAT Gateway (eliminates cross-AZ SPOF)
# - Fargate only (no Spot — unacceptable interruption risk)
# - ECS Exec disabled (no interactive shell in production)
# - Deletion protection on
# - 90-day log retention

# -----------------------------------------------------------------------------
# Networking
# -----------------------------------------------------------------------------

module "networking" {
  source = "../../modules/networking"

  project_name       = var.project_name
  environment        = var.environment
  single_nat_gateway = false
}

# -----------------------------------------------------------------------------
# DNS
# -----------------------------------------------------------------------------

module "dns" {
  source = "../../modules/dns"

  project_name = var.project_name
  environment  = var.environment
  domain_name  = var.domain_name
}

# -----------------------------------------------------------------------------
# ECR
# -----------------------------------------------------------------------------

module "ecr" {
  source = "../../modules/ecr"

  project_name = var.project_name
}

# -----------------------------------------------------------------------------
# ECS Cluster
# -----------------------------------------------------------------------------

module "ecs_cluster" {
  source = "../../modules/ecs-cluster"

  project_name           = var.project_name
  environment            = var.environment
  enable_fargate_spot = false
  fargate_base        = 2
  fargate_weight         = 3
  log_retention_days     = 90
}

# -----------------------------------------------------------------------------
# ALB
# -----------------------------------------------------------------------------

module "alb" {
  source = "../../modules/alb"

  project_name          = var.project_name
  environment           = var.environment
  vpc_id                = module.networking.vpc_id
  public_subnet_ids     = module.networking.public_subnet_ids
  alb_security_group_id = module.networking.alb_security_group_id
  certificate_arn       = module.dns.certificate_arn

  enable_deletion_protection = true
}
