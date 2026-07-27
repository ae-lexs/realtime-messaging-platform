// Package adapter contains implementations of interfaces defined in app.
//
// Two families live here. The substrate-neutral ones — Redis rate limiting and
// revocation, log-only SMS — carried over from the AWS build untouched, because
// Memorystore is Redis and the SMS provider was already behind an interface.
// The persistence adapters (firestore_*.go) are the M1.2 re-home: they map
// between the service's records and internal/firestore's documents, and hold no
// logic of their own beyond that mapping. Anything conditional or transactional
// belongs in internal/firestore, where the store can express it.
package adapter

import "go.opentelemetry.io/otel"

var tracer = otel.Tracer("chatmgmt/adapter")
