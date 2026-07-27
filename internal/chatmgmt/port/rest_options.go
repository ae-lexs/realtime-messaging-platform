package port

import (
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"
)

// ServeMuxOptions returns the grpc-gateway configuration the REST bridge needs.
//
// Both options correct a default whose failure mode is silence — the response
// or the request looks fine and simply lacks something.
func ServeMuxOptions() []runtime.ServeMuxOption {
	return []runtime.ServeMuxOption{
		runtime.WithIncomingHeaderMatcher(IncomingHeaderMatcher),
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				// protojson omits zero values by default, so a bool that is
				// false is not rendered as `false` — it is absent. The live
				// gate caught it on VerifyOTP: a returning user's response
				// carried no is_new_user at all, leaving a client to infer
				// "false" from "missing" and unable to tell that apart from a
				// field the server never set. Emitting unpopulated fields makes
				// the JSON match the proto: every field the message declares is
				// present with its value.
				EmitUnpopulated: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				// A request naming a field this build does not know is a
				// version skew, not a malformed request. Rejecting it would
				// make adding an optional field a breaking change for older
				// servers.
				DiscardUnknown: true,
			},
		}),
	}
}
