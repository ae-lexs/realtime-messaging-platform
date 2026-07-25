// Package adapter contains implementations of interfaces defined in app.
// Substrate-neutral adapters (Redis rate limiting / revocation, log-only SMS)
// live here; the persistence adapters (Firestore) arrive with the auth
// re-home in M1.2.
package adapter

import "go.opentelemetry.io/otel"

var tracer = otel.Tracer("chatmgmt/adapter")
