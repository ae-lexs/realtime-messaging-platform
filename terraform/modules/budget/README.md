# budget

Cumulative-spend Cloud Billing budget scoped to the project (ADR-021 Deployment
Req 4). With no `all_updates_rule`, threshold breaches email the billing
account's admins automatically — no notification channel wiring needed. The
amount is in the billing account's currency (`currency_code` is intentionally
omitted so a mismatched currency can't cause a 400).

## Usage

```hcl
module "budget" {
  source             = "../../modules/budget"
  project_number     = data.google_project.this.number
  billing_account_id = var.billing_account_id
  name_prefix        = "messaging-dev"
  amount_units       = 5000
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
| [google_billing_budget.main](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/billing_budget) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| billing\_account\_id | Billing account ID (XXXXXX-XXXXXX-XXXXXX) that owns the budget. | `string` | n/a | yes |
| name\_prefix | Prefix for the budget display name (e.g. messaging-dev). | `string` | n/a | yes |
| project\_number | GCP project number the budget filter is scoped to (projects/{number}). | `string` | n/a | yes |
| amount\_units | Budget amount in whole units of the billing account's currency (ADR-021 Deployment Req 4). | `number` | `500` | no |
| threshold\_percents | Fractions of the budget at which to alert billing admins (0.0-1.0). | `list(number)` | <pre>[<br/>  0.5,<br/>  0.9,<br/>  1<br/>]</pre> | no |

## Outputs

| Name | Description |
|------|-------------|
| budget\_id | The resource ID of the billing budget. |
| budget\_name | The display name of the billing budget. |
<!-- END_TF_DOCS -->
