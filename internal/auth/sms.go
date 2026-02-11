package auth

import "context"

// SMSProvider abstracts OTP delivery for vendor independence (ADR-015 §2.1).
type SMSProvider interface {
	// SendOTP delivers the OTP to the given phone number.
	// Returns nil on successful delivery acceptance (not necessarily receipt).
	SendOTP(ctx context.Context, phone string, otp string) error
}
