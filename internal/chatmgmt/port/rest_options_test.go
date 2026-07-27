package port

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
)

// marshalerFor pulls the configured marshaler back out of a mux built with
// ServeMuxOptions, so the test exercises what the service actually installs
// rather than a hand-built copy of it.
func marshalerFor(t *testing.T) runtime.Marshaler {
	t.Helper()

	mux := runtime.NewServeMux(ServeMuxOptions()...)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/auth/otp/verify", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	_, marshaler := runtime.MarshalerForRequest(mux, req)
	return marshaler
}

// TestFalseBoolsAreEmitted is the assertion behind EmitUnpopulated, and it
// covers a response shape the live gate found wrong.
//
// protojson omits zero values by default, so a returning user's VerifyOTP
// response contained no is_new_user field at all. A client then has to read
// "absent" as "false" — and cannot distinguish that from a field the server
// simply never populated, which is exactly the ambiguity a typed API exists to
// remove.
func TestFalseBoolsAreEmitted(t *testing.T) {
	// Arrange — a returning user: is_new_user is false, not unset.
	resp := &messagingv1.VerifyOTPResponse{
		SessionId: "session-1",
		IsNewUser: false,
	}

	// Act
	data, err := marshalerFor(t).Marshal(resp)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, string(data), "isNewUser",
		"a false bool must appear in the JSON, not vanish from it")
}

// TestUnknownRequestFieldsAreIgnored covers DiscardUnknown: a client sending a
// field this build does not know is version skew, and rejecting it would make
// adding an optional field a breaking change for older servers.
func TestUnknownRequestFieldsAreIgnored(t *testing.T) {
	// Arrange
	body := []byte(`{"phone_number":"+525512345678","some_future_field":"value"}`)
	var req messagingv1.RequestOTPRequest

	// Act
	err := marshalerFor(t).Unmarshal(body, &req)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "+525512345678", req.GetPhoneNumber())
}

// TestServeMuxOptionsIncludesTheHeaderMatcher keeps the two REST corrections
// travelling together: a mux built without ServeMuxOptions would silently lose
// the Authorization and X-Device-Id plumbing as well as the marshaling fix.
func TestServeMuxOptionsIncludesTheHeaderMatcher(t *testing.T) {
	assert.Len(t, ServeMuxOptions(), 2)
}
