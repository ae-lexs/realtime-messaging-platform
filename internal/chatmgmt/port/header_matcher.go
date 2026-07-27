package port

import (
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

// forwardedHeaders are the HTTP headers the auth handler reads out of gRPC
// metadata. Every one of them is dropped or renamed by grpc-gateway's default
// matcher, so they are listed explicitly.
var forwardedHeaders = map[string]struct{}{
	// Carries the bearer token on refresh and logout. The default matcher does
	// forward it — but renamed to grpcgateway-authorization, because
	// Authorization is on the IANA permanent-header list and permanent headers
	// get the grpcgateway- prefix. AuthHandler reads "authorization".
	"authorization": {},

	// The device binding a session is issued against (ADR-015 §4.2). Not a
	// permanent header and not Grpc-Metadata- prefixed, so the default matcher
	// drops it outright.
	"x-device-id": {},

	// The client IP used for the per-IP OTP rate limit (ADR-013 §4.1). Also
	// dropped by default; without it every REST request falls back to the peer
	// address, which for in-process grpc-gateway is not the client at all.
	"x-forwarded-for": {},
}

// IncomingHeaderMatcher maps HTTP headers onto gRPC metadata keys for the
// REST-to-gRPC bridge.
//
// It exists because every failure the default matcher causes here is silent.
// Refresh would see an empty device ID and reject every request with
// DEVICE_MISMATCH; logout would see no bearer token and reject with
// UNAUTHORIZED; the per-IP rate limit would key every caller under the same
// non-client address. None of those look like a header-plumbing problem from
// the outside, which is why this is a named, tested function rather than a
// closure in the composition root.
func IncomingHeaderMatcher(key string) (string, bool) {
	lower := strings.ToLower(key)
	if _, ok := forwardedHeaders[lower]; ok {
		return lower, true
	}
	return runtime.DefaultHeaderMatcher(key)
}
