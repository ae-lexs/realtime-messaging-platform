// Package events holds the Kafka event wire contract (ADR-022 D1): which
// Protobuf type belongs on which topic, under which schema-registry subject,
// and which .proto files back them.
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

	// Topics per ADR-011; all three are partitioned by chat_id.
	TopicMessagesPersisted  = "messages.persisted"
	TopicMembershipsChanged = "memberships.changed"
	TopicChatsCreated       = "chats.created"

	// subjectSuffix is Confluent's TopicNameStrategy: the value schema of
	// topic T is registered under subject "T-value".
	subjectSuffix = "-value"

	// Subjects for the two schemas that are imported rather than produced.
	// Neither may start with "google" — the registry rejects such subject
	// names outright ("not in valid format").
	SubjectTimestamp = "wkt.timestamp"
	SubjectEnvelope  = "events.v1.envelope"
)

// SchemaSource is one .proto file as the registry sees it: a subject, the text
// to register, and the direct imports that must already be registered so they
// can be passed as references.
//
// The registry does not resolve imports on its own — not even the Protobuf
// well-known types — so google/protobuf/timestamp.proto is vendored verbatim
// under proto/third_party and registered like any other schema.
type SchemaSource struct {
	// Subject the schema is registered under.
	Subject string

	// Path is the import path other schemas use to reference this one, and the
	// `name` of the reference entry.
	Path string

	// File is the repository-relative source to read.
	File string

	// Imports lists the direct imports of File, by their Path. Transitive
	// imports resolve through the referenced schema's own references.
	Imports []string
}

// Sources returns every schema to register, in dependency order: a schema must
// exist before anything that imports it.
func Sources() []SchemaSource {
	return []SchemaSource{
		{
			Subject: SubjectTimestamp,
			Path:    "google/protobuf/timestamp.proto",
			File:    "proto/third_party/google/protobuf/timestamp.proto",
		},
		{
			Subject: SubjectEnvelope,
			Path:    "events/v1/envelope.proto",
			File:    "proto/events/v1/envelope.proto",
			Imports: []string{"google/protobuf/timestamp.proto"},
		},
		{
			Subject: TopicMessagesPersisted + subjectSuffix,
			Path:    "events/v1/message_persisted.proto",
			File:    "proto/events/v1/message_persisted.proto",
			Imports: []string{"events/v1/envelope.proto"},
		},
		{
			Subject: TopicMembershipsChanged + subjectSuffix,
			Path:    "events/v1/membership_changed.proto",
			File:    "proto/events/v1/membership_changed.proto",
			Imports: []string{"events/v1/envelope.proto"},
		},
		{
			Subject: TopicChatsCreated + subjectSuffix,
			Path:    "events/v1/chat_created.proto",
			File:    "proto/events/v1/chat_created.proto",
			Imports: []string{"events/v1/envelope.proto"},
		},
	}
}

// Descriptor binds a topic to the Protobuf type carried on it.
type Descriptor struct {
	// Topic is the Kafka topic name.
	Topic string

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
		{Topic: TopicMessagesPersisted, New: func() proto.Message { return &eventsv1.MessagePersisted{} }},
		{Topic: TopicMembershipsChanged, New: func() proto.Message { return &eventsv1.MembershipChanged{} }},
		{Topic: TopicChatsCreated, New: func() proto.Message { return &eventsv1.ChatCreated{} }},
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
