package domain_test

import (
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestIsValidContentType(t *testing.T) {
	tests := []struct {
		name string
		ct   domain.ContentType
		want bool
	}{
		{"text is valid", "text", true},
		{"empty is invalid", "", false},
		{"image is invalid", "image", false},
		{"TEXT is invalid (case-sensitive)", "TEXT", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsValidContentType(tt.ct)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidChatType(t *testing.T) {
	tests := []struct {
		name string
		ct   domain.ChatType
		want bool
	}{
		{"direct is valid", "direct", true},
		{"group is valid", "group", true},
		{"empty is invalid", "", false},
		{"channel is invalid", "channel", false},
		{"Direct is invalid (case-sensitive)", "Direct", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsValidChatType(tt.ct)

			assert.Equal(t, tt.want, got)
		})
	}
}
