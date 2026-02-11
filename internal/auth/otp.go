package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
)

var otpMax = big.NewInt(1_000_000) // 10^6 for 6-digit OTP

// GenerateOTP generates a cryptographically random 6-digit OTP.
// Uses crypto/rand with rejection sampling (via big.Int) to avoid modulo bias.
// The OTP is zero-padded (e.g., "000123") per ADR-015 §1.2.
func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, otpMax)
	if err != nil {
		return "", fmt.Errorf("generate OTP: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// HashPhone returns the SHA-256 hex digest of an E.164 phone number.
// Used as the partition key in the otp_requests table (ADR-015 §1.1).
func HashPhone(phone string) string {
	h := sha256.Sum256([]byte(phone))
	return hex.EncodeToString(h[:])
}

// ComputeOTPMAC computes HMAC-SHA256(pepper, otp || phoneHash || expiresAt)
// as specified in ADR-015 §1.2. The MAC binds the OTP to the specific
// request context (phone and expiry window).
func ComputeOTPMAC(pepper []byte, otp, phoneHash, expiresAt string) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(otp))
	mac.Write([]byte(phoneHash))
	mac.Write([]byte(expiresAt))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyOTPMAC verifies an OTP candidate against a stored MAC using
// constant-time comparison to prevent timing side-channels (ADR-015 §1.4).
func VerifyOTPMAC(pepper []byte, otpCandidate, phoneHash, expiresAt, storedMAC string) bool {
	candidateMAC := ComputeOTPMAC(pepper, otpCandidate, phoneHash, expiresAt)
	return subtle.ConstantTimeCompare([]byte(candidateMAC), []byte(storedMAC)) == 1
}
