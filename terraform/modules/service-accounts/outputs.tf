output "chatmgmt_email" {
  description = "Email of the ChatMgmt service account; the value of the KSA's iam.gke.io/gcp-service-account annotation, and the member granted access to the auth secrets."
  value       = google_service_account.chatmgmt.email
}

output "chatmgmt_member" {
  description = "The ChatMgmt identity as an IAM member string, serviceAccount:{email}."
  value       = "serviceAccount:${google_service_account.chatmgmt.email}"
}
