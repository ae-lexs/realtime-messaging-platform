package events

import (
	"fmt"

	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// messageIndex is the index of the encoded type among the top-level messages
// of its schema. Every event schema holds exactly one message — the registry
// rejects any other layout — so the index is always 0, which the Confluent
// framing encodes as a single zero byte.
const messageIndex = 0

// NewSerde builds the encoder/decoder for the Confluent wire format:
//
//	magic byte 0x00 | schema ID (4 bytes, big endian) | message index | payload
//
// The BigQuery Sink V2 connector (ADR-022 D2) reads exactly these bytes, so the
// framing is the contract, not an implementation detail. Schema IDs are
// assigned by the registry at registration time, which is why they are passed
// in rather than computed: an ID is only meaningful against the registry that
// issued it.
//
// Every event topic must have an ID; a partially-registered Serde would fail at
// produce time, in production, instead of here at wiring time.
func NewSerde(idsBySubject map[string]int) (*sr.Serde, error) {
	serde := sr.NewSerde(sr.Header(&sr.ConfluentHeader{}))

	for _, d := range Descriptors() {
		id, ok := idsBySubject[d.Subject()]
		if !ok {
			return nil, fmt.Errorf("events: no schema ID for subject %q", d.Subject())
		}

		want := d.New().ProtoReflect().Descriptor()
		serde.Register(id, d.New(),
			sr.Index(messageIndex),
			sr.EncodeFn(encodeFor(want)),
			sr.DecodeFn(decodeFor(want)),
			sr.GenerateFn(func() any { return d.New() }),
		)
	}

	return serde, nil
}

// encodeFor marshals v, rejecting anything that is not the expected message.
func encodeFor(want protoreflect.MessageDescriptor) func(any) ([]byte, error) {
	return func(v any) ([]byte, error) {
		msg, err := assertType(v, want)
		if err != nil {
			return nil, err
		}

		b, err := proto.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("events: marshal %s: %w", want.FullName(), err)
		}
		return b, nil
	}
}

// decodeFor unmarshals into v. The schema ID in the header selects this
// function, so a caller passing a different message type is decoding a record
// as something it is not — caught here rather than left to whatever the field
// numbers happen to collide into.
func decodeFor(want protoreflect.MessageDescriptor) func([]byte, any) error {
	return func(b []byte, v any) error {
		msg, err := assertType(v, want)
		if err != nil {
			return err
		}

		if err := proto.Unmarshal(b, msg); err != nil {
			return fmt.Errorf("events: unmarshal %s: %w", want.FullName(), err)
		}
		return nil
	}
}

func assertType(v any, want protoreflect.MessageDescriptor) (proto.Message, error) {
	msg, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("events: %T is not a proto.Message", v)
	}
	if got := msg.ProtoReflect().Descriptor(); got.FullName() != want.FullName() {
		return nil, fmt.Errorf("events: schema is %s, got %s", want.FullName(), got.FullName())
	}
	return msg, nil
}
