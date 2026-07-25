# gke

Regional GKE Autopilot cluster running all four services (ADR-021 Decision B).
Autopilot manages nodes, autoscaling, and security posture. The control plane is
restricted to `master_authorized_cidr_blocks`, and `deletion_protection = false`
so the mandatory end-of-session `terraform destroy` is never blocked.

## Usage

```hcl
module "gke" {
  source              = "../../modules/gke"
  project_id          = var.project_id
  region              = var.region
  name_prefix         = "messaging-dev"
  network_id          = module.networking.network_id
  subnet_id           = module.networking.subnet_id
  pods_range_name     = module.networking.pods_range_name
  services_range_name = module.networking.services_range_name
}
```

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
|------|---------|
| terraform | >= 1.9.0, < 2.0.0 |
| google | >= 6.0 |

## Providers

| Name | Version |
|------|---------|
| google | >= 6.0 |

## Resources

| Name | Type |
|------|------|
| [google_container_cluster.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/container_cluster) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| name\_prefix | Prefix for cloud resource names (e.g. messaging-dev). | `string` | n/a | yes |
| network\_id | VPC network self-link/ID the cluster attaches to. | `string` | n/a | yes |
| pods\_range\_name | Secondary range name for pod IPs (must exist on the subnet). | `string` | n/a | yes |
| project\_id | GCP project ID that owns the cluster. | `string` | n/a | yes |
| region | Region for the regional Autopilot cluster. | `string` | n/a | yes |
| services\_range\_name | Secondary range name for service IPs (must exist on the subnet). | `string` | n/a | yes |
| subnet\_id | Subnet self-link/ID the cluster nodes run in. | `string` | n/a | yes |
| master\_authorized\_cidr\_blocks | CIDR blocks allowed to reach the cluster control plane (kubectl). The apply script auto-fills the operator's public IP; empty locks the control plane to Google-internal only. | <pre>list(object({<br/>    cidr_block   = string<br/>    display_name = string<br/>  }))</pre> | `[]` | no |
| release\_channel | GKE release channel for the Autopilot cluster. | `string` | `"REGULAR"` | no |

## Outputs

| Name | Description |
|------|-------------|
| ca\_certificate | Base64-encoded cluster CA certificate (for kubeconfig). |
| cluster\_id | The fully-qualified cluster resource ID. |
| cluster\_name | The Autopilot cluster name. |
| endpoint | The cluster control-plane endpoint IP. |
| location | The cluster location (region). |
<!-- END_TF_DOCS -->
