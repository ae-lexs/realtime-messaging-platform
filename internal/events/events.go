// Package events holds the Kafka event wire contract (ADR-022 D1): which
// Protobuf type belongs on which topic, under which schema-registry subject,
// and at which index inside the shared events/v1 schema.
//
// It is a leaf package — it depends on the generated types and the schema
// registry client, never on a service's app/port/adapter layers — so the
// Ingest outbox relay (M2.3), the Fanout consumer (M4.1), and the schema
// publish flow all bind to one table instead of three copies of it.
package events

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/aelexs/realtime-messaging-platform/gen/events/v1"
)

const (
	// SchemaVersion is the semver stamped into EnvelopeMeta.schema_version
	// (ADR-022 D1 N1). It tracks proto/events/v1, not the service version.
	SchemaVersion = "1.0.0"

	// DefaultSchemaPath is the schema text registered for every subject,
	// relative to the repository root. The registry stores the .proto source,
	// so registration reads this file rather than reconstructing it from the
	// compiled descriptor.
	DefaultSchemaPath = "proto/events/v1/events.proto"

	// Topics per ADR-011; all three are partitioned by chat_id.
	TopicMessagesPersisted  = "messages.persisted"
	TopicMembershipsChanged = "memberships.changed"
	TopicChatsCreated       = "chats.created"

	// subjectSuffix is Confluent's TopicNameStrategy: the value schema of
	// topic T is registered under subject "T-value".
	subjectSuffix = "-value"
)

// Descriptor binds a topic to the Protobuf type carried on it.
type Descriptor struct {
	// Topic is the Kafka topic name.
	Topic string

	// Index is the type's declaration index among the top-level messages of
	// events.proto. The Confluent wire format identifies a message inside a
	// multi-message schema by this index, so it is part of the contract:
	// reordering the file silently re-points every encoded record. Guarded by
	// TestDescriptorIndexesMatchProtoFile.
	Index int

	// New returns a zero value of the type, used both as the Serde's type
	// exemplar and to allocate decode targets.
	New func() proto.Message
}

// Subject is the schema-registry subject holding this topic's value schema.
func (d Descriptor) Subject() string { return d.Topic + subjectSuffix }

// Descriptors returns every event type carried on a topic. EnvelopeMeta is
// absent by design: it is embedded in the others, never a topic value.
func Descriptors() []Descriptor {
	return []Descriptor{
		{
			Topic: TopicMessagesPersisted,
			Index: 1,
			New:   func() proto.Message { return &eventsv1.MessagePersisted{} },
		},
		{
			Topic: TopicMembershipsChanged,
			Index: 2,
			New:   func() proto.Message { return &eventsv1.MembershipChanged{} },
		},
		{
			Topic: TopicChatsCreated,
			Index: 3,
			New:   func() proto.Message { return &eventsv1.ChatCreated{} },
		},
	}
}

// Subjects returns the registry subjects for all event topics, in the order
// Descriptors returns them.
func Subjects() []string {
	ds := Descriptors()
	subjects := make([]string, 0, len(ds))
	for _, d := range ds {
		subjects = append(subjects, d.Subject())
	}
	return subjects
}

// DescriptorForTopic looks up the event type carried on topic.
func DescriptorForTopic(topic string) (Descriptor, error) {
	for _, d := range Descriptors() {
		if d.Topic == topic {
			return d, nil
		}
	}
	return Descriptor{}, fmt.Errorf("events: no event type registered for topic %q", topic)
}
