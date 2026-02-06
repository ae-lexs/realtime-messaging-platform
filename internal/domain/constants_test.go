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
		{name: "text is valid", ct: "text", want: true},
		{name: "empty is invalid", ct: "", want: false},
		{name: "image is invalid", ct: "image", want: false},
		{name: "TEXT is invalid (case-sensitive)", ct: "TEXT", want: false},
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
		{name: "direct is valid", ct: "direct", want: true},
		{name: "group is valid", ct: "group", want: true},
		{name: "empty is invalid", ct: "", want: false},
		{name: "channel is invalid", ct: "channel", want: false},
		{name: "Direct is invalid (case-sensitive)", ct: "Direct", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsValidChatType(tt.ct)

			assert.Equal(t, tt.want, got)
		})
	}
}
