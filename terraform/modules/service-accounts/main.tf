# Per-service Google identities, bound to Kubernetes service accounts through
# GKE Workload Identity. This is how ADR-015 §3.2's least-privilege table is
# enforced: one identity per service, granted only what that service reads.
#
# Workload Identity is the mechanism that lets a pod call Google APIs with a
# specific identity and no key file: a pod running under KSA
# {namespace}/{name} authenticates as this service account because of the
# binding below plus the iam.gke.io/gcp-service-account annotation on the KSA.
# Both halves are required, and getting one wrong is silent — the pod simply
# has no usable credentials and every Firestore call returns PermissionDenied
# with nothing pointing at the cause. That is the first thing to check on a
# failed first deploy.
#
# Only ChatMgmt has an identity today. Gateway and Ingest need public-key
# access to validate tokens, but neither validates anything yet (M3.2, M2.2),
# and an identity with no workload behind it is a permission grant nobody is
# accountable for.
resource "google_service_account" "chatmgmt" {
  project      = var.project_id
  account_id   = "${var.name_prefix}-chatmgmt"
  display_name = "Chat Management service (auth, chats, memberships)"
  description  = "Workload Identity target for the chatmgmt deployment; reads Firestore and the auth secrets."
}

# Firestore access. roles/datastore.user is read+write on documents but grants
# nothing administrative — it cannot create or delete a database, change
# indexes, or alter the TTL policy, all of which belong to Terraform.
resource "google_project_iam_member" "chatmgmt_firestore" {
  project = var.project_id
  role    = "roles/datastore.user"
  member  = "serviceAccount:${google_service_account.chatmgmt.email}"
}

# The Workload Identity binding. The member is a fixed GCP-side format, not a
# name we choose: {project}.svc.id.goog[{namespace}/{ksa}].
resource "google_service_account_iam_member" "chatmgmt_workload_identity" {
  service_account_id = google_service_account.chatmgmt.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[${var.kubernetes_namespace}/${var.chatmgmt_ksa_name}]"
}
