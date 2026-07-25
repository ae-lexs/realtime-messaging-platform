# artifact-registry

Docker-format Artifact Registry repository holding the four service images, plus
repo-scoped `roles/artifactregistry.reader` bindings for `reader_members` (e.g.
the GKE node service account, so Autopilot can pull the images — new projects no
longer grant this by default).

## Usage

```hcl
module "artifact_registry" {
  source         = "../../modules/artifact-registry"
  project_id     = var.project_id
  region         = var.region
  reader_members = ["serviceAccount:${data.google_project.this.number}-compute@developer.gserviceaccount.com"]
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
| [google_artifact_registry_repository.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/artifact_registry_repository) | resource |
| [google_artifact_registry_repository_iam_member.readers](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/artifact_registry_repository_iam_member) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| project\_id | GCP project ID that owns the Artifact Registry repository. | `string` | n/a | yes |
| region | Region (location) for the Docker repository. | `string` | n/a | yes |
| description | Human-readable description of the repository. | `string` | `"Container images for the realtime messaging platform services."` | no |
| reader\_members | IAM members granted read (pull) access — e.g. the GKE node service account, so Autopilot can pull the images. | `list(string)` | `[]` | no |
| repository\_id | Artifact Registry repository ID (the last path segment of image refs). | `string` | `"messaging"` | no |

## Outputs

| Name | Description |
|------|-------------|
| reader\_members | IAM members granted read (pull) access to the repository. |
| registry\_url | Base image-push URL: {region}-docker.pkg.dev/{project}/{repository\_id}. |
| repository\_id | The Artifact Registry repository ID. |
| repository\_name | The fully-qualified Artifact Registry repository resource name. |
<!-- END_TF_DOCS -->
