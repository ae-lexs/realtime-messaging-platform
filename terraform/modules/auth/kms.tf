# KMS customer-managed keys for auth — TBD-TF1-3
#
# Two separate CMKs:
# 1. auth-secrets: encrypts Secrets Manager secrets (JWT signing key, OTP pepper)
# 2. otp-encryption: direct KMS.Encrypt/Decrypt for OTP ciphertexts

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# -----------------------------------------------------------------------------
# Auth Secrets CMK — Secrets Manager encryption
# Key policy: root admin + Secrets Manager service via kms:ViaService
# -----------------------------------------------------------------------------

resource "aws_kms_key" "auth_secrets" {
  description             = "Encrypts auth Secrets Manager secrets (JWT signing key, OTP pepper)"
  key_usage               = "ENCRYPT_DECRYPT"
  enable_key_rotation     = true
  deletion_window_in_days = 30

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "RootAdmin"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "kms:*"
        Resource = "*"
      },
      {
        Sid    = "SecretsManagerAccess"
        Effect = "Allow"
        Principal = {
          AWS = "*"
        }
        Action = [
          "kms:Encrypt",
          "kms:Decrypt",
          "kms:ReEncrypt*",
          "kms:GenerateDataKey*",
          "kms:DescribeKey",
        ]
        Resource = "*"
        Condition = {
          StringEquals = {
            "kms:ViaService"    = "secretsmanager.${data.aws_region.current.region}.amazonaws.com"
            "kms:CallerAccount" = data.aws_caller_identity.current.account_id
          }
        }
      },
    ]
  })

  tags = {
    Name = "${local.name}-auth-secrets"
  }
}

resource "aws_kms_alias" "auth_secrets" {
  name          = "alias/${local.name}-auth-secrets"
  target_key_id = aws_kms_key.auth_secrets.key_id
}

# -----------------------------------------------------------------------------
# OTP Encryption CMK — direct Encrypt/Decrypt for OTP ciphertexts
# Key policy: root admin (Chat Mgmt task role grant added in IAM commit)
# -----------------------------------------------------------------------------

resource "aws_kms_key" "otp_encryption" {
  description             = "Encrypts OTP ciphertexts via direct KMS.Encrypt/Decrypt"
  key_usage               = "ENCRYPT_DECRYPT"
  enable_key_rotation     = true
  deletion_window_in_days = 30

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "RootAdmin"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "kms:*"
        Resource = "*"
      },
    ]
  })

  tags = {
    Name = "${local.name}-otp-encryption"
  }
}

resource "aws_kms_alias" "otp_encryption" {
  name          = "alias/${local.name}-otp-encryption"
  target_key_id = aws_kms_key.otp_encryption.key_id
}
