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
| [google_project_iam_member.chatmgmt_firestore](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/project_iam_member) | resource |
| [google_service_account.chatmgmt](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/service_account) | resource |
| [google_service_account_iam_member.chatmgmt_workload_identity](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/service_account_iam_member) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| name\_prefix | Resource name prefix, {project}-{environment}. Service account IDs are capped at 30 characters, so a longer prefix than messaging-{env} will not fit. | `string` | n/a | yes |
| project\_id | GCP project ID that owns the service accounts. | `string` | n/a | yes |
| chatmgmt\_ksa\_name | Name of the ChatMgmt Kubernetes service account. Must match the KSA in k8s/base/chatmgmt.yaml, including its iam.gke.io/gcp-service-account annotation — a mismatch leaves the pod with no GCP identity and no error explaining why. | `string` | `"chatmgmt"` | no |
| kubernetes\_namespace | Namespace of the Kubernetes service accounts being bound. Must match k8s/base/namespace.yaml. | `string` | `"messaging"` | no |

## Outputs

| Name | Description |
|------|-------------|
| chatmgmt\_email | Email of the ChatMgmt service account; the value of the KSA's iam.gke.io/gcp-service-account annotation, and the member granted access to the auth secrets. |
| chatmgmt\_member | The ChatMgmt identity as an IAM member string, serviceAccount:{email}. |
<!-- END_TF_DOCS -->