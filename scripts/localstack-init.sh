#!/bin/bash
# LocalStack initialization script
# Creates DynamoDB tables for development and testing.
# Referenced by docker-compose.yaml

set -e

echo "Initializing LocalStack resources..."

# Wait for LocalStack to be ready
awslocal dynamodb wait table-not-exists --table-name __nonexistent__ 2>/dev/null || true

echo "Creating DynamoDB tables..."

# PR-0: No tables yet (tables created in subsequent PRs as needed)
# PR-1: users, sessions, otp_requests
# PR-3: messages, idempotency_keys, chat_counters, chat_memberships
# PR-4: chats, direct_chat_index
# PR-5: delivery_state

# Placeholder table to verify LocalStack is working
awslocal dynamodb create-table \
    --table-name _health_check \
    --attribute-definitions AttributeName=pk,AttributeType=S \
    --key-schema AttributeName=pk,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST \
    2>/dev/null || echo "Health check table already exists"

echo "LocalStack initialization complete."
