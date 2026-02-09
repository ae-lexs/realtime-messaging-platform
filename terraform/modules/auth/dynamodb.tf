# DynamoDB auth tables — users, sessions, otp_requests
#
# Implements TBD-TF1-2. All tables use On-Demand capacity, PITR, and
# AWS-managed SSE. Deletion protection is environment-gated.

locals {
  name = "${var.project_name}-${var.environment}"
}

# -----------------------------------------------------------------------------
# users — PK: user_id, GSI: phone_number-index (KEYS_ONLY)
# ADR-007 §2.4
# -----------------------------------------------------------------------------

resource "aws_dynamodb_table" "users" {
  name         = "${local.name}-users"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "user_id"
  table_class  = "STANDARD"

  deletion_protection_enabled = var.enable_deletion_protection

  attribute {
    name = "user_id"
    type = "S"
  }

  attribute {
    name = "phone_number"
    type = "S"
  }

  global_secondary_index {
    name            = "phone_number-index"
    hash_key        = "phone_number"
    projection_type = "KEYS_ONLY"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = {
    Name = "${local.name}-users"
  }
}

# -----------------------------------------------------------------------------
# sessions — PK: session_id, GSI: user_sessions-index (ALL), TTL on ttl
# ADR-007 §2.8
# -----------------------------------------------------------------------------

resource "aws_dynamodb_table" "sessions" {
  name         = "${local.name}-sessions"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "session_id"
  table_class  = "STANDARD"

  deletion_protection_enabled = var.enable_deletion_protection

  attribute {
    name = "session_id"
    type = "S"
  }

  attribute {
    name = "user_id"
    type = "S"
  }

  global_secondary_index {
    name            = "user_sessions-index"
    hash_key        = "user_id"
    projection_type = "ALL"
  }

  ttl {
    attribute_name = "ttl"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = {
    Name = "${local.name}-sessions"
  }
}

# -----------------------------------------------------------------------------
# otp_requests — PK: phone_hash, TTL on ttl, no GSI
# ADR-015 Appendix A
# -----------------------------------------------------------------------------

resource "aws_dynamodb_table" "otp_requests" {
  name         = "${local.name}-otp-requests"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "phone_hash"
  table_class  = "STANDARD"

  deletion_protection_enabled = var.enable_deletion_protection

  attribute {
    name = "phone_hash"
    type = "S"
  }

  ttl {
    attribute_name = "ttl"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = {
    Name = "${local.name}-otp-requests"
  }
}
