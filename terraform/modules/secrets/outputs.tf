output "signing_key_id" {
  description = "The active JWT key ID; scripts/auth-keys.sh needs it to know which secrets to fill."
  value       = var.signing_key_id
}

output "signing_key_secret_id" {
  description = "Secret ID holding the RSA private key (PEM). Created empty; filled by scripts/auth-keys.sh."
  value       = google_secret_manager_secret.auth["signing_key"].secret_id
}

output "public_key_secret_id" {
  description = "Secret ID holding the RSA public key (PEM). Listing by the jwt-public-key- prefix is how token-validating services discover acceptable `kid` values."
  value       = google_secret_manager_secret.auth["public_key"].secret_id
}

output "current_key_id_secret_id" {
  description = "Secret ID naming the active signing key. Read at startup to resolve which private key to load."
  value       = google_secret_manager_secret.auth["current_key_id"].secret_id
}

output "otp_pepper_secret_id" {
  description = "Secret ID holding the OTP HMAC pepper (ADR-015 §1.2)."
  value       = google_secret_manager_secret.auth["otp_pepper"].secret_id
}
