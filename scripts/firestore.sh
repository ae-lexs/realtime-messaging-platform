#!/usr/bin/env bash
# Run the Firestore CRUD round-trip (the M1.1 gate) against the dev database.
#
# Firestore is Terraform-managed, unlike the schema registry, so there is
# nothing to create here — the database must already exist:
#
#   PROJECT_ID=my-project make deploy          # or a targeted apply of module.firestore
#   PROJECT_ID=my-project make firestore-test
#
# The toolbox carries the gcloud Application Default Credentials the Go client
# reads; there is no emulator (ADR-021 Axis F).
#
# Usage:
#   PROJECT_ID=my-project [REGION=us-central1] [FIRESTORE_DATABASE=messaging-dev] \
#     scripts/firestore.sh
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-us-central1}"
DATABASE="${FIRESTORE_DATABASE:-messaging-dev}"

echo "==> Firestore round-trip against ${PROJECT_ID}/${DATABASE} (${REGION})"
docker compose run --rm -T \
  -e FIRESTORE_PROJECT="${PROJECT_ID}" \
  -e FIRESTORE_DATABASE="${DATABASE}" \
  toolbox go test -race -tags=integration -v ./internal/firestore/...
