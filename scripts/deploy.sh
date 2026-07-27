#!/usr/bin/env bash
# Deploy the base infrastructure + four health services to GKE (M0.2).
#
# gcloud/terraform/kubectl run in the toolbox container; docker build/push runs
# on the host daemon (authenticated with a short-lived token from the toolbox).
# Authenticate first with: make gcp-auth, and bootstrap state once with:
# make gcp-bootstrap-state.
#
# This is the first cloud spend (ADR-021 Deployment Req 4). Always tear down at
# end of session with: make teardown.
#
# Usage:
#   PROJECT_ID=my-project BILLING_ACCOUNT_ID=XXXXXX-XXXXXX-XXXXXX \
#     [REGION=us-central1] [IMAGE_TAG=<tag>] scripts/deploy.sh
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
: "${BILLING_ACCOUNT_ID:?set BILLING_ACCOUNT_ID}"
REGION="${REGION:-us-central1}"
BUCKET="${TF_STATE_BUCKET:-${PROJECT_ID}-tf-state}"
TAG="${IMAGE_TAG:-$(git rev-parse --short HEAD)}"
ENV_DIR="terraform/environments/dev"

tb() { docker compose run --rm -T toolbox "$@"; }

# 1. Restrict the GKE control plane to the operator's current public IP.
OPERATOR_IP="$(curl -fsS https://api.ipify.org | tr -d '[:space:]')"
CIDR_VAR="master_authorized_cidr_blocks=[{cidr_block=\"${OPERATOR_IP}/32\",display_name=\"operator\"}]"
echo "==> Control plane will allow ${OPERATOR_IP}/32"

# 2. Provision infrastructure.
echo "==> terraform init + apply"
tb terraform -chdir="${ENV_DIR}" init -input=false -reconfigure \
  -backend-config="bucket=${BUCKET}" -backend-config="prefix=dev"
tb terraform -chdir="${ENV_DIR}" apply -input=false -auto-approve \
  -var="project_id=${PROJECT_ID}" \
  -var="billing_account_id=${BILLING_ACCOUNT_ID}" \
  -var="region=${REGION}" \
  -var="${CIDR_VAR}"

# 3. Resolve outputs and fetch cluster credentials.
AR="$(tb terraform -chdir="${ENV_DIR}" output -raw artifact_registry_url | tr -d '[:space:]')"
CLUSTER="$(tb terraform -chdir="${ENV_DIR}" output -raw cluster_name | tr -d '[:space:]')"
FIRESTORE_DB="$(tb terraform -chdir="${ENV_DIR}" output -raw firestore_database_id | tr -d '[:space:]')"
REDIS_ADDR="$(tb terraform -chdir="${ENV_DIR}" output -raw redis_address | tr -d '[:space:]')"
CHATMGMT_SA="$(tb terraform -chdir="${ENV_DIR}" output -raw chatmgmt_service_account | tr -d '[:space:]')"
KEY_ID="$(tb terraform -chdir="${ENV_DIR}" output -raw jwt_signing_key_id | tr -d '[:space:]')"
echo "==> Artifact Registry: ${AR}"
tb gcloud container clusters get-credentials "${CLUSTER}" --region "${REGION}" --project "${PROJECT_ID}"

# 3b. Fill the auth secrets (M1.2). Terraform created them empty on purpose, so
#     no key material is in state; ChatMgmt refuses to start against an empty
#     signing key (ADR-015 signing_key_required_for_startup), which is why this
#     runs before the rollout rather than after it.
PROJECT_ID="${PROJECT_ID}" KEY_ID="${KEY_ID}" ./scripts/auth-keys.sh

# 4. Build + push the four images (host docker; token from the toolbox).
echo "==> docker login to ${REGION}-docker.pkg.dev"
tb gcloud auth print-access-token | docker login -u oauth2accesstoken --password-stdin "https://${REGION}-docker.pkg.dev"
for svc in gateway ingest fanout chatmgmt; do
  echo "==> build + push ${svc}:${TAG}"
  # GKE nodes are amd64; force the image platform so a build on an arm64 host
  # (Apple Silicon) doesn't publish an arm64-labelled manifest the nodes reject.
  docker build --platform linux/amd64 -f "docker/${svc}.Dockerfile" -t "${AR}/${svc}:${TAG}" .
  docker push "${AR}/${svc}:${TAG}"
done

# 5. Create the namespace and the Memorystore AUTH secret before the workloads.
#    The ChatMgmt pod mounts redis-auth as an env var, so a pod scheduled ahead
#    of the secret would sit in ContainerCreating until it appeared — this just
#    removes the race. The AUTH string goes from a Terraform output straight to
#    a Kubernetes secret and is never written to a manifest or an image.
echo "==> namespace + redis-auth secret"
tb sh -c "kubectl create namespace messaging --dry-run=client -o yaml | kubectl apply -f -"
REDIS_AUTH="$(tb terraform -chdir="${ENV_DIR}" output -raw redis_auth_string | tr -d '[:space:]')"
tb sh -c "kubectl -n messaging create secret generic redis-auth \
  --from-literal=password='${REDIS_AUTH}' \
  --dry-run=client -o yaml | kubectl apply -f -"
unset REDIS_AUTH

# 6. Render manifests with the real image refs and environment values, and apply.
#    The Workload Identity annotation is substituted from a Terraform output
#    rather than hardcoded: it must match the IAM binding exactly, and a
#    mismatch fails silently at runtime with PermissionDenied on every Google
#    API call.
echo "==> kubectl apply (namespace messaging)"
tb sh -c "kubectl kustomize k8s/overlays/dev \
  | sed -e 's#REGISTRY-PLACEHOLDER/gateway:latest#${AR}/gateway:${TAG}#' \
        -e 's#REGISTRY-PLACEHOLDER/ingest:latest#${AR}/ingest:${TAG}#' \
        -e 's#REGISTRY-PLACEHOLDER/fanout:latest#${AR}/fanout:${TAG}#' \
        -e 's#REGISTRY-PLACEHOLDER/chatmgmt:latest#${AR}/chatmgmt:${TAG}#' \
        -e 's#CHATMGMT-SA-PLACEHOLDER#${CHATMGMT_SA}#' \
        -e 's#PROJECT-ID-PLACEHOLDER#${PROJECT_ID}#' \
        -e 's#FIRESTORE-DATABASE-PLACEHOLDER#${FIRESTORE_DB}#' \
        -e 's#REDIS-ADDR-PLACEHOLDER#${REDIS_ADDR}#' \
  | kubectl apply -f -"

# 7. Wait for the health services to become Available.
echo "==> waiting for rollouts"
tb kubectl -n messaging wait --for=condition=Available deployment --all --timeout=300s

# 8. Publish the event schemas (M0.3). The registry is not a Terraform resource
#    — see scripts/schema.sh — so it is created and populated here, after the
#    Kafka cluster its creation depends on exists.
echo "==> schema registry"
REGION="${REGION}" PROJECT_ID="${PROJECT_ID}" ./scripts/schema.sh create
REGION="${REGION}" PROJECT_ID="${PROJECT_ID}" ./scripts/schema.sh register

echo "==> Deployed. External LB IP (may take a few minutes to populate):"
tb kubectl -n messaging get ingress gateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}' || true
echo
echo "==> Remember to tear down at end of session: make teardown"
