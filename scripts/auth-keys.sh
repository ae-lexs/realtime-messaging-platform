#!/usr/bin/env bash
# Generate the JWT signing key pair and the OTP pepper, and add them as
# versions of the Secret Manager secrets Terraform created (ADR-015 §1.2,
# §3.2 as amended for GCP in Appendix F).
#
# This is the GCP replacement for the retired AWS generate-jwt-keys.sh, and it
# keeps that script's central property: Terraform creates the secret
# containers, this script creates the *versions*. Key material therefore never
# enters Terraform state or a plan file, which is the same split M0.3 used for
# the schema registry.
#
# It is idempotent by design, not by accident: a secret that already holds a
# version is left alone. Re-running it must not mint a new signing key, because
# every access token issued under the old one would stop validating the moment
# the pods refreshed.
#
# Usage:
#   PROJECT_ID=my-project [KEY_ID=primary] scripts/auth-keys.sh
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
KEY_ID="${KEY_ID:-primary}"

SIGNING_SECRET="jwt-signing-key-${KEY_ID}"
PUBLIC_SECRET="jwt-public-key-${KEY_ID}"
CURRENT_SECRET="jwt-current-key-id"
PEPPER_SECRET="otp-pepper"

tb() { docker compose run --rm -T toolbox "$@"; }

# has_version reports whether a secret already holds an enabled version.
has_version() {
  tb gcloud secrets versions list "$1" \
    --project="${PROJECT_ID}" --filter="state=ENABLED" --limit=1 --format="value(name)" \
    2>/dev/null | grep -q .
}

# add_version pipes a payload into a new secret version. --data-file=- keeps
# the material off the process command line, where it would be visible to
# anything that can read /proc.
add_version() {
  local secret="$1"
  tb gcloud secrets versions add "${secret}" --project="${PROJECT_ID}" --data-file=- >/dev/null
  echo "    added a version to ${secret}"
}

echo "==> auth secrets in ${PROJECT_ID} (key ID: ${KEY_ID})"

# ---------------------------------------------------------------------------
# JWT signing key pair
# ---------------------------------------------------------------------------

if has_version "${SIGNING_SECRET}"; then
  echo "    ${SIGNING_SECRET} already has a version — leaving the key pair alone"
else
  PRIVATE_KEY_FILE="$(mktemp)"
  PUBLIC_KEY_FILE="$(mktemp)"

  # Shred on every exit path, including failure. macOS has no shred, so
  # overwrite before unlinking.
  cleanup() {
    if command -v shred >/dev/null 2>&1; then
      shred -u "${PRIVATE_KEY_FILE}" "${PUBLIC_KEY_FILE}" 2>/dev/null || true
    else
      dd if=/dev/urandom of="${PRIVATE_KEY_FILE}" bs=4096 count=1 2>/dev/null || true
      dd if=/dev/urandom of="${PUBLIC_KEY_FILE}" bs=4096 count=1 2>/dev/null || true
      rm -f "${PRIVATE_KEY_FILE}" "${PUBLIC_KEY_FILE}"
    fi
  }
  trap cleanup EXIT

  echo "    generating an RSA-2048 key pair"
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "${PRIVATE_KEY_FILE}" 2>/dev/null
  openssl pkey -in "${PRIVATE_KEY_FILE}" -pubout -out "${PUBLIC_KEY_FILE}" 2>/dev/null

  add_version "${SIGNING_SECRET}" < "${PRIVATE_KEY_FILE}"
  add_version "${PUBLIC_SECRET}" < "${PUBLIC_KEY_FILE}"
fi

# ---------------------------------------------------------------------------
# Active key ID
# ---------------------------------------------------------------------------
# Written unconditionally: it is a pointer, not material, and it is what a
# rotation repoints. Writing it every run also repairs the case where the key
# pair landed and this did not.

printf '%s' "${KEY_ID}" | add_version "${CURRENT_SECRET}"

# ---------------------------------------------------------------------------
# OTP pepper
# ---------------------------------------------------------------------------
# Rotating the pepper invalidates every outstanding OTP, so like the signing
# key it is generated once and then left alone. ADR-015 §1.2 notes that this is
# cheap to rotate deliberately — OTPs live five minutes — but not by accident.

if has_version "${PEPPER_SECRET}"; then
  echo "    ${PEPPER_SECRET} already has a version — leaving it alone"
else
  echo "    generating a 32-byte OTP pepper"
  openssl rand -base64 32 | tr -d '\n' | add_version "${PEPPER_SECRET}"
fi

echo "==> auth secrets ready"
