# Firestore in native mode — the identity & membership tier (ADR-021 Decision A,
# ADR-023 "Firestore model"). It holds users, chats, memberships and sessions:
# read-mostly document state that needs no cross-entity transaction.
#
# Firestore is serverless and scales to zero, so unlike the Postgres primary or
# the Kafka cluster it carries no idle cost between deploy-and-destroy cycles.
resource "google_firestore_database" "main" {
  project     = var.project_id
  name        = var.name_prefix
  location_id = var.region
  type        = "FIRESTORE_NATIVE"

  # Both are required for the mandatory end-of-session destroy to work, and
  # neither defaults to what this project needs: `deletion_policy` defaults to
  # ABANDON, which leaves the database (and its data) behind after
  # `terraform destroy` while reporting success — the same trap as GKE's
  # deletion_protection in M0.2.
  delete_protection_state = "DELETE_PROTECTION_DISABLED"
  deletion_policy         = "DELETE"
}

# TTL policy on sessions.expires_at (ADR-023): Firestore expires session
# documents natively, so there is no sweeper on this side of the split — the
# opposite of Postgres, where idempotency_keys and outbox need pg_cron.
#
# TTL is garbage collection, NOT the correctness gate. Firestore deletes within
# ~24h of expires_at, so an expired session stays readable for up to a day; the
# auth service must check expires_at in code on every read (ADR-023
# application-enforced invariant, implemented as SessionDoc.IsExpired).
resource "google_firestore_field" "sessions_ttl" {
  project    = var.project_id
  database   = google_firestore_database.main.name
  collection = "sessions"
  field      = "expires_at"

  ttl_config {}

  # Disables the built-in single-field indexes for `expires_at` ONLY. A
  # monotonically increasing timestamp is a poor index (hotspot on write) and
  # nothing queries by it. Do NOT copy this onto user_id: ADR-023's
  # `where('user_id','==',u)` depends on that field's automatic index.
  index_config {}
}
