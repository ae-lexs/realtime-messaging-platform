# Networking

output "vpc_id" {
  description = "VPC ID"
  value       = module.networking.vpc_id
}

output "private_subnet_ids" {
  description = "Private subnet IDs for ECS task placement"
  value       = module.networking.private_subnet_ids
}

# Security Groups

output "gateway_security_group_id" {
  description = "Security group ID for Gateway tasks"
  value       = module.networking.gateway_security_group_id
}

output "ingest_security_group_id" {
  description = "Security group ID for Ingest tasks"
  value       = module.networking.ingest_security_group_id
}

output "fanout_security_group_id" {
  description = "Security group ID for Fanout tasks"
  value       = module.networking.fanout_security_group_id
}

output "chatmgmt_security_group_id" {
  description = "Security group ID for Chat Mgmt tasks"
  value       = module.networking.chatmgmt_security_group_id
}

output "redis_security_group_id" {
  description = "Security group ID for Redis cluster"
  value       = module.networking.redis_security_group_id
}

output "msk_security_group_id" {
  description = "Security group ID for MSK (empty shell for TF-2)"
  value       = module.networking.msk_security_group_id
}

# DNS

output "zone_id" {
  description = "Route 53 hosted zone ID"
  value       = module.dns.zone_id
}

output "zone_name_servers" {
  description = "Name servers for domain delegation"
  value       = module.dns.zone_name_servers
}

output "certificate_arn" {
  description = "ACM certificate ARN"
  value       = module.dns.certificate_arn
}

# ECR

output "ecr_repository_urls" {
  description = "Map of service name to ECR repository URL"
  value       = module.ecr.repository_urls
}

# ECS

output "ecs_cluster_name" {
  description = "ECS cluster name"
  value       = module.ecs_cluster.cluster_name
}

output "ecs_cluster_arn" {
  description = "ECS cluster ARN"
  value       = module.ecs_cluster.cluster_arn
}

output "service_connect_namespace_arn" {
  description = "Service Connect namespace ARN"
  value       = module.ecs_cluster.service_connect_namespace_arn
}

# DynamoDB Auth Tables

output "users_table_arn" {
  description = "ARN of the users DynamoDB table"
  value       = module.auth.users_table_arn
}

output "sessions_table_arn" {
  description = "ARN of the sessions DynamoDB table"
  value       = module.auth.sessions_table_arn
}

output "otp_requests_table_arn" {
  description = "ARN of the otp_requests DynamoDB table"
  value       = module.auth.otp_requests_table_arn
}

# KMS Keys

output "auth_secrets_kms_key_arn" {
  description = "ARN of the auth-secrets KMS CMK"
  value       = module.auth.auth_secrets_kms_key_arn
}

output "otp_encryption_kms_key_arn" {
  description = "ARN of the otp-encryption KMS CMK"
  value       = module.auth.otp_encryption_kms_key_arn
}

# Secrets Manager

output "otp_pepper_secret_arn" {
  description = "ARN of the OTP pepper Secrets Manager secret"
  value       = module.auth.otp_pepper_secret_arn
}

# SSM Parameters

output "jwt_cache_ttl_parameter_arn" {
  description = "ARN of the JWT cache TTL SSM parameter"
  value       = module.auth.jwt_cache_ttl_parameter_arn
}

# IAM Roles

output "ecs_execution_role_arn" {
  description = "ARN of the shared ECS execution role"
  value       = module.auth.ecs_execution_role_arn
}

output "chatmgmt_task_role_arn" {
  description = "ARN of the Chat Mgmt ECS task role"
  value       = module.auth.chatmgmt_task_role_arn
}

output "gateway_task_role_arn" {
  description = "ARN of the Gateway ECS task role"
  value       = module.auth.gateway_task_role_arn
}

output "ingest_task_role_arn" {
  description = "ARN of the Ingest ECS task role"
  value       = module.auth.ingest_task_role_arn
}

output "fanout_task_role_arn" {
  description = "ARN of the Fanout ECS task role"
  value       = module.auth.fanout_task_role_arn
}

# ALB

output "alb_dns_name" {
  description = "ALB DNS name"
  value       = module.alb.alb_dns_name
}

output "gateway_target_group_arn" {
  description = "Gateway target group ARN (for ECS service)"
  value       = module.alb.gateway_target_group_arn
}

output "chatmgmt_target_group_arn" {
  description = "Chat Mgmt target group ARN (for ECS service)"
  value       = module.alb.chatmgmt_target_group_arn
}
