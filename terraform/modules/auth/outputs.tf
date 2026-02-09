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
