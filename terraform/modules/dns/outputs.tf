output "zone_id" {
  description = "Route 53 hosted zone ID"
  value       = aws_route53_zone.main.zone_id
}

output "zone_name_servers" {
  description = "Name servers for the hosted zone (delegate from registrar)"
  value       = aws_route53_zone.main.name_servers
}

output "certificate_arn" {
  description = "ARN of the validated ACM certificate"
  value       = aws_acm_certificate.main.arn
}

output "certificate_validation_id" {
  description = "ID of the ACM certificate validation resource (use as depends_on target)"
  value       = aws_acm_certificate_validation.main.id
}
