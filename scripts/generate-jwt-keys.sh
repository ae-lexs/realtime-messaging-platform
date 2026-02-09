#!/usr/bin/env bash
# Generate and provision JWT signing keys and OTP pepper for auth infrastructure.
# This script is idempotent — safe to re-run.
#
# Usage:
#   ./scripts/generate-jwt-keys.sh --env dev --region us-east-2
#   ./scripts/generate-jwt-keys.sh --env prod --region us-east-2 --kms-key-arn arn:aws:kms:...
#
# What it does:
#   1. Generates RSA-2048 key pair via openssl
#   2. Creates Secrets Manager secret at jwt/signing-key/{KEY_ID}
#   3. Creates SSM parameter at /messaging/jwt/public-keys/{KEY_ID}
#   4. Updates SSM /messaging/jwt/current-key-id
#   5. Creates OTP pepper in Secrets Manager if not exists
#   6. Cleans up local key files (shred -u)
#
# Prerequisites:
#   - AWS CLI v2 configured with appropriate credentials
#   - openssl installed
#   - Permissions: secretsmanager:CreateSecret, secretsmanager:PutSecretValue,
#     ssm:PutParameter, ssm:GetParameter, kms:Encrypt, kms:GenerateDataKey

set -euo pipefail

# ---------------------------------------------------------------------------
# Defaults and argument parsing
# ---------------------------------------------------------------------------

ENV=""
REGION=""
KMS_KEY_ARN=""
PROJECT="messaging-platform"

usage() {
  echo "Usage: $0 --env <dev|prod> --region <aws-region> [--kms-key-arn <arn>]"
  echo ""
  echo "Options:"
  echo "  --env          Environment (dev, staging, prod) — REQUIRED"
  echo "  --region       AWS region — REQUIRED"
  echo "  --kms-key-arn  Auth-secrets KMS key ARN for Secrets Manager encryption"
  echo "                 If not provided, looks up alias/${PROJECT}-{env}-auth-secrets"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env)
      ENV="$2"
      shift 2
      ;;
    --region)
      REGION="$2"
      shift 2
      ;;
    --kms-key-arn)
      KMS_KEY_ARN="$2"
      shift 2
      ;;
    -h|--help)
      usage
      ;;
    *)
      echo "Unknown option: $1"
      usage
      ;;
  esac
done

if [[ -z "${ENV}" || -z "${REGION}" ]]; then
  echo "Error: --env and --region are required"
  usage
fi

if [[ ! "${ENV}" =~ ^(dev|staging|prod)$ ]]; then
  echo "Error: --env must be dev, staging, or prod"
  exit 1
fi

PREFIX="${PROJECT}-${ENV}"

# ---------------------------------------------------------------------------
# Resolve KMS key ARN if not provided
# ---------------------------------------------------------------------------

if [[ -z "${KMS_KEY_ARN}" ]]; then
  echo "==> Looking up KMS key alias/${PREFIX}-auth-secrets..."
  KMS_KEY_ARN=$(aws kms describe-key \
    --key-id "alias/${PREFIX}-auth-secrets" \
    --region "${REGION}" \
    --query 'KeyMetadata.Arn' \
    --output text)
  echo "    Found: ${KMS_KEY_ARN}"
fi

# ---------------------------------------------------------------------------
# Generate key ID and RSA-2048 key pair
# ---------------------------------------------------------------------------

KEY_ID=$(date -u +%Y%m%dT%H%M%SZ)-$(openssl rand -hex 4)
PRIVATE_KEY_FILE=$(mktemp /tmp/jwt-private-XXXXXX.pem)
PUBLIC_KEY_FILE=$(mktemp /tmp/jwt-public-XXXXXX.pem)

# Ensure cleanup on exit (even on error)
cleanup() {
  if command -v shred &>/dev/null; then
    shred -u "${PRIVATE_KEY_FILE}" 2>/dev/null || true
    shred -u "${PUBLIC_KEY_FILE}" 2>/dev/null || true
  else
    # macOS doesn't have shred — overwrite then remove
    dd if=/dev/urandom of="${PRIVATE_KEY_FILE}" bs=4096 count=1 2>/dev/null || true
    dd if=/dev/urandom of="${PUBLIC_KEY_FILE}" bs=4096 count=1 2>/dev/null || true
    rm -f "${PRIVATE_KEY_FILE}" "${PUBLIC_KEY_FILE}"
  fi
}
trap cleanup EXIT

echo "==> Generating RSA-2048 key pair (KEY_ID: ${KEY_ID})..."
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "${PRIVATE_KEY_FILE}" 2>/dev/null
openssl pkey -in "${PRIVATE_KEY_FILE}" -pubout -out "${PUBLIC_KEY_FILE}" 2>/dev/null

# ---------------------------------------------------------------------------
# Store private key in Secrets Manager
# ---------------------------------------------------------------------------

SECRET_NAME="jwt/signing-key/${KEY_ID}"
PRIVATE_KEY_PEM=$(cat "${PRIVATE_KEY_FILE}")

echo "==> Creating Secrets Manager secret: ${SECRET_NAME}..."
aws secretsmanager create-secret \
  --name "${SECRET_NAME}" \
  --description "JWT signing key ${KEY_ID} (RSA-2048 private key)" \
  --kms-key-id "${KMS_KEY_ARN}" \
  --secret-string "${PRIVATE_KEY_PEM}" \
  --region "${REGION}" \
  --output text --query 'ARN'

# ---------------------------------------------------------------------------
# Store public key in SSM Parameter Store
# ---------------------------------------------------------------------------

PUBLIC_KEY_PEM=$(cat "${PUBLIC_KEY_FILE}")
SSM_PUBLIC_KEY="/messaging/jwt/public-keys/${KEY_ID}"

echo "==> Creating SSM parameter: ${SSM_PUBLIC_KEY}..."
aws ssm put-parameter \
  --name "${SSM_PUBLIC_KEY}" \
  --description "JWT public key ${KEY_ID} (RSA-2048)" \
  --type "String" \
  --value "${PUBLIC_KEY_PEM}" \
  --region "${REGION}" \
  --overwrite

# ---------------------------------------------------------------------------
# Update current key ID
# ---------------------------------------------------------------------------

SSM_CURRENT_KEY="/messaging/jwt/current-key-id"

echo "==> Updating SSM parameter: ${SSM_CURRENT_KEY} -> ${KEY_ID}..."
aws ssm put-parameter \
  --name "${SSM_CURRENT_KEY}" \
  --description "Active JWT signing key ID" \
  --type "String" \
  --value "${KEY_ID}" \
  --region "${REGION}" \
  --overwrite

# ---------------------------------------------------------------------------
# Initialize OTP pepper if not exists
# ---------------------------------------------------------------------------

OTP_PEPPER_NAME="${PREFIX}/otp/pepper"

echo "==> Checking OTP pepper secret: ${OTP_PEPPER_NAME}..."
CURRENT_VALUE=$(aws secretsmanager get-secret-value \
  --secret-id "${OTP_PEPPER_NAME}" \
  --region "${REGION}" \
  --query 'SecretString' \
  --output text 2>/dev/null || echo "")

if [[ "${CURRENT_VALUE}" == "PLACEHOLDER_REPLACE_VIA_CLI" || -z "${CURRENT_VALUE}" ]]; then
  OTP_PEPPER=$(openssl rand -base64 32)
  echo "    Initializing OTP pepper with random value..."
  aws secretsmanager put-secret-value \
    --secret-id "${OTP_PEPPER_NAME}" \
    --secret-string "${OTP_PEPPER}" \
    --region "${REGION}"
else
  echo "    OTP pepper already initialized — skipping"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo ""
echo "==> Done!"
echo "    KEY_ID:         ${KEY_ID}"
echo "    Private key:    Secrets Manager: ${SECRET_NAME}"
echo "    Public key:     SSM: ${SSM_PUBLIC_KEY}"
echo "    Current key:    SSM: ${SSM_CURRENT_KEY} = ${KEY_ID}"
echo "    OTP pepper:     Secrets Manager: ${OTP_PEPPER_NAME}"
echo ""
echo "    Local key files have been securely deleted."
