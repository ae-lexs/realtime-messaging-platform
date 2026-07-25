# Enables the Google Cloud APIs the platform depends on. APIs are left enabled
# on destroy (disable_on_destroy = false): they carry no cost, may be shared,
# and disabling them can be slow or fail — the deploy-and-destroy loop tears
# down billable resources, not the API surface.
resource "google_project_service" "this" {
  for_each = toset(var.services)

  project = var.project_id
  service = each.value

  disable_on_destroy = false
}
