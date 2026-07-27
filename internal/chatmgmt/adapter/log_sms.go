package adapter

import (
	"context"
	"log/slog"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
)

// Compile-time interface satisfaction check.
var _ auth.SMSProvider = (*LogSMSProvider)(nil)

// LogSMSProvider writes the OTP to the structured log instead of sending it.
// It is the only SMSProvider implementation, in every environment.
//
// That is a decision, not a gap. ADR-015 §2.2 chose Amazon SNS because it was
// native to the substrate and needed no vendor relationship — and GCP has no
// first-party SMS service at all, so the reasoning that picked SNS argues
// against any GCP-native successor. Delivering a real SMS means a third-party
// provider (Twilio and peers) behind this same interface, which is a small
// adapter and a large amount of A2P sender registration. Recorded as a non-goal
// in ADR-015 v1.3 and the execution plan rather than left looking unfinished.
//
// **This must not survive contact with real users.** It logs the OTP in full —
// the phone number is masked, the code is not — into Cloud Logging, where it is
// retained and readable by anyone with log-viewer. Harmless for a
// deploy-and-destroy lab whose only reader is the M1.2 flow gate; an account
// takeover the moment someone real is on the other end. Wiring a live provider
// means removing this from deployed environments in the same change.
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
