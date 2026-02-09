# DynamoDB Tables — ARNs

output "users_table_arn" {
  description = "ARN of the users DynamoDB table"
  value       = aws_dynamodb_table.users.arn
}

output "sessions_table_arn" {
  description = "ARN of the sessions DynamoDB table"
  value       = aws_dynamodb_table.sessions.arn
}

output "otp_requests_table_arn" {
  description = "ARN of the otp_requests DynamoDB table"
  value       = aws_dynamodb_table.otp_requests.arn
}

output "users_phone_index_arn" {
  description = "ARN of the users phone_number-index GSI"
  value       = "${aws_dynamodb_table.users.arn}/index/phone_number-index"
}

output "sessions_user_index_arn" {
  description = "ARN of the sessions user_sessions-index GSI"
  value       = "${aws_dynamodb_table.sessions.arn}/index/user_sessions-index"
}

# DynamoDB Tables — Names

output "users_table_name" {
  description = "Name of the users DynamoDB table"
  value       = aws_dynamodb_table.users.name
}

output "sessions_table_name" {
  description = "Name of the sessions DynamoDB table"
  value       = aws_dynamodb_table.sessions.name
}

output "otp_requests_table_name" {
  description = "Name of the otp_requests DynamoDB table"
  value       = aws_dynamodb_table.otp_requests.name
}

# KMS Keys

output "auth_secrets_kms_key_arn" {
  description = "ARN of the auth-secrets KMS CMK (Secrets Manager encryption)"
  value       = aws_kms_key.auth_secrets.arn
}

output "auth_secrets_kms_key_id" {
  description = "ID of the auth-secrets KMS CMK"
  value       = aws_kms_key.auth_secrets.key_id
}

output "auth_secrets_kms_alias_arn" {
  description = "ARN of the auth-secrets KMS alias"
  value       = aws_kms_alias.auth_secrets.arn
}

output "otp_encryption_kms_key_arn" {
  description = "ARN of the otp-encryption KMS CMK (OTP ciphertext operations)"
  value       = aws_kms_key.otp_encryption.arn
}

output "otp_encryption_kms_key_id" {
  description = "ID of the otp-encryption KMS CMK"
  value       = aws_kms_key.otp_encryption.key_id
}

output "otp_encryption_kms_alias_arn" {
  description = "ARN of the otp-encryption KMS alias"
  value       = aws_kms_alias.otp_encryption.arn
}

# Secrets Manager

output "otp_pepper_secret_arn" {
  description = "ARN of the OTP pepper Secrets Manager secret"
  value       = aws_secretsmanager_secret.otp_pepper.arn
}

# SSM Parameters

output "jwt_cache_ttl_parameter_arn" {
  description = "ARN of the JWT cache TTL SSM parameter"
  value       = aws_ssm_parameter.jwt_cache_ttl.arn
}
