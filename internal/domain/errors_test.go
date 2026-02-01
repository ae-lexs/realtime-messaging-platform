package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"ErrUnavailable", domain.ErrUnavailable, true},
		{"ErrRateLimited", domain.ErrRateLimited, true},
		{"ErrNotFound", domain.ErrNotFound, false},
		{"ErrUnauthorized", domain.ErrUnauthorized, false},
		{"wrapped ErrUnavailable", fmt.Errorf("context: %w", domain.ErrUnavailable), true},
		{"random error", errors.New("something else"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsRetryable(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsClientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"ErrInvalidInput", domain.ErrInvalidInput, true},
		{"ErrMessageTooLarge", domain.ErrMessageTooLarge, true},
		{"ErrInvalidContentType", domain.ErrInvalidContentType, true},
		{"ErrNotFound", domain.ErrNotFound, true},
		{"ErrNotMember", domain.ErrNotMember, true},
		{"ErrForbidden", domain.ErrForbidden, true},
		{"ErrUnauthorized", domain.ErrUnauthorized, true},
		{"ErrEmptyID", domain.ErrEmptyID, true},
		{"ErrInvalidID", domain.ErrInvalidID, true},
		{"ErrUnavailable", domain.ErrUnavailable, false},
		{"ErrRateLimited", domain.ErrRateLimited, false},
		{"wrapped ErrNotFound", fmt.Errorf("context: %w", domain.ErrNotFound), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsClientError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsPermissionDenied(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"ErrForbidden", domain.ErrForbidden, true},
		{"ErrNotMember", domain.ErrNotMember, true},
		{"ErrUnauthorized", domain.ErrUnauthorized, true},
		{"ErrNotFound", domain.ErrNotFound, false},
		{"wrapped ErrForbidden", fmt.Errorf("user %s: %w", "123", domain.ErrForbidden), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsPermissionDenied(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"ErrNotFound", domain.ErrNotFound, true},
		{"ErrForbidden", domain.ErrForbidden, false},
		{"wrapped ErrNotFound", fmt.Errorf("chat %s: %w", "123", domain.ErrNotFound), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsNotFound(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}
