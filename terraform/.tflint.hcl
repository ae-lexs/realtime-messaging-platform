# tflint configuration for Realtime Messaging Platform
# See: https://github.com/terraform-linters/tflint/blob/master/docs/user-guide/config.md

config {
  call_module_type = "local"
}

# Enforce snake_case naming convention
rule "terraform_naming_convention" {
  enabled = true
}

# Require description on all variables
rule "terraform_documented_variables" {
  enabled = true
}

# Require description on all outputs
rule "terraform_documented_outputs" {
  enabled = true
}

# Validate standard module structure (main.tf, variables.tf, outputs.tf)
rule "terraform_standard_module_structure" {
  enabled = true
}

# Require type on all variables
rule "terraform_typed_variables" {
  enabled = true
}

# Google plugin for GCP-specific best practices
plugin "google" {
  enabled = true
  version = "0.31.0"
  source  = "github.com/terraform-linters/tflint-ruleset-google"
}
