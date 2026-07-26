//go:build integration

// The M0.3 gate: encode → register → decode against a live Managed Kafka
// schema registry. Hermetic tests can prove the framing is self-consistent;
// only this one proves the registry accepts the schema, hands back IDs, and
// holds the FULL compatibility the evolution policy depends on.
//
// Run after `make deploy` (the registry does not exist before it):
//
//	PROJECT_ID=... make schema-register
//	SCHEMA_REGISTRY_URL=$(...) SR_TOKEN=$(gcloud auth print-access-token) \
//	  make test-integration

package events_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/aelexs/realtime-messaging-platform/gen/events/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/events"
)

func TestRegistryRoundTrip(t *testing.T) {
	// Arrange
	url, token := os.Getenv("SCHEMA_REGISTRY_URL"), os.Getenv("SR_TOKEN")
	if url == "" || token == "" {
		t.Skip("set SCHEMA_REGISTRY_URL and SR_TOKEN to run against a live registry")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := events.NewRegistryClient(url, token)
	require.NoError(t, err)

	// Act — register every schema, then encode and decode through the IDs the
	// registry assigned.
	published, err := events.Publish(ctx, client, repoRoot)
	require.NoError(t, err)

	serde, err := events.NewSerde(events.SchemaIDs(published))
	require.NoError(t, err)

	event := &eventsv1.MessagePersisted{
		Meta: &eventsv1.EnvelopeMeta{
			EventId:       "01JZ8P7Q9V0000000000000000",
			EventType:     "MessagePersisted",
			SchemaVersion: events.SchemaVersion,
			OccurredAt:    timestamppb.Now(),
			ProducerId:    "schemactl-integration",
			TraceId:       "4bf92f3577b34da6a3ce929d0e0e4736",
		},
		ChatId:       "chat-1",
		Sequence:     1,
		SenderId:     "user-1",
		ContentBytes: 11,
		ContentType:  "text/plain",
	}

	encoded, err := serde.Encode(event)
	require.NoError(t, err)

	decoded := &eventsv1.MessagePersisted{}
	require.NoError(t, serde.Decode(encoded, decoded))

	// Assert
	assert.True(t, proto.Equal(event, decoded))

	require.Len(t, published, len(events.Sources()))
	for _, state := range published {
		assert.Positive(t, state.ID, "%s must have a registry-assigned ID", state.Subject)
	}

	// Every subject reads back as FULL — the policy is on the registry, not
	// only in CI.
	inspected, err := events.Inspect(ctx, client)
	require.NoError(t, err)
	require.Len(t, inspected, len(events.Sources()))
	for _, state := range inspected {
		assert.Equal(t, events.Compatibility.String(), state.Compatibility, state.Subject)
	}

	// Registration is idempotent: identical text must not mint new IDs.
	republished, err := events.Publish(ctx, client, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, events.SchemaIDs(published), events.SchemaIDs(republished))
}
