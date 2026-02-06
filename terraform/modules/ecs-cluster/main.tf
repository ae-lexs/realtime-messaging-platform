# ECS Cluster module — Fargate cluster, capacity providers, Service Connect
#
# Implements TBD-TF0-7 (ECS Cluster Configuration).
# Creates ECS cluster with Fargate (+ optional Spot), Container Insights,
# Service Connect namespace, and CloudWatch log group.

locals {
  name         = "${var.project_name}-${var.environment}"
  cluster_name = "messaging-${var.environment}"
}

# -----------------------------------------------------------------------------
# ECS Cluster
# -----------------------------------------------------------------------------

resource "aws_ecs_cluster" "main" {
  name = local.cluster_name

  setting {
    name  = "containerInsights"
    value = var.enable_container_insights ? "enabled" : "disabled"
  }

  configuration {
    execute_command_configuration {
      logging = "OVERRIDE"

      log_configuration {
        cloud_watch_log_group_name = aws_cloudwatch_log_group.ecs.name
      }
    }
  }

  tags = {
    Name = local.cluster_name
  }
}

# -----------------------------------------------------------------------------
# Capacity Providers
# -----------------------------------------------------------------------------

resource "aws_ecs_cluster_capacity_providers" "main" {
  cluster_name = aws_ecs_cluster.main.name

  capacity_providers = var.enable_fargate_spot ? ["FARGATE", "FARGATE_SPOT"] : ["FARGATE"]

  default_capacity_provider_strategy {
    capacity_provider = "FARGATE"
    base              = var.fargate_base
    weight            = var.fargate_weight
  }

  dynamic "default_capacity_provider_strategy" {
    for_each = var.enable_fargate_spot ? [1] : []

    content {
      capacity_provider = "FARGATE_SPOT"
      base              = 0
      weight            = var.fargate_spot_weight
    }
  }
}

# -----------------------------------------------------------------------------
# Service Connect Namespace (Cloud Map)
# Per ADR-014 §5.3: messaging.local
# -----------------------------------------------------------------------------

resource "aws_service_discovery_http_namespace" "main" {
  name        = var.service_connect_namespace
  description = "Service Connect namespace for ${local.cluster_name}"

  tags = {
    Name = "${local.name}-namespace"
  }
}

# -----------------------------------------------------------------------------
# CloudWatch Log Group
# -----------------------------------------------------------------------------

resource "aws_cloudwatch_log_group" "ecs" {
  name              = "/ecs/${local.cluster_name}"
  retention_in_days = var.log_retention_days

  tags = {
    Name = "${local.name}-ecs-logs"
  }
}
