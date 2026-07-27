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
| [google_firestore_database.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/firestore_database) | resource |
| [google_firestore_field.otp_requests_ttl](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/firestore_field) | resource |
| [google_firestore_field.sessions_ttl](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/firestore_field) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| name\_prefix | Resource name prefix, {project}-{environment}; used verbatim as the Firestore database ID. | `string` | n/a | yes |
| project\_id | GCP project ID that owns the Firestore database. | `string` | n/a | yes |
| region | Firestore location. Immutable after creation — moving regions means a new database. | `string` | n/a | yes |

## Outputs

| Name | Description |
|------|-------------|
| database\_id | The Firestore database ID, passed to clients as FIRESTORE\_DATABASE. |
| location | The Firestore location the database was created in (immutable). |
| ttl\_field | The field carrying the sessions TTL policy, as collection.field. |
<!-- END_TF_DOCS -->