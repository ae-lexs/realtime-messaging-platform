package events_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/aelexs/realtime-messaging-platform/gen/events/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/events"
)

// Schema IDs the registry would have assigned. Values are arbitrary here — what
// matters is that the ID travels in the header and selects the right type.
var testIDs = map[string]int{
	"messages.persisted-value":  11,
	"memberships.changed-value": 22,
	"chats.created-value":       33,
}

func newTestSerde(t *testing.T) *sr.Serde {
	t.Helper()

	serde, err := events.NewSerde(testIDs)
	require.NoError(t, err)
	return serde
}

func TestSerdeRoundTrip(t *testing.T) {
	meta := &eventsv1.EnvelopeMeta{
		EventId:       "01JZ8P7Q9V0000000000000000",
		SchemaVersion: events.SchemaVersion,
		OccurredAt:    timestamppb.New(timestamppb.Now().AsTime()),
		ProducerId:    "ingest-7d9f",
		TraceId:       "4bf92f3577b34da6a3ce929d0e0e4736",
	}

	tests := []struct {
		name     string
		topic    string
		event    proto.Message
		decodeTo proto.Message
	}{
		{
			name:  "message persisted",
			topic: events.TopicMessagesPersisted,
			event: &eventsv1.MessagePersisted{
				Meta:         proto.Clone(meta).(*eventsv1.EnvelopeMeta),
				ChatId:       "chat-1",
				Sequence:     42,
				SenderId:     "user-1",
				ContentBytes: 11,
				ContentType:  "text/plain",
			},
			decodeTo: &eventsv1.MessagePersisted{},
		},
		{
			name:  "membership changed",
			topic: events.TopicMembershipsChanged,
			event: &eventsv1.MembershipChanged{
				Meta:   proto.Clone(meta).(*eventsv1.EnvelopeMeta),
				ChatId: "chat-1",
				UserId: "user-2",
				Change: eventsv1.MembershipChanged_CHANGE_ADDED,
			},
			decodeTo: &eventsv1.MembershipChanged{},
		},
		{
			name:  "chat created",
			topic: events.TopicChatsCreated,
			event: &eventsv1.ChatCreated{
				Meta:      proto.Clone(meta).(*eventsv1.EnvelopeMeta),
				ChatId:    "chat-1",
				CreatorId: "user-1",
			},
			decodeTo: &eventsv1.ChatCreated{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			serde := newTestSerde(t)
			d, err := events.DescriptorForTopic(tt.topic)
			require.NoError(t, err)

			// Act
			encoded, err := serde.Encode(tt.event)
			require.NoError(t, err)
			err = serde.Decode(encoded, tt.decodeTo)

			// Assert
			require.NoError(t, err)
			assert.True(t, proto.Equal(tt.event, tt.decodeTo), "round-trip must preserve the event")

			// The header is the contract the BigQuery sink reads (ADR-022 D2):
			// magic byte, then the schema ID big-endian, then the index.
			require.Greater(t, len(encoded), 5)
			assert.Equal(t, byte(0), encoded[0], "magic byte")
			assert.Equal(t, uint32(testIDs[d.Subject()]), binary.BigEndian.Uint32(encoded[1:5]))

			id, rest, err := serde.DecodeID(encoded)
			require.NoError(t, err)
			assert.Equal(t, testIDs[d.Subject()], id)

			// One message per schema, so the index is always 0 — the
			// framing's single-zero-byte shortcut.
			index, _, err := serde.DecodeIndex(rest, 1)
			require.NoError(t, err)
			assert.Equal(t, []int{0}, index)
		})
	}
}

func TestSerdeDecodeNew(t *testing.T) {
	// Arrange — a consumer that does not know the type ahead of time.
	serde := newTestSerde(t)
	event := &eventsv1.ChatCreated{ChatId: "chat-1", CreatorId: "user-1"}
	encoded, err := serde.Encode(event)
	require.NoError(t, err)

	// Act
	decoded, err := serde.DecodeNew(encoded)

	// Assert
	require.NoError(t, err)
	require.IsType(t, &eventsv1.ChatCreated{}, decoded)
	assert.True(t, proto.Equal(event, decoded.(proto.Message)))
}

func TestSerdeRejectsWrongType(t *testing.T) {
	// Arrange — bytes encoded as MessagePersisted...
	serde := newTestSerde(t)
	encoded, err := serde.Encode(&eventsv1.MessagePersisted{ChatId: "chat-1", Sequence: 1})
	require.NoError(t, err)

	// Act — ...decoded as if they were a ChatCreated.
	err = serde.Decode(encoded, &eventsv1.ChatCreated{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema is events.v1.MessagePersisted")
}

func TestSerdeRejectsUnknownSchemaID(t *testing.T) {
	// Arrange — a record produced against a registry we do not know.
	serde := newTestSerde(t)
	unknown := []byte{0, 0, 0, 0, 99, 0}

	// Act
	err := serde.Decode(unknown, &eventsv1.ChatCreated{})

	// Assert
	require.ErrorIs(t, err, sr.ErrNotRegistered)
}

func TestNewSerdeRequiresEveryTopic(t *testing.T) {
	// Arrange — a half-published registry.
	partial := map[string]int{"messages.persisted-value": 11}

	// Act
	serde, err := events.NewSerde(partial)

	// Assert — fail at wiring time, not at produce time.
	require.Error(t, err)
	assert.Nil(t, serde)
	assert.Contains(t, err.Error(), "memberships.changed-value")
}
