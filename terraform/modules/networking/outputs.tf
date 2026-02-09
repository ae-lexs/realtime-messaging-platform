# VPC

output "vpc_id" {
  description = "ID of the VPC"
  value       = aws_vpc.main.id
}

output "vpc_cidr" {
  description = "CIDR block of the VPC"
  value       = aws_vpc.main.cidr_block
}

# Subnets

output "public_subnet_ids" {
  description = "IDs of the public subnets"
  value       = [for s in aws_subnet.public : s.id]
}

output "private_subnet_ids" {
  description = "IDs of the private subnets"
  value       = [for s in aws_subnet.private : s.id]
}

# Security Groups

output "alb_security_group_id" {
  description = "Security group ID for the ALB"
  value       = aws_security_group.alb.id
}

output "gateway_security_group_id" {
  description = "Security group ID for the Gateway service"
  value       = aws_security_group.gateway.id
}

output "ingest_security_group_id" {
  description = "Security group ID for the Ingest service"
  value       = aws_security_group.ingest.id
}

output "fanout_security_group_id" {
  description = "Security group ID for the Fanout worker"
  value       = aws_security_group.fanout.id
}

output "chatmgmt_security_group_id" {
  description = "Security group ID for the Chat Mgmt service"
  value       = aws_security_group.chatmgmt.id
}

output "redis_security_group_id" {
  description = "Security group ID for the Redis cluster"
  value       = aws_security_group.redis.id
}

output "msk_security_group_id" {
  description = "Security group ID for MSK (empty shell — populated in TF-2)"
  value       = aws_security_group.msk.id
}

# VPC Endpoints

output "s3_endpoint_id" {
  description = "ID of the S3 VPC Gateway Endpoint"
  value       = aws_vpc_endpoint.s3.id
}

output "dynamodb_endpoint_id" {
  description = "ID of the DynamoDB VPC Gateway Endpoint"
  value       = aws_vpc_endpoint.dynamodb.id
}

# VPC Interface Endpoints (TBD-TF1-6)

output "vpc_endpoints_security_group_id" {
  description = "Security group ID for VPC Interface Endpoints (null when disabled)"
  value       = var.enable_vpc_interface_endpoints ? aws_security_group.vpc_endpoints[0].id : null
}

output "interface_endpoint_ids" {
  description = "Map of service name to VPC Interface Endpoint ID (empty when disabled)"
  value       = { for k, ep in aws_vpc_endpoint.interface : k => ep.id }
}

# NAT Gateway

output "nat_gateway_ids" {
  description = "IDs of the NAT Gateway(s)"
  value       = [for ng in aws_nat_gateway.main : ng.id]
}
