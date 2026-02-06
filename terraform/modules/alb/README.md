# ALB Module

Provisions an Application Load Balancer with HTTPS listener, target groups, and routing rules for the Realtime Messaging Platform.

## Architecture

- **ALB**: External-facing, in public subnets
- **HTTPS listener**: TLS 1.3 policy, default 404 response
- **HTTP listener**: 301 redirect to HTTPS
- **Idle timeout**: 3600s for WebSocket connections (ADR-005)
- **No sticky sessions**: WebSocket connections pin naturally (ADR-014 §1.2)

### Routing Rules

| Priority | Condition | Target |
|----------|-----------|--------|
| 1 | `/ws` + Upgrade: websocket | Gateway (8080) |
| 2 | `/api/v1/*` | Chat Mgmt (8083) |
| 3 | `/health` | Gateway (8080) |
| Default | All others | 404 |

## Usage

```hcl
module "alb" {
  source = "../../modules/alb"

  project_name          = "messaging-platform"
  environment           = "dev"
  vpc_id                = module.networking.vpc_id
  public_subnet_ids     = module.networking.public_subnet_ids
  alb_security_group_id = module.networking.alb_security_group_id
  certificate_arn       = module.dns.certificate_arn
}
```

## References

- [TBD-TF0-5: ALB & DNS Configuration](../../../docs/tbd/TF0-DECISIONS.md)
- [ADR-005: WebSocket Protocol](../../adr/ADR-005.md)
- [ADR-014 §1.2: ALB Routing](../../adr/ADR-014.md)

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
