# Auth Module

Provisions the authentication infrastructure for the Realtime Messaging Platform: DynamoDB tables, KMS customer-managed keys, Secrets Manager secrets, SSM parameters, and IAM roles for all ECS services.

## Architecture

- **DynamoDB tables**: `users` (phone_number-index GSI), `sessions` (user_sessions-index GSI, TTL), `otp_requests` (TTL)
- **KMS keys**: `auth-secrets` CMK (Secrets Manager encryption), `otp-encryption` CMK (OTP ciphertext operations)
- **Secrets Manager**: OTP pepper secret container (value managed by operational script)
- **SSM Parameter Store**: JWT cache TTL (`/messaging/jwt/cache-ttl-seconds`); public keys and key metadata managed by operational script
- **IAM roles**: 1 shared execution role + 4 task roles (chatmgmt, gateway, ingest, fanout) with least-privilege auth policies

## Usage

```hcl
module "auth" {
  source = "../../modules/auth"

  project_name                = "messaging-platform"
  environment                 = "dev"
  enable_deletion_protection  = false
  secret_recovery_window_days = 0
}
```

## Operational Scripts

JWT signing keys and the OTP pepper value are managed by `scripts/generate-jwt-keys.sh`, not Terraform. See [TBD-TF1-8](../../../docs/tbd/TF1-DECISIONS.md) for the ownership boundary.

## References

- [TBD-TF1-1 through TBD-TF1-8: Auth Infrastructure Decisions](../../../docs/tbd/TF1-DECISIONS.md)
- [ADR-015: Authentication & OTP Implementation](../../../docs/adr/ADR-015.md)
- [ADR-007: Data Model and Index Strategy](../../../docs/adr/ADR-007.md)
- [ADR-014: Technology Stack & Deployment](../../../docs/adr/ADR-014.md)

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
