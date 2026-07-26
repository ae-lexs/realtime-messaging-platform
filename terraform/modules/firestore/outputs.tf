output "database_id" {
  description = "The Firestore database ID, passed to clients as FIRESTORE_DATABASE."
  value       = google_firestore_database.main.name
}

output "location" {
  description = "The Firestore location the database was created in (immutable)."
  value       = google_firestore_database.main.location_id
}

output "ttl_field" {
  description = "The field carrying the sessions TTL policy, as collection.field."
  value       = "${google_firestore_field.sessions_ttl.collection}.${google_firestore_field.sessions_ttl.field}"
}
