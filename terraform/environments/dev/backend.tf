# Remote state — GCS bucket. Object-level locking is native to GCS, so no
# separate lock table is provisioned or needed anywhere. The bucket name
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
