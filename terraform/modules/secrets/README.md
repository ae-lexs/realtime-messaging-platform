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
| [google_secret_manager_secret.auth](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/secret_manager_secret) | resource |
| [google_secret_manager_secret_iam_member.accessors](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/secret_manager_secret_iam_member) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| project\_id | GCP project ID that owns the secrets. | `string` | n/a | yes |
| accessor\_members | IAM members granted roles/secretmanager.secretAccessor on the auth secrets, as serviceAccount:{email}. | `list(string)` | `[]` | no |
| labels | Resource labels for cost allocation. | `map(string)` | `{}` | no |
| signing\_key\_id | Identifier of the active JWT signing key; it becomes the JWT `kid` header and the suffix of the signing/public secret names. A rotation adds a second key ID rather than replacing this one (ADR-015 §3.2), which nothing exercises before M7. | `string` | `"primary"` | no |

## Outputs

| Name | Description |
|------|-------------|
| current\_key\_id\_secret\_id | Secret ID naming the active signing key. Read at startup to resolve which private key to load. |
| otp\_pepper\_secret\_id | Secret ID holding the OTP HMAC pepper (ADR-015 §1.2). |
| public\_key\_secret\_id | Secret ID holding the RSA public key (PEM). Listing by the jwt-public-key- prefix is how token-validating services discover acceptable `kid` values. |
| signing\_key\_id | The active JWT key ID; scripts/auth-keys.sh needs it to know which secrets to fill. |
| signing\_key\_secret\_id | Secret ID holding the RSA private key (PEM). Created empty; filled by scripts/auth-keys.sh. |
<!-- END_TF_DOCS -->