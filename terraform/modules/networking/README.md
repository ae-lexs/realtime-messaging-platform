# Networking Module

Provisions the VPC, public/private subnets, NAT Gateway(s), Internet Gateway, VPC Gateway Endpoints (S3, DynamoDB), and security groups for the Realtime Messaging Platform.

## Architecture

- **Public subnets**: Host ALB and NAT Gateway only
- **Private subnets**: Host all compute (ECS tasks), no IGW routes
- **VPC Endpoints**: S3 and DynamoDB Gateway Endpoints (free, reduces NAT charges)
- **Security groups**: 7 groups enforcing ADR-002 plane boundaries (sg-alb, sg-gateway, sg-ingest, sg-fanout, sg-chatmgmt, sg-redis, sg-msk)

## Usage

```hcl
module "networking" {
  source = "../../modules/networking"

  project_name       = "messaging-platform"
  environment        = "dev"
  vpc_cidr           = "10.0.0.0/16"
  az_count           = 2
  single_nat_gateway = true
}
```

## References

- [TBD-TF0-1: Region & Availability Zones](../../../docs/tbd/TF0-DECISIONS.md)
- [TBD-TF0-2: VPC & Network Topology](../../../docs/tbd/TF0-DECISIONS.md)
- [TBD-TF0-4: Security Group Architecture](../../../docs/tbd/TF0-DECISIONS.md)

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
