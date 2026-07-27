# networking

Custom-mode VPC with a single regional subnet (secondary ranges for VPC-native
GKE pods and services) plus Cloud Router + Cloud NAT for private egress. Nodes
reach the internet (image pulls, module downloads) without external IPs.

## Usage

```hcl
module "networking" {
  source      = "../../modules/networking"
  project_id  = var.project_id
  region      = var.region
  name_prefix = "messaging-dev"
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
| [google_compute_global_address.private_service_access](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/compute_global_address) | resource |
| [google_compute_network.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/compute_network) | resource |
| [google_compute_router.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/compute_router) | resource |
| [google_compute_router_nat.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/compute_router_nat) | resource |
| [google_compute_subnetwork.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/compute_subnetwork) | resource |
| [google_service_networking_connection.private_service_access](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/service_networking_connection) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| name\_prefix | Prefix for cloud resource names (e.g. messaging-dev). | `string` | n/a | yes |
| project\_id | GCP project ID that owns the network resources. | `string` | n/a | yes |
| region | Region for the subnet, Cloud Router, and Cloud NAT. | `string` | n/a | yes |
| pods\_cidr | Secondary IPv4 CIDR range for GKE pod IPs. | `string` | `"10.20.0.0/16"` | no |
| pods\_range\_name | Name of the secondary range used for GKE pod IPs (VPC-native). | `string` | `"pods"` | no |
| private\_service\_access\_prefix\_length | Prefix length of the range reserved for Private Services Access. Google allocates the address itself; /16 is their recommended size and leaves room for Memorystore (M1.2) and Cloud SQL (M2.1) to coexist without a second reservation. | `number` | `16` | no |
| services\_cidr | Secondary IPv4 CIDR range for GKE service (ClusterIP) IPs. | `string` | `"10.30.0.0/20"` | no |
| services\_range\_name | Name of the secondary range used for GKE service (ClusterIP) IPs. | `string` | `"services"` | no |
| subnet\_cidr | Primary IPv4 CIDR range for the GKE node subnet. | `string` | `"10.10.0.0/20"` | no |

## Outputs

| Name | Description |
|------|-------------|
| nat\_name | The Cloud NAT name providing pod egress. |
| network\_id | The VPC network self-link/ID, for attaching the GKE cluster. |
| network\_name | The VPC network name. |
| pods\_range\_name | Secondary range name for GKE pod IPs (for ip\_allocation\_policy). |
| private\_service\_access\_connection\_id | The service networking connection ID, for consumers to depend\_on. |
| private\_service\_access\_range\_name | Name of the reserved range for Private Services Access; managed services take reserved\_ip\_range from it. |
| router\_name | The Cloud Router name backing egress NAT. |
| services\_range\_name | Secondary range name for GKE service IPs (for ip\_allocation\_policy). |
| subnet\_id | The subnet self-link/ID, for attaching the GKE cluster. |
| subnet\_name | The subnet name. |
<!-- END_TF_DOCS -->
