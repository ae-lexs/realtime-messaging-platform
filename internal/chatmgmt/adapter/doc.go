// Package adapter contains implementations of interfaces defined in app.
//
// Two families live here. The Redis adapters — rate limiting and revocation —
// speak to Memorystore through internal/redis and need nothing cloud-specific,
// as does the log-only SMS provider behind auth.SMSProvider. The persistence
// adapters (firestore_*.go) map between the service's records and
// internal/firestore's documents and hold no logic of their own beyond that
// mapping: anything conditional or transactional belongs in internal/firestore,
// where the store can express it atomically.
package adapter

import "go.opentelemetry.io/otel"

var tracer = otel.Tracer("chatmgmt/adapter")
