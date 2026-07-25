package adapter

import (
	"context"
	"log/slog"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
)

// Compile-time interface satisfaction check.
var _ auth.SMSProvider = (*LogSMSProvider)(nil)

// LogSMSProvider is a fake SMSProvider (per 07_TESTING_PHILOSOPHY) that logs
// OTP delivery instead of sending real SMS. Suitable for local development
// and testing environments. It is the substrate-neutral SMS adapter salvaged
// from the AWS build; the production provider (Firestore/GCP) arrives with the
// auth re-home in M1.2.
type LogSMSProvider struct {
	logger *slog.Logger
}

// NewLogSMSProvider creates a LogSMSProvider that writes OTP events to the
// given structured logger.
func NewLogSMSProvider(logger *slog.Logger) *LogSMSProvider {
	return &LogSMSProvider{logger: logger}
}

// SendOTP logs the OTP delivery with a masked phone number (last 4 digits visible).
// It never sends a real SMS.
func (p *LogSMSProvider) SendOTP(ctx context.Context, phone, otp string) error {
	masked := maskPhone(phone)

	p.logger.InfoContext(ctx, "otp delivery (log-only)",
		slog.String("phone", masked),
		slog.String("otp", otp),
	)

	return nil
}

// maskPhone returns a masked representation of the phone number showing only
// the last 4 digits. Numbers shorter than 5 characters are fully masked.
func maskPhone(phone string) string {
	if len(phone) <= 4 {
		return "****"
	}
	return "***" + phone[len(phone)-4:]
}
