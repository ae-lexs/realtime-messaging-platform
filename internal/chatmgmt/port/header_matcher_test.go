package port

import (
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncomingHeaderMatcherForwardsWhatAuthReads pins the three headers the
// handler extracts from metadata against the keys it extracts them by. Get one
// wrong and nothing fails loudly — refresh rejects every request as a device
// mismatch, or the per-IP rate limit buckets everyone together.
func TestIncomingHeaderMatcherForwardsWhatAuthReads(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "bearer token", header: "Authorization", want: "authorization"},
		{name: "device binding", header: "X-Device-Id", want: "x-device-id"},
		{name: "client IP", header: "X-Forwarded-For", want: "x-forwarded-for"},
		{name: "already lowercase", header: "authorization", want: "authorization"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := IncomingHeaderMatcher(tt.header)

			require.True(t, ok, "%s must reach gRPC metadata", tt.header)
			assert.Equal(t, tt.want, key)
		})
	}
}

// TestIncomingHeaderMatcherDiffersFromTheDefault states the reason this
// function exists at all. If grpc-gateway's default ever started forwarding
// these under the keys the handler reads, this test fails and the override can
// go — until then it is documenting a real gap, not a preference.
func TestIncomingHeaderMatcherDiffersFromTheDefault(t *testing.T) {
	t.Run("default renames Authorization", func(t *testing.T) {
		// Authorization is an IANA permanent header, so the default forwards
		// it under a grpcgateway- prefix the handler does not look for.
		key, ok := runtime.DefaultHeaderMatcher("Authorization")

		require.True(t, ok)
		assert.NotEqual(t, "authorization", key)
	})

	t.Run("default drops the custom headers", func(t *testing.T) {
		for _, header := range []string{"X-Device-Id", "X-Forwarded-For"} {
			_, ok := runtime.DefaultHeaderMatcher(header)
			assert.False(t, ok, "%s is dropped by the default matcher", header)
		}
	})
}

// TestIncomingHeaderMatcherDelegatesEverythingElse keeps this from becoming an
// allowlist: anything the default would forward still gets forwarded.
func TestIncomingHeaderMatcherDelegatesEverythingElse(t *testing.T) {
	t.Run("Grpc-Metadata- prefixed headers still pass through", func(t *testing.T) {
		key, ok := IncomingHeaderMatcher("Grpc-Metadata-Trace-Id")

		require.True(t, ok)
		assert.Equal(t, "Trace-Id", key)
	})

	t.Run("unrelated headers are still dropped", func(t *testing.T) {
		_, ok := IncomingHeaderMatcher("X-Some-Client-Header")

		assert.False(t, ok)
	})
}
