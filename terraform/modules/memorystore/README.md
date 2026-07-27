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
| [google_redis_instance.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/redis_instance) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| name\_prefix | Resource name prefix, {project}-{environment}; the instance is named {prefix}-redis. | `string` | n/a | yes |
| network\_id | VPC network the instance is authorized on, as projects/{project}/global/networks/{name}. | `string` | n/a | yes |
| project\_id | GCP project ID that owns the Redis instance. | `string` | n/a | yes |
| region | Region for the Redis instance. Must match the GKE cluster's region. | `string` | n/a | yes |
| reserved\_ip\_range | Name of the Private Services Access reserved range the instance allocates from (networking module output). | `string` | n/a | yes |
| auth\_enabled | Whether to require an AUTH string. Keep this true: without it, anything with a route into the VPC can read and clear the revocation list. | `bool` | `true` | no |
| labels | Resource labels for cost allocation. | `map(string)` | `{}` | no |
| memory\_size\_gb | Instance memory in GiB. 1 is the BASIC-tier minimum and ample: the keyspace is rate-limit counters and revoked JTIs, all TTL-bounded to at most an hour. | `number` | `1` | no |
| redis\_version | Redis engine version. The rate-limit script uses only INCR/EXPIRE and needs nothing newer than 6.x, but 7.x is the current default. | `string` | `"REDIS_7_0"` | no |

## Outputs

| Name | Description |
|------|-------------|
| address | host:port, the form internal/redis takes as REDIS\_ADDR. |
| auth\_string | Generated AUTH string, empty when auth\_enabled is false. The deploy script reads it into a Kubernetes secret; it is never baked into an image or a manifest. |
| host | Private IP of the Redis instance. |
| instance\_name | The Redis instance name. |
| port | Port the Redis instance listens on. |
<!-- END_TF_DOCS -->