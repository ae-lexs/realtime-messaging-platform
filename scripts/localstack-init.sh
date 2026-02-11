#!/bin/bash
# LocalStack initialization script
# Creates DynamoDB tables, Secrets Manager secrets, and SSM parameters
# for development and testing.
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

# --- PR-1: Auth tables ---

# otp_requests: PK=phone_hash, TTL on ttl attribute, On-Demand billing.
awslocal dynamodb create-table \
    --table-name otp_requests \
    --attribute-definitions AttributeName=phone_hash,AttributeType=S \
    --key-schema AttributeName=phone_hash,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST \
    2>/dev/null || echo "otp_requests table already exists"

awslocal dynamodb update-time-to-live \
    --table-name otp_requests \
    --time-to-live-specification Enabled=true,AttributeName=ttl \
    2>/dev/null || true

# users: PK=user_id, GSI phone_number-index (PK=phone_number).
awslocal dynamodb create-table \
    --table-name users \
    --attribute-definitions \
        AttributeName=user_id,AttributeType=S \
        AttributeName=phone_number,AttributeType=S \
    --key-schema AttributeName=user_id,KeyType=HASH \
    --global-secondary-indexes \
        'IndexName=phone_number-index,KeySchema=[{AttributeName=phone_number,KeyType=HASH}],Projection={ProjectionType=KEYS_ONLY},ProvisionedThroughput={ReadCapacityUnits=5,WriteCapacityUnits=5}' \
    --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5 \
    2>/dev/null || echo "users table already exists"

# sessions: PK=session_id, GSI user_sessions-index (PK=user_id), TTL on ttl.
awslocal dynamodb create-table \
    --table-name sessions \
    --attribute-definitions \
        AttributeName=session_id,AttributeType=S \
        AttributeName=user_id,AttributeType=S \
    --key-schema AttributeName=session_id,KeyType=HASH \
    --global-secondary-indexes \
        'IndexName=user_sessions-index,KeySchema=[{AttributeName=user_id,KeyType=HASH}],Projection={ProjectionType=ALL},ProvisionedThroughput={ReadCapacityUnits=5,WriteCapacityUnits=5}' \
    --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5 \
    2>/dev/null || echo "sessions table already exists"

awslocal dynamodb update-time-to-live \
    --table-name sessions \
    --time-to-live-specification Enabled=true,AttributeName=ttl \
    2>/dev/null || true

# --- PR-1: Secrets Manager (JWT signing key for local dev) ---

echo "Creating Secrets Manager secrets..."

# Generate a 2048-bit RSA key pair for local development.
PRIVATE_KEY=$(openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 2>/dev/null)
PUBLIC_KEY=$(echo "$PRIVATE_KEY" | openssl rsa -pubout 2>/dev/null)

awslocal secretsmanager create-secret \
    --name "jwt/signing-key/dev-key-001" \
    --secret-string "$PRIVATE_KEY" \
    2>/dev/null || echo "jwt/signing-key/dev-key-001 already exists"

# --- PR-1: SSM Parameter Store (JWT key metadata) ---

echo "Creating SSM parameters..."

awslocal ssm put-parameter \
    --name "/messaging/jwt/current-key-id" \
    --value "dev-key-001" \
    --type String \
    --overwrite \
    2>/dev/null

awslocal ssm put-parameter \
    --name "/messaging/jwt/public-keys/dev-key-001" \
    --value "$PUBLIC_KEY" \
    --type String \
    --overwrite \
    2>/dev/null

echo "LocalStack initialization complete."
