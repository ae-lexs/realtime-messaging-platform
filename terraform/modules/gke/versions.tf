terraform {
  required_version = ">= 1.9.0, < 2.0.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 6.0"
    }
    # managed_opentelemetry_config is google-beta only (not yet promoted to
    # the GA provider) and landed in the provider after the 6.x line this repo
    # otherwise pins to (hashicorp/terraform-provider-google-beta#11438,
    # 2026-01-21) — see the comment on the cluster resource in main.tf.
    google-beta = {
      source                = "hashicorp/google-beta"
      version               = ">= 7.0"
      configuration_aliases = [google-beta]
    }
  }
}
