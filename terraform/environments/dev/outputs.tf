output "region" {
  description = "The region all regional resources were created in."
  value       = module.gke.location
}

output "cluster_name" {
  description = "The GKE Autopilot cluster name."
  value       = module.gke.cluster_name
}

output "network_name" {
  description = "The VPC network name."
  value       = module.networking.network_name
}

output "artifact_registry_url" {
  description = "Base image-push URL for the service images."
  value       = module.artifact_registry.registry_url
}

output "kafka_cluster_id" {
  description = "The Managed Kafka cluster ID. Its existence is what allows the schema registry to be created in this region."
  value       = module.kafka.cluster_id
}

output "budget_name" {
  description = "The billing budget display name guarding this environment."
  value       = module.budget.budget_name
}

output "get_credentials_command" {
  description = "gcloud command to populate kubeconfig for kubectl access."
  value       = "gcloud container clusters get-credentials ${module.gke.cluster_name} --region ${module.gke.location} --project ${var.project_id}"
}
