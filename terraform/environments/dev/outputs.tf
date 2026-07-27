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

output "firestore_database_id" {
  description = "The Firestore database ID; clients read it as FIRESTORE_DATABASE."
  value       = module.firestore.database_id
}

output "kafka_cluster_id" {
  description = "The Managed Kafka cluster ID. Its existence is what allows the schema registry to be created in this region."
  value       = module.kafka.cluster_id
}

output "redis_address" {
  description = "host:port of the Memorystore instance; ChatMgmt reads it as REDIS_ADDR. Reachable only from inside the VPC."
  value       = module.memorystore.address
}

output "redis_auth_string" {
  description = "Memorystore AUTH string. scripts/deploy.sh reads it into a Kubernetes secret rather than a manifest or an image."
  value       = module.memorystore.auth_string
  sensitive   = true
}

output "chatmgmt_service_account" {
  description = "ChatMgmt's Google service account email — the value of the KSA's iam.gke.io/gcp-service-account annotation."
  value       = module.service_accounts.chatmgmt_email
}

output "jwt_signing_key_id" {
  description = "Active JWT key ID. scripts/auth-keys.sh fills the secrets named after it."
  value       = module.secrets.signing_key_id
}

output "budget_name" {
  description = "The billing budget display name guarding this environment."
  value       = module.budget.budget_name
}

output "get_credentials_command" {
  description = "gcloud command to populate kubeconfig for kubectl access."
  value       = "gcloud container clusters get-credentials ${module.gke.cluster_name} --region ${module.gke.location} --project ${var.project_id}"
}
