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

# IAM Roles — ARNs

output "ecs_execution_role_arn" {
  description = "ARN of the shared ECS execution role"
  value       = aws_iam_role.ecs_execution.arn
}

output "ecs_execution_role_name" {
  description = "Name of the shared ECS execution role"
  value       = aws_iam_role.ecs_execution.name
}

output "chatmgmt_task_role_arn" {
  description = "ARN of the Chat Mgmt ECS task role"
  value       = aws_iam_role.chatmgmt_task.arn
}

output "chatmgmt_task_role_name" {
  description = "Name of the Chat Mgmt ECS task role"
  value       = aws_iam_role.chatmgmt_task.name
}

output "gateway_task_role_arn" {
  description = "ARN of the Gateway ECS task role"
  value       = aws_iam_role.gateway_task.arn
}

output "gateway_task_role_name" {
  description = "Name of the Gateway ECS task role"
  value       = aws_iam_role.gateway_task.name
}

output "ingest_task_role_arn" {
  description = "ARN of the Ingest ECS task role"
  value       = aws_iam_role.ingest_task.arn
}

output "ingest_task_role_name" {
  description = "Name of the Ingest ECS task role"
  value       = aws_iam_role.ingest_task.name
}

output "fanout_task_role_arn" {
  description = "ARN of the Fanout ECS task role"
  value       = aws_iam_role.fanout_task.arn
}

output "fanout_task_role_name" {
  description = "Name of the Fanout ECS task role"
  value       = aws_iam_role.fanout_task.name
}
