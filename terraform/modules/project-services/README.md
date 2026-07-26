# project-services

Enables the Google Cloud APIs the platform depends on. APIs are left enabled on
destroy (`disable_on_destroy = false`) — they carry no cost, may be shared, and
disabling them can be slow or fail; the deploy-and-destroy loop tears down
billable resources, not the API surface.

## Usage

```hcl
module "project_services" {
  source     = "../../modules/project-services"
  project_id = var.project_id
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
| [google_project_service.this](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/project_service) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| project\_id | GCP project ID in which to enable the APIs. | `string` | n/a | yes |
| services | Google Cloud APIs to enable for the platform. | `list(string)` | <pre>[<br/>  "compute.googleapis.com",<br/>  "container.googleapis.com",<br/>  "artifactregistry.googleapis.com",<br/>  "managedkafka.googleapis.com",<br/>  "firestore.googleapis.com",<br/>  "cloudresourcemanager.googleapis.com",<br/>  "billingbudgets.googleapis.com",<br/>  "iam.googleapis.com",<br/>  "logging.googleapis.com",<br/>  "monitoring.googleapis.com"<br/>]</pre> | no |

## Outputs

| Name | Description |
|------|-------------|
| enabled\_services | The set of Google Cloud API service names enabled by this module. |
<!-- END_TF_DOCS -->
