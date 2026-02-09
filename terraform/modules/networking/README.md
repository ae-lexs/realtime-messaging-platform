# Networking Module

Provisions the VPC, public/private subnets, NAT Gateway(s), Internet Gateway, VPC Gateway Endpoints (S3, DynamoDB), and security groups for the Realtime Messaging Platform.

## Architecture

- **Public subnets**: Host ALB and NAT Gateway only
- **Private subnets**: Host all compute (ECS tasks), no IGW routes
- **VPC Gateway Endpoints**: S3 and DynamoDB (free, reduces NAT charges)
- **VPC Interface Endpoints** (optional): Secrets Manager, SSM, KMS — for ECS tasks accessing auth services in private subnets (TBD-TF1-6)
- **Security groups**: 7 base groups enforcing ADR-002 plane boundaries (sg-alb, sg-gateway, sg-ingest, sg-fanout, sg-chatmgmt, sg-redis, sg-msk) + optional sg-vpc-endpoints

## Usage

```hcl
module "networking" {
  source = "../../modules/networking"

  project_name                   = "messaging-platform"
  environment                    = "dev"
  vpc_cidr                       = "10.0.0.0/16"
  az_count                       = 2
  single_nat_gateway             = true
  enable_vpc_interface_endpoints = true
}
```

## References

- [TBD-TF0-1: Region & Availability Zones](../../../docs/tbd/TF0-DECISIONS.md)
- [TBD-TF0-2: VPC & Network Topology](../../../docs/tbd/TF0-DECISIONS.md)
- [TBD-TF0-4: Security Group Architecture](../../../docs/tbd/TF0-DECISIONS.md)
- [TBD-TF1-6: VPC Interface Endpoints](../../../docs/tbd/TF1-DECISIONS.md)

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
