package domain

import "log/slog"

// SecretString wraps sensitive string values.
// Implements slog.LogValuer to prevent accidental logging.
// Implements fmt.Stringer to return redacted value.
// This provides defense-in-depth per ADR-013 and TBD-PR0-1.
type SecretString string

// String returns a redacted placeholder, never the actual value.
// This prevents accidental exposure via fmt.Printf, string concatenation, etc.
func (s SecretString) String() string {
	return "[REDACTED]"
}

// LogValue implements slog.LogValuer to ensure secrets are never logged in plaintext.
// Even if ReplaceAttr is misconfigured or bypassed, this interface ensures protection.
func (s SecretString) LogValue() slog.Value {
	return slog.StringValue("[REDACTED]")
}

// Expose returns the actual secret value.
// Use sparingly - only when the secret must be used (e.g., JWT signing, API calls).
// This method name is intentionally explicit to discourage casual use.
func (s SecretString) Expose() string {
	return string(s)
}

// IsEmpty returns true if the secret is empty.
func (s SecretString) IsEmpty() bool {
	return len(s) == 0
}

// SecretBytes wraps sensitive byte slice values with the same protections as SecretString.
type SecretBytes []byte

// String returns a redacted placeholder.
func (s SecretBytes) String() string {
	return "[REDACTED]"
}

// LogValue implements slog.LogValuer.
func (s SecretBytes) LogValue() slog.Value {
	return slog.StringValue("[REDACTED]")
}

// Expose returns the actual secret bytes.
func (s SecretBytes) Expose() []byte {
	return []byte(s)
}

// IsEmpty returns true if the secret is empty.
func (s SecretBytes) IsEmpty() bool {
	return len(s) == 0
}

// Ensure interfaces are implemented at compile time.
var (
	_ slog.LogValuer = SecretString("")
	_ slog.LogValuer = SecretBytes{}
)
