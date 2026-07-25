# Docker-format Artifact Registry repository holding the four service images.
resource "google_artifact_registry_repository" "main" {
  project       = var.project_id
  location      = var.region
  repository_id = var.repository_id
  format        = "DOCKER"
  description   = var.description
}

# Grant pull access (least privilege, repo-scoped). New GCP projects no longer
# auto-grant the default compute service account any Artifact Registry role, so
# without this the GKE nodes get 403 on image pull (ImagePullBackOff).
resource "google_artifact_registry_repository_iam_member" "readers" {
  for_each = toset(var.reader_members)

  project    = google_artifact_registry_repository.main.project
  location   = google_artifact_registry_repository.main.location
  repository = google_artifact_registry_repository.main.name
  role       = "roles/artifactregistry.reader"
  member     = each.value
}
