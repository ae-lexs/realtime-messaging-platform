#!/usr/bin/env bash
# Bootstrap Terraform remote state backend (S3 + DynamoDB).
# This script is idempotent — safe to re-run.
#
# Usage: ./scripts/bootstrap-terraform-state.sh
#
# Prerequisites:
#   - AWS CLI configured with appropriate credentials
#   - Permissions: s3:CreateBucket, s3:PutBucketVersioning, s3:PutBucketEncryption,
#     s3:PutPublicAccessBlock, dynamodb:CreateTable

set -euo pipefail

BUCKET="messaging-platform-terraform-state"
TABLE="terraform-locks"
REGION="us-east-2"

echo "==> Bootstrapping Terraform state backend in ${REGION}"

# --- S3 Bucket ---
if aws s3api head-bucket --bucket "${BUCKET}" --region "${REGION}" 2>/dev/null; then
  echo "    S3 bucket '${BUCKET}' already exists — skipping creation"
else
  echo "    Creating S3 bucket '${BUCKET}'..."
  aws s3api create-bucket \
    --bucket "${BUCKET}" \
    --region "${REGION}" \
    --create-bucket-configuration LocationConstraint="${REGION}"
fi

echo "    Enabling versioning on '${BUCKET}'..."
aws s3api put-bucket-versioning \
  --bucket "${BUCKET}" \
  --versioning-configuration Status=Enabled

echo "    Enabling server-side encryption on '${BUCKET}'..."
aws s3api put-bucket-encryption \
  --bucket "${BUCKET}" \
  --server-side-encryption-configuration '{
    "Rules": [
      {
        "ApplyServerSideEncryptionByDefault": {
          "SSEAlgorithm": "AES256"
        },
        "BucketKeyEnabled": true
      }
    ]
  }'

echo "    Blocking public access on '${BUCKET}'..."
aws s3api put-public-access-block \
  --bucket "${BUCKET}" \
  --public-access-block-configuration '{
    "BlockPublicAcls": true,
    "IgnorePublicAcls": true,
    "BlockPublicPolicy": true,
    "RestrictPublicBuckets": true
  }'

# --- DynamoDB Table ---
if aws dynamodb describe-table --table-name "${TABLE}" --region "${REGION}" >/dev/null 2>&1; then
  echo "    DynamoDB table '${TABLE}' already exists — skipping creation"
else
  echo "    Creating DynamoDB table '${TABLE}'..."
  aws dynamodb create-table \
    --table-name "${TABLE}" \
    --region "${REGION}" \
    --attribute-definitions AttributeName=LockID,AttributeType=S \
    --key-schema AttributeName=LockID,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST

  echo "    Waiting for table to become active..."
  aws dynamodb wait table-exists --table-name "${TABLE}" --region "${REGION}"
fi

echo "==> Terraform state backend ready"
echo "    Bucket: s3://${BUCKET}"
echo "    Lock table: ${TABLE}"
echo "    Region: ${REGION}"
