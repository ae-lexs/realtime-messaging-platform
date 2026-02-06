# ECS Cluster Module

Provisions an ECS Fargate cluster with capacity providers, Service Connect namespace, and CloudWatch log group for the Realtime Messaging Platform.

## Architecture

- **ECS Cluster**: Fargate-based with Container Insights
- **Capacity providers**: FARGATE (always) + FARGATE_SPOT (dev only per TBD-TF0-7)
- **Service Connect**: Cloud Map HTTP namespace (`messaging.local` per ADR-014 §5.3)
- **Logging**: CloudWatch log group with configurable retention
- **Execute Command**: Toggleable for dev debugging

## Usage

```hcl
module "ecs_cluster" {
  source = "../../modules/ecs-cluster"

  project_name          = "messaging-platform"
  environment           = "dev"
  enable_fargate_spot   = true
  enable_execute_command = true
  log_retention_days    = 30
}
```

## References

- [TBD-TF0-7: ECS Cluster Configuration](../../../docs/tbd/TF0-DECISIONS.md)
- [ADR-014 §5.3: Service Discovery](../../adr/ADR-014.md)

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
