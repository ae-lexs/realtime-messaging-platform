output "alb_id" {
  description = "ID of the Application Load Balancer"
  value       = aws_lb.main.id
}

output "alb_arn" {
  description = "ARN of the Application Load Balancer"
  value       = aws_lb.main.arn
}

output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = aws_lb.main.dns_name
}

output "alb_zone_id" {
  description = "Route 53 zone ID of the ALB (for alias records)"
  value       = aws_lb.main.zone_id
}

output "https_listener_arn" {
  description = "ARN of the HTTPS listener"
  value       = aws_lb_listener.https.arn
}

output "gateway_target_group_arn" {
  description = "ARN of the Gateway target group"
  value       = aws_lb_target_group.gateway.arn
}

output "chatmgmt_target_group_arn" {
  description = "ARN of the Chat Mgmt target group"
  value       = aws_lb_target_group.chatmgmt.arn
}
