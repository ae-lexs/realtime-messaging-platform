package events_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eventsv1 "github.com/aelexs/realtime-messaging-platform/gen/events/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/events"
)

// TestDescriptorIndexesMatchProtoFile guards the one thing about events.proto
// that cannot be recovered from the schema text: the Confluent wire format
// identifies a message by its declaration index, so inserting or reordering a
// top-level message silently re-points every record already on a topic and in
// the lake. If this fails, the proto was reordered — append instead.
func TestDescriptorIndexesMatchProtoFile(t *testing.T) {
	// Arrange
	messages := eventsv1.File_events_v1_events_proto.Messages()

	for _, d := range events.Descriptors() {
		t.Run(d.Topic, func(t *testing.T) {
			// Act
			require.Greater(t, messages.Len(), d.Index)
			atIndex := messages.Get(d.Index).FullName()

			// Assert
			assert.Equal(t, d.New().ProtoReflect().Descriptor().FullName(), atIndex)
		})
	}
}

func TestSubjectsUseTopicNameStrategy(t *testing.T) {
	// Act
	subjects := events.Subjects()

	// Assert
	assert.Equal(t, []string{
		"messages.persisted-value",
		"memberships.changed-value",
		"chats.created-value",
	}, subjects)
}

func TestDescriptorForTopicUnknown(t *testing.T) {
	// Act
	_, err := events.DescriptorForTopic("messages.deleted")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "messages.deleted")
}
