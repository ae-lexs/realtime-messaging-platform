# DNS Module

Provisions a Route 53 hosted zone and ACM certificate with DNS validation for the Realtime Messaging Platform.

## Architecture

- **Route 53 zone**: Created for the configurable domain name, with `prevent_destroy` lifecycle
- **ACM certificate**: Wildcard (`*.domain`) + apex (`domain`), DNS-validated in the same zone
- **Validation**: Automatic DNS record creation and certificate validation

## Usage

```hcl
module "dns" {
  source = "../../modules/dns"

  project_name = "messaging-platform"
  environment  = "dev"
  domain_name  = "messaging.example.com"
}
```

## References

- [TBD-TF0-5: ALB & DNS Configuration](../../../docs/tbd/TF0-DECISIONS.md)

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
