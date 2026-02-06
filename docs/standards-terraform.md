# Terraform Standards

This document defines Terraform conventions for the Realtime Messaging Platform. The principles mirror our Go standards: clarity over cleverness, explicit over implicit, minimal complexity for the current task.

For architecture, process, and CI/CD, see [CONTRIBUTING.md](../CONTRIBUTING.md). For Go conventions, see [STANDARDS-GO.md](STANDARDS-GO.md).

---

## Invariants

These rules must never be violated. They are the Terraform equivalent of the Go Invariants section in the Go standards.

| Rule | Rationale | Enforcement |
|------|-----------|-------------|
| No credentials in code or `.tfvars` — ever | Secrets belong in AWS Secrets Manager, SSM, or CI environment variables | Code review, tfsec/trivy |
| Every variable and output has a `description` | Self-documenting infrastructure; terraform-docs generates from these | tflint |
| Every variable has an explicit `type` | Catches misconfiguration at plan time, not apply time | tflint |
| Providers are configured only in `environments/`, never in `modules/` | Modules must be environment-agnostic | Code review |
| No hardcoded IDs, ARNs, or account numbers | Use variables, data sources, or outputs from other modules | Code review |
| `required_version` and `required_providers` in every root module | Prevents version drift across environments | tflint |
| `.terraform.lock.hcl` is committed to version control | Ensures reproducible provider installations across machines and CI | `.gitignore` config |
| State files (`.tfstate`) are never committed | State contains sensitive data in plaintext | `.gitignore` config |

## File Structure Per Module

Every Terraform module follows HashiCorp's standard layout:

```
terraform/modules/<module-name>/
├── main.tf           # Resources and data sources
├── variables.tf      # Input variables (REQUIRED then OPTIONAL, separated by comments)
├── outputs.tf        # Output values
├── versions.tf       # required_version + required_providers
└── README.md         # Module documentation (auto-generated via terraform-docs)
```

For modules with many resources, split `main.tf` into logical files named by concern:

```
terraform/modules/networking/
├── main.tf              # VPC, subnets, route tables
├── security_groups.tf   # Security group resources
├── endpoints.tf         # VPC endpoints
├── variables.tf
├── outputs.tf
└── versions.tf
```

**Root modules** (`environments/dev/`, `environments/prod/`) additionally contain:

| File | Purpose |
|------|---------|
| `backend.tf` | Remote state backend configuration |
| `providers.tf` | Provider configuration with `default_tags` |
| `terraform.tfvars.example` | Example variable values (committed, never real secrets) |

## Root Module vs Child Module Conventions

**Child modules** (`terraform/modules/*`):
- Must NOT configure providers or backends
- Define `required_providers` version constraints (broad: `>= 5.0`)
- Expose flexible interfaces via variables with sensible defaults
- Export at least one output per resource created

**Root modules** (`terraform/environments/*`):
- Configure providers with `default_tags` and region
- Configure backend (S3 + DynamoDB)
- Pin providers to minor version (`~> 5.x.0`)
- Compose child modules — contain minimal inline resources
- Target < 100 resources per state file

## When to Create a Module

Create a module when:
- Resources form a logical unit with a clear purpose (e.g., "networking", "ALB")
- The same pattern appears or will appear in 2+ environments
- You want to hide complexity behind a clean variable/output interface
- Resources share a security/IAM boundary

Do NOT create a module when:
- It would contain a single resource with pass-through variables
- The pattern exists only once and has no reuse potential
- It would create deep nesting (3+ module levels)

## Naming Conventions

**Terraform identifiers** — `snake_case`:
- Resource names: `aws_ecs_service.gateway`
- Variable names: `gateway_task_cpu`
- Module references: `module.ecs_cluster`
- Local values: `local.common_tags`

**Cloud resource names** — `kebab-case` with project-environment prefix:

```
{project}-{environment}-{component}
messaging-dev-gateway
messaging-prod-ecs-cluster
messaging-dev-alb
```

**Resource naming within modules:**
- Use `main` if a module contains a single resource of that type: `aws_vpc.main`
- Use descriptive names when differentiating: `aws_subnet.public`, `aws_subnet.private`
- Never repeat the resource type in the name: `aws_instance.web`, not `aws_instance.web_instance`

## Variable Design

**Ordering within `variables.tf`:**

```hcl
# ─────────────────────────────────────────────
# REQUIRED VARIABLES (no defaults)
# ─────────────────────────────────────────────

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
}

# ─────────────────────────────────────────────
# OPTIONAL VARIABLES (have defaults)
# ─────────────────────────────────────────────

variable "environment" {
  description = "Deployment environment"
  type        = string
  default     = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Environment must be dev, staging, or prod."
  }
}
```

Rules:
- Every variable MUST have `description` and `type` — CI-enforced via tflint
- Use `validation` blocks for variables with constrained value sets
- Name boolean variables positively: `enable_container_insights`, not `disable_insights`
- Include units in numeric variable names: `timeout_seconds`, `disk_size_gb`
- Mark sensitive variables with `sensitive = true`
- Prefer concrete `object({...})` types over `map(any)` for structured inputs

## Output Design

- Every output MUST have a `description` — CI-enforced via tflint
- Outputs MUST reference resource attributes, not input variables (ensures dependency graph)
- Mark sensitive outputs with `sensitive = true`
- Export at least one output per resource created in a module

## Variable Strategy

- No `terraform.tfvars` committed to version control — `.gitignore` enforced
- `terraform.tfvars.example` committed for documentation
- Per-environment values in `environments/{env}/terraform.tfvars.example`
- Sensitive values come from Secrets Manager or SSM Parameter Store (ADR-014 §7), never from `.tfvars` files
- CI injects variables via `TF_VAR_*` environment variables or `-var-file`

## Tagging Strategy

All AWS resources MUST carry these tags — enforced via `default_tags` in the provider block:

| Tag | Value | Purpose |
|-----|-------|---------|
| `Project` | `messaging-platform` | Cost allocation, resource identification |
| `Environment` | `dev`, `staging`, `prod` | Environment identification |
| `ManagedBy` | `terraform` | Distinguishes IaC-managed from manual resources |

```hcl
# environments/dev/providers.tf
provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = var.project_name
      Environment = var.environment
      ManagedBy   = "terraform"
    }
  }
}
```

Additional tags (`Owner`, `CostCenter`) may be added per-resource as needed. Module variables accept a `tags` map that is merged with `default_tags`.

## State Management

**Remote backend:** S3 bucket with DynamoDB locking. Versioning enabled on the bucket for state recovery. Encryption at rest via SSE-S3. Public access blocked.

**State isolation:** Separate state file per environment. State key pattern: `{environment}/terraform.tfstate`.

```hcl
# environments/dev/backend.tf
terraform {
  backend "s3" {
    bucket         = "messaging-platform-terraform-state"
    key            = "dev/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "terraform-locks"
    encrypt        = true
  }
}
```

**Cross-state references:** When one environment root needs outputs from another (rare for this project), use `terraform_remote_state` data sources. Prefer dedicated data sources over remote state for simple lookups.

**Access control:** Restrict S3 bucket access to CI/CD roles and infrastructure admins. Never use root credentials for Terraform operations.

## Versioning & Pinning

**Terraform version:** Bounded range in every root module:

```hcl
terraform {
  required_version = ">= 1.9.0, < 2.0.0"
}
```

**Provider versions:**
- Root modules: pessimistic constraint to minor version — `~> 5.82.0`
- Child modules: broader constraint — `>= 5.0`

```hcl
# modules/*/versions.tf — broad constraint
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

# environments/*/versions.tf — pinned to minor
terraform {
  required_version = ">= 1.9.0, < 2.0.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.82.0"
    }
  }
}
```

**Lock file:** `.terraform.lock.hcl` is committed to version control. Run `terraform init -upgrade` deliberately and review lock file changes in PRs.

## Module Versioning

Terraform modules in `terraform/modules/` are internal and consumed via relative paths — not versioned independently. The source of truth is the current commit on `main`. If modules are later extracted to a shared registry, they will be pinned by semantic version.

External registry modules (if used): always specify `version`. Git-sourced modules: pin to a tag (`?ref=v1.2.3`), never a branch.

## Code Quality

**`for_each` vs `count`:**
- Use `for_each` with maps/sets for multiple resources with distinct identities
- Use `count` only for simple boolean toggles (0 or 1): `count = var.create_resource ? 1 : 0`
- Never use `count > 1` with distinct configurations — index shifts cause cascading resource recreation

**Lifecycle management:**
- `prevent_destroy` on stateful resources (DynamoDB tables, S3 buckets)
- `create_before_destroy` for zero-downtime replacements (security groups, target groups)
- Use `ignore_changes` sparingly and always on specific attributes, never `all`

**Data sources:**
- Use data sources to look up existing infrastructure (AMIs, AZs, caller identity)
- Never hardcode AWS account IDs, AZ names, or region-specific values

**Locals:**
- Use `locals` to name complex expressions and avoid repetition
- Prefer `locals` over repeated inline expressions

## Security

**Provider credentials:** never in code. Hierarchy (most to least preferred):
1. OIDC / Workload Identity Federation (CI/CD)
2. Instance profiles / IAM roles (ECS, EC2)
3. Environment variables (local development)
4. Static credentials in provider block — **NEVER**

**Pre-apply security scanning:** `trivy config .` (successor to tfsec) runs in CI and blocks PRs on high/critical findings. See [CI/CD Pipeline](../CONTRIBUTING.md#cicd-pipeline).

**State file security:** Encrypted at rest (S3 SSE), access restricted to CI/CD roles and admins, never committed to git.

## Validation Tooling

```
make terraform-fmt        # terraform fmt -check -recursive (CI)
make terraform-fmt-fix    # terraform fmt -recursive (local fix)
make terraform-validate   # terraform init -backend=false && terraform validate
make terraform-lint       # tflint --recursive
make terraform-security   # trivy config terraform/
```

All Terraform tooling runs in Docker. No local Terraform installation required.

**tflint configuration** (`.tflint.hcl` at repo root):
- `terraform_naming_convention` — enforces `snake_case`
- `terraform_documented_variables` — requires `description` on all variables
- `terraform_documented_outputs` — requires `description` on all outputs
- `terraform_standard_module_structure` — validates file layout
- AWS ruleset — AWS-specific best practices

## Documentation

Module READMEs are auto-generated via [terraform-docs](https://github.com/terraform-docs/terraform-docs). Each module README includes:

- Module description and purpose
- Usage example
- Auto-generated inputs/outputs tables (via `<!-- BEGIN_TF_DOCS -->` markers)
- Prerequisites and dependencies on other modules

`terraform-docs` runs as a pre-commit hook or CI check. Stale READMEs block PRs.
