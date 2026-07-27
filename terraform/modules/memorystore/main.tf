# Memorystore for Redis — abuse control and token revocation (ADR-010,
# ADR-013, ADR-021 Decision D).
#
# This instance arrives in M1.2 rather than M3.1 because auth needs it before
# presence does: ADR-013 requires the revocation check and the OTP rate limits
# to fail closed, so there is no version of the auth service that runs without
# Redis. M3.1 adds the presence adapter on top of what this provisions.
#
# Unlike Firestore, this is not serverless and bills while it exists. It must
# come down with `make teardown` at the end of a session (ADR-021).
resource "google_redis_instance" "main" {
  project        = var.project_id
  name           = "${var.name_prefix}-redis"
  region         = var.region
  memory_size_gb = var.memory_size_gb
  redis_version  = var.redis_version

  # BASIC is a single node with no replica and no failover. That is the honest
  # choice here rather than a cost compromise: ADR-010 treats Redis as
  # reconstructible state, and ADR-013 requires a Redis outage to deny access
  # rather than degrade it. A STANDARD_HA instance would mask exactly the
  # failure mode M7.4 sets out to induce.
  tier = "BASIC"

  # See the networking module: DIRECT_PEERING would not carry return traffic to
  # GKE pod IPs without an ip-masq-agent config Autopilot will not accept.
  connect_mode       = "PRIVATE_SERVICE_ACCESS"
  authorized_network = var.network_id
  reserved_ip_range  = var.reserved_ip_range

  # AUTH is on, so possession of a VPC route is not by itself possession of the
  # revocation list. The generated string is read back as an output and handed
  # to the workload as a Kubernetes secret; it is never written to Terraform
  # state in plaintext by us, though the provider does record it, which is why
  # the state bucket is private.
  auth_enabled = var.auth_enabled

  # In-transit encryption is off: the traffic never leaves the VPC, and
  # go-redis would need a TLS config and the server CA threaded through
  # internal/redis to speak it. That is real work with no attacker it excludes
  # here, so it is named rather than silently omitted — revisit if this ever
  # carries traffic across a network boundary.
  transit_encryption_mode = "DISABLED"

  labels = var.labels
}
