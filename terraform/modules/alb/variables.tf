# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
}

variable "environment" {
  description = "Deployment environment (dev, staging, prod)"
  type        = string

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Environment must be dev, staging, or prod."
  }
}

variable "vpc_id" {
  description = "VPC ID where the ALB will be created"
  type        = string
}

variable "public_subnet_ids" {
  description = "Public subnet IDs for ALB placement"
  type        = list(string)
}

variable "alb_security_group_id" {
  description = "Security group ID for the ALB"
  type        = string
}

variable "certificate_arn" {
  description = "ARN of the ACM certificate for HTTPS listener"
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "idle_timeout_seconds" {
  description = "ALB idle timeout in seconds (3600 for WebSocket per ADR-005)"
  type        = number
  default     = 3600
}

variable "deregistration_delay_seconds" {
  description = "Target group deregistration delay (matches Gateway drain budget)"
  type        = number
  default     = 30
}

variable "enable_deletion_protection" {
  description = "Enable ALB deletion protection (true for prod)"
  type        = bool
  default     = false
}

variable "gateway_port" {
  description = "Port for the Gateway service target group"
  type        = number
  default     = 8080
}

variable "chatmgmt_port" {
  description = "Port for the Chat Mgmt service target group"
  type        = number
  default     = 8083
}

variable "health_check_path" {
  description = "Health check path for target groups"
  type        = string
  default     = "/healthz"
}
