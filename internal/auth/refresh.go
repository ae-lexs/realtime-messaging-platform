package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const refreshTokenBytes = 32

// GenerateRefreshToken generates a cryptographically random refresh token
// as a base64url-encoded string (43 characters) per ADR-015 §4.1.
func GenerateRefreshToken() (string, error) {
	b := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashRefreshToken returns the SHA-256 hex digest of a refresh token.
// Only the hash is stored server-side (ADR-015 §4.1).
func HashRefreshToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ValidateRefreshHash verifies a refresh token against its stored hash
// using constant-time comparison.
func ValidateRefreshHash(token, storedHash string) bool {
	candidateHash := HashRefreshToken(token)
	return subtle.ConstantTimeCompare([]byte(candidateHash), []byte(storedHash)) == 1
}
