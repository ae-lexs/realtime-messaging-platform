package adapter

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/service/sns"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
)

// snsPublisher is a narrow, consumer-defined interface for the subset of SNS
// operations required by the SMS provider. The real *sns.Client satisfies it.
type snsPublisher interface {
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
}

// Compile-time interface satisfaction checks.
var _ auth.SMSProvider = (*SNSSMSProvider)(nil)
var _ auth.SMSProvider = (*LogSMSProvider)(nil)

// SNSSMSProvider delivers OTP codes via Amazon SNS SMS.
type SNSSMSProvider struct {
	client snsPublisher
}

// NewSNSSMSProvider creates an SNSSMSProvider backed by the given SNS client.
func NewSNSSMSProvider(client snsPublisher) *SNSSMSProvider {
	return &SNSSMSProvider{client: client}
}

// SendOTP publishes an OTP message to the given phone number via SNS.
func (p *SNSSMSProvider) SendOTP(ctx context.Context, phone, otp string) error {
	message := fmt.Sprintf("Your verification code is: %s", otp)

	_, err := p.client.Publish(ctx, &sns.PublishInput{
		PhoneNumber: &phone,
		Message:     &message,
	})
	if err != nil {
		return fmt.Errorf("sns sms: send otp to %s: %w", phone, err)
	}

	return nil
}

// LogSMSProvider is a fake SMSProvider (per 07_TESTING_PHILOSOPHY) that logs
// OTP delivery instead of sending real SMS. Suitable for local development
// and testing environments.
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
