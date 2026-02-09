# Secrets Manager and SSM parameters for auth — TBD-TF1-4, TBD-TF1-5
#
# Terraform manages:
# - OTP pepper secret container (value managed by operational script)
# - JWT cache TTL SSM parameter (fully managed)
#
# Script-managed (NOT in Terraform):
# - jwt/signing-key/{KEY_ID} secrets
# - /messaging/jwt/current-key-id SSM parameter
# - /messaging/jwt/public-keys/{KEY_ID} SSM parameters

# -----------------------------------------------------------------------------
# Secrets Manager — OTP Pepper
# TBD-TF1-4: Container managed by Terraform, value by operational script.
# -----------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "otp_pepper" {
  name                    = "${local.name}/otp/pepper"
  description             = "OTP HMAC pepper for phone number hashing (ADR-015 §1.1)"
  kms_key_id              = aws_kms_key.auth_secrets.arn
  recovery_window_in_days = var.secret_recovery_window_days

  tags = {
    Name = "${local.name}-otp-pepper"
  }
}

resource "aws_secretsmanager_secret_version" "otp_pepper" {
  secret_id     = aws_secretsmanager_secret.otp_pepper.id
  secret_string = "PLACEHOLDER_REPLACE_VIA_CLI"

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# -----------------------------------------------------------------------------
# SSM Parameter Store — JWT cache TTL
# TBD-TF1-5: Fully managed by Terraform.
# -----------------------------------------------------------------------------

resource "aws_ssm_parameter" "jwt_cache_ttl" {
  name        = "/messaging/jwt/cache-ttl-seconds"
  description = "JWT public key cache refresh interval in seconds"
  type        = "String"
  value       = "300"

  tags = {
    Name = "${local.name}-jwt-cache-ttl"
  }
}
