terraform {
  required_version = ">= 1.9.0, < 2.0.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
    # Only for the gke module's managed_opentelemetry_config — see its
    # versions.tf for why this needs a newer line than the "google" provider
    # above. Configured once here, passed down explicitly.
    google-beta = {
      source  = "hashicorp/google-beta"
      version = ">= 7.0, < 9.0"
    }
  }
}
