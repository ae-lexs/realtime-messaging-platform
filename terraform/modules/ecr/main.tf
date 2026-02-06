# ECR module — container image repositories with lifecycle policies
#
# Implements TBD-TF0-6 (ECR Lifecycle & Image Strategy).
# Creates one repository per service with immutable tags, scan-on-push,
# and lifecycle policies for cost control.

locals {
  repositories = { for name in var.service_names : name => "${var.project_name}-${name}" }
}

resource "aws_ecr_repository" "service" {
  for_each = local.repositories

  name                 = each.value
  image_tag_mutability = var.image_tag_mutability
  force_delete         = false

  image_scanning_configuration {
    scan_on_push = var.enable_scan_on_push
  }

  encryption_configuration {
    encryption_type = "AES256"
  }

  tags = {
    Name    = each.value
    Service = each.key
  }
}

resource "aws_ecr_lifecycle_policy" "service" {
  for_each = aws_ecr_repository.service

  repository = each.value.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images after ${var.untagged_image_expiry_days} days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = var.untagged_image_expiry_days
        }
        action = {
          type = "expire"
        }
      },
      {
        rulePriority = 2
        description  = "Keep last ${var.max_tagged_image_count} tagged images"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = ["v", "sha-"]
          countType     = "imageCountMoreThan"
          countNumber   = var.max_tagged_image_count
        }
        action = {
          type = "expire"
        }
      }
    ]
  })
}
