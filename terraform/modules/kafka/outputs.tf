output "cluster_id" {
  description = "The Kafka cluster ID."
  value       = google_managed_kafka_cluster.main.cluster_id
}

output "cluster_name" {
  description = "The fully-qualified cluster resource name, projects/{project}/locations/{region}/clusters/{cluster_id}."
  value       = google_managed_kafka_cluster.main.id
}

# The provider exports no bootstrap address; clients resolve it at M2.3 with
# `gcloud managed-kafka clusters describe --format="value(bootstrapAddress)"`.

output "location" {
  description = "The region the cluster runs in; the schema registry must match it."
  value       = google_managed_kafka_cluster.main.location
}
