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
| [google_managed_kafka_cluster.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/managed_kafka_cluster) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| name\_prefix | Resource name prefix, {project}-{environment}; the cluster is named {prefix}-kafka. | `string` | n/a | yes |
| project\_id | GCP project ID that owns the Kafka cluster. | `string` | n/a | yes |
| region | Region (location) for the Kafka cluster. The schema registry must be created in this same region. | `string` | n/a | yes |
| subnet\_id | Subnet the cluster attaches to, as projects/{project}/regions/{region}/subnetworks/{name}. | `string` | n/a | yes |
| memory\_gib | Memory to provision, in GiB. Must be between 1 and 8 GiB per vCPU; the default is the 1 GiB-per-vCPU floor. | `number` | `3` | no |
| vcpu\_count | vCPUs to provision. 3 is the service minimum and what the lab uses — capacity is fixed, not autoscaled. | `number` | `3` | no |

## Outputs

| Name | Description |
|------|-------------|
| cluster\_id | The Kafka cluster ID. |
| cluster\_name | The fully-qualified cluster resource name, projects/{project}/locations/{region}/clusters/{cluster\_id}. |
| location | The region the cluster runs in; the schema registry must match it. |
<!-- END_TF_DOCS -->