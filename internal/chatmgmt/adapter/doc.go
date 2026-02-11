// Package adapter contains implementations of interfaces defined in app.
// DynamoDB and Kafka event adapters live here.
package adapter

import "go.opentelemetry.io/otel"

var tracer = otel.Tracer("chatmgmt/adapter")
