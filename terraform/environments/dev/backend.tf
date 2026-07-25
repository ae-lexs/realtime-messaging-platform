# Remote state — GCS bucket (native object-level locking; no separate lock
# table is needed, unlike the retired S3 + DynamoDB backend). The bucket name
# is project-derived, so it is supplied at init time via partial configuration
# rather than hardcoded here:
#
#   terraform init \
#     -backend-config="bucket=${PROJECT_ID}-tf-state" \
#     -backend-config="prefix=dev"
#
# Bootstrap the bucket first with: scripts/bootstrap-terraform-state.sh
terraform {
  backend "gcs" {}
}
