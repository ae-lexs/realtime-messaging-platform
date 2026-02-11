package adapter

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snsPublisherStub is a configurable stub for the snsPublisher interface.
type snsPublisherStub struct {
	err error
}

func (s *snsPublisherStub) Publish(_ context.Context, _ *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &sns.PublishOutput{}, nil
}

func TestSNSSMSProvider_SendOTP_Success(t *testing.T) {
	// Arrange
	stub := &snsPublisherStub{}
	provider := NewSNSSMSProvider(stub)

	// Act
	err := provider.SendOTP(context.Background(), "+15551234567", "123456")

	// Assert
	require.NoError(t, err)
}

func TestSNSSMSProvider_SendOTP_Error(t *testing.T) {
	// Arrange
	publishErr := errors.New("sns throttled")
	stub := &snsPublisherStub{err: publishErr}
	provider := NewSNSSMSProvider(stub)

	// Act
	err := provider.SendOTP(context.Background(), "+15551234567", "123456")

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, publishErr)
	assert.Contains(t, err.Error(), "sns sms: send otp")
}

func TestLogSMSProvider_SendOTP(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	provider := NewLogSMSProvider(logger)

	// Act
	err := provider.SendOTP(context.Background(), "+15551234567", "987654")

	// Assert
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "otp delivery (log-only)")
	assert.Contains(t, output, "***4567")
	assert.Contains(t, output, "987654")
	assert.NotContains(t, output, "+15551234567")
}

func TestMaskPhone(t *testing.T) {
	tests := []struct {
		name  string
		phone string
		want  string
	}{
		{
			name:  "standard phone number",
			phone: "+15551234567",
			want:  "***4567",
		},
		{
			name:  "exactly 5 characters",
			phone: "12345",
			want:  "***2345",
		},
		{
			name:  "exactly 4 characters",
			phone: "1234",
			want:  "****",
		},
		{
			name:  "short number",
			phone: "12",
			want:  "****",
		},
		{
			name:  "empty string",
			phone: "",
			want:  "****",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskPhone(tt.phone)
			assert.Equal(t, tt.want, got)
		})
	}
}
