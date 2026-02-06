# ECR Module

Provisions ECR container image repositories with lifecycle policies for the Realtime Messaging Platform.

## Architecture

- **4 repositories**: One per service (gateway, ingest, fanout, chatmgmt)
- **Immutable tags**: Prevents tag overwrite per ADR-014 §8.1
- **Scan on push**: CVE scanning for every image push
- **Lifecycle policies**: Keep last 10 tagged, expire untagged after 7 days

## Usage

```hcl
module "ecr" {
  source = "../../modules/ecr"

  project_name = "messaging-platform"
}
```

## References

- [TBD-TF0-6: ECR Lifecycle & Image Strategy](../../../docs/tbd/TF0-DECISIONS.md)

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
