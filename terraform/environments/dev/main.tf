# Dev environment root module — composes TF-0 foundation modules.
#
# Dev-specific settings:
# - Single NAT Gateway (cost savings)
# - Fargate Spot enabled (70% savings, acceptable interruptions)
# - ECS Exec enabled (debugging)
# - Deletion protection off
# - 30-day log retention

# -----------------------------------------------------------------------------
# Networking
# -----------------------------------------------------------------------------

module "networking" {
  source = "../../modules/networking"

  project_name                   = var.project_name
  environment                    = var.environment
  single_nat_gateway             = true
  enable_vpc_interface_endpoints = true
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

  project_name        = var.project_name
  environment         = var.environment
  enable_fargate_spot = true
  log_retention_days  = 30
}

# -----------------------------------------------------------------------------
# Auth
# -----------------------------------------------------------------------------

module "auth" {
  source = "../../modules/auth"

  project_name                = var.project_name
  environment                 = var.environment
  enable_deletion_protection  = false
  secret_recovery_window_days = 0
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

  enable_deletion_protection = false
}
