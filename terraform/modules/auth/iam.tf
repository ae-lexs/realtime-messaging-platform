# IAM roles and policies for ECS services — TBD-TF1-7
#
# - 1 shared execution role (ECR pull + CloudWatch Logs)
# - 4 task roles (chatmgmt, gateway, ingest, fanout) with least-privilege auth policies
# - All trust policies include confused deputy prevention conditions
#
# Additive policy composition: TF-2 will add separate inline policies
# (e.g., "messaging-policy") to these roles without modifying auth policies.

# -----------------------------------------------------------------------------
# Shared ECS Execution Role
# ECR pull + CloudWatch Logs — identical for all 4 services.
# -----------------------------------------------------------------------------

resource "aws_iam_role" "ecs_execution" {
  name = "${local.name}-ecs-execution"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnLike = {
            "aws:SourceArn" = "arn:aws:ecs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:*"
          }
        }
      },
    ]
  })

  tags = {
    Name = "${local.name}-ecs-execution"
  }
}

resource "aws_iam_role_policy" "ecs_execution" {
  name = "execution-policy"
  role = aws_iam_role.ecs_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "ECRAuth"
        Effect = "Allow"
        Action = [
          "ecr:GetAuthorizationToken",
        ]
        Resource = "*"
      },
      {
        Sid    = "ECRPull"
        Effect = "Allow"
        Action = [
          "ecr:BatchGetImage",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchCheckLayerAvailability",
        ]
        Resource = "arn:aws:ecr:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:repository/${var.project_name}-*"
      },
      {
        Sid    = "CloudWatchLogs"
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ]
        Resource = "arn:aws:logs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:log-group:/ecs/${var.project_name}-*:*"
      },
    ]
  })
}

# -----------------------------------------------------------------------------
# Chat Mgmt Task Role — full auth permissions
# DynamoDB (all 3 tables + indexes), Secrets Manager, KMS, SSM
# -----------------------------------------------------------------------------

resource "aws_iam_role" "chatmgmt_task" {
  name = "${local.name}-chatmgmt-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnLike = {
            "aws:SourceArn" = "arn:aws:ecs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:*"
          }
        }
      },
    ]
  })

  tags = {
    Name = "${local.name}-chatmgmt-task"
  }
}

resource "aws_iam_role_policy" "chatmgmt_auth" {
  name = "auth-policy"
  role = aws_iam_role.chatmgmt_task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "DynamoDB"
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem",
          "dynamodb:Query",
        ]
        Resource = [
          aws_dynamodb_table.users.arn,
          "${aws_dynamodb_table.users.arn}/index/*",
          aws_dynamodb_table.sessions.arn,
          "${aws_dynamodb_table.sessions.arn}/index/*",
          aws_dynamodb_table.otp_requests.arn,
        ]
      },
      {
        Sid    = "SecretsManager"
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
        ]
        Resource = [
          "${aws_secretsmanager_secret.otp_pepper.arn}-*",
          "arn:aws:secretsmanager:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:secret:jwt/signing-key/*",
        ]
      },
      {
        Sid    = "KMSOTPEncryption"
        Effect = "Allow"
        Action = [
          "kms:Encrypt",
          "kms:Decrypt",
          "kms:DescribeKey",
        ]
        Resource = [
          aws_kms_key.otp_encryption.arn,
        ]
      },
      {
        Sid    = "KMSAuthSecrets"
        Effect = "Allow"
        Action = [
          "kms:Decrypt",
          "kms:DescribeKey",
        ]
        Resource = [
          aws_kms_key.auth_secrets.arn,
        ]
      },
      {
        Sid    = "SSM"
        Effect = "Allow"
        Action = [
          "ssm:GetParameter",
          "ssm:GetParametersByPath",
        ]
        Resource = "arn:aws:ssm:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:parameter/messaging/jwt/*"
      },
    ]
  })
}

# -----------------------------------------------------------------------------
# Gateway Task Role — JWT validation only (SSM)
# -----------------------------------------------------------------------------

resource "aws_iam_role" "gateway_task" {
  name = "${local.name}-gateway-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnLike = {
            "aws:SourceArn" = "arn:aws:ecs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:*"
          }
        }
      },
    ]
  })

  tags = {
    Name = "${local.name}-gateway-task"
  }
}

resource "aws_iam_role_policy" "gateway_auth" {
  name = "auth-policy"
  role = aws_iam_role.gateway_task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "SSM"
        Effect = "Allow"
        Action = [
          "ssm:GetParameter",
          "ssm:GetParametersByPath",
        ]
        Resource = "arn:aws:ssm:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:parameter/messaging/jwt/*"
      },
    ]
  })
}

# -----------------------------------------------------------------------------
# Ingest Task Role — JWT validation only (SSM)
# -----------------------------------------------------------------------------

resource "aws_iam_role" "ingest_task" {
  name = "${local.name}-ingest-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnLike = {
            "aws:SourceArn" = "arn:aws:ecs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:*"
          }
        }
      },
    ]
  })

  tags = {
    Name = "${local.name}-ingest-task"
  }
}

resource "aws_iam_role_policy" "ingest_auth" {
  name = "auth-policy"
  role = aws_iam_role.ingest_task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "SSM"
        Effect = "Allow"
        Action = [
          "ssm:GetParameter",
          "ssm:GetParametersByPath",
        ]
        Resource = "arn:aws:ssm:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:parameter/messaging/jwt/*"
      },
    ]
  })
}

# -----------------------------------------------------------------------------
# Fanout Task Role — no auth permissions
# Fanout trusts Kafka events, not identity (ADR-002 plane separation).
# TF-2 may add MSK/DynamoDB permissions for messaging tables.
# -----------------------------------------------------------------------------

resource "aws_iam_role" "fanout_task" {
  name = "${local.name}-fanout-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
          ArnLike = {
            "aws:SourceArn" = "arn:aws:ecs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:*"
          }
        }
      },
    ]
  })

  tags = {
    Name = "${local.name}-fanout-task"
  }
}
