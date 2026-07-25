output "repository_id" {
  description = "The Artifact Registry repository ID."
  value       = google_artifact_registry_repository.main.repository_id
}

output "repository_name" {
  description = "The fully-qualified Artifact Registry repository resource name."
  value       = google_artifact_registry_repository.main.name
}

output "registry_url" {
  description = "Base image-push URL: {region}-docker.pkg.dev/{project}/{repository_id}."
  value       = "${google_artifact_registry_repository.main.location}-docker.pkg.dev/${google_artifact_registry_repository.main.project}/${google_artifact_registry_repository.main.repository_id}"
}
