package events

import (
	"context"
	"fmt"
	"os"

	"github.com/twmb/franz-go/pkg/sr"
)

// Compatibility is the evolution policy every event subject is pinned to
// (ADR-011 §5.1, unchanged by ADR-022). FULL is affordable under Protobuf
// because proto3 retains unknown fields, so an older consumer round-trips a
// record carrying a newer optional field. `buf breaking` enforces the same
// contract at compile time; this pins it at the registry, for producers that
// never went through CI.
const Compatibility = sr.CompatFull

// NewRegistryClient dials a Confluent-API-compatible schema registry.
//
// For GCP Managed Kafka, baseURL is the registry resource URL —
// https://managedkafka.googleapis.com/v1/projects/{project}/locations/{region}/schemaRegistries/{id}
// — and token is an OAuth access token (`gcloud auth print-access-token`).
// The token is passed in rather than minted here so the caller decides which
// identity publishes schemas.
func NewRegistryClient(baseURL, token string) (*sr.Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("events: schema registry URL is required")
	}
	if token == "" {
		return nil, fmt.Errorf("events: schema registry bearer token is required")
	}

	client, err := sr.NewClient(sr.URLs(baseURL), sr.BearerToken(token))
	if err != nil {
		return nil, fmt.Errorf("events: schema registry client: %w", err)
	}
	return client, nil
}

// SchemaText reads the .proto source that is registered for every subject. The
// registry stores schema text, not descriptors, so registration reads the file
// the generated types were built from.
func SchemaText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("events: read schema %s: %w", path, err)
	}
	return string(b), nil
}

// SubjectState is what the registry holds for one event subject.
type SubjectState struct {
	Subject       string
	ID            int
	Version       int
	Compatibility string
}

// Publish registers schemaText under every event subject and pins each to FULL.
//
// All three subjects share one schema — the whole events/v1 file — and are told
// apart by the message index in the wire header (see NewSerde). Registration is
// idempotent: re-registering identical text returns the existing ID and version
// rather than creating a new one.
//
// Only the subjects this repository owns are touched; the registry-wide default
// is left alone, since it governs subjects other producers may add.
func Publish(ctx context.Context, client *sr.Client, schemaText string) ([]SubjectState, error) {
	states := make([]SubjectState, 0, len(Descriptors()))

	for _, d := range Descriptors() {
		subject := d.Subject()

		registered, err := client.CreateSchema(ctx, subject, sr.Schema{
			Schema: schemaText,
			Type:   sr.TypeProtobuf,
		})
		if err != nil {
			return nil, fmt.Errorf("events: register %s: %w", subject, err)
		}

		// Compatibility is set after the first version exists — there is
		// nothing to be incompatible with until then.
		for _, result := range client.SetCompatibility(ctx, sr.SetCompatibility{Level: Compatibility}, subject) {
			if result.Err != nil {
				return nil, fmt.Errorf("events: set %s compatibility on %s: %w", Compatibility, subject, result.Err)
			}
		}

		states = append(states, SubjectState{
			Subject:       subject,
			ID:            registered.ID,
			Version:       registered.Version,
			Compatibility: Compatibility.String(),
		})
	}

	return states, nil
}

// Inspect reads back the latest registered version of every event subject and
// its compatibility level — the counterpart to Publish, used to confirm what a
// registry actually holds rather than what was sent to it.
func Inspect(ctx context.Context, client *sr.Client) ([]SubjectState, error) {
	states := make([]SubjectState, 0, len(Descriptors()))

	for _, d := range Descriptors() {
		subject := d.Subject()

		latest, err := client.SchemaByVersion(ctx, subject, -1)
		if err != nil {
			return nil, fmt.Errorf("events: read %s: %w", subject, err)
		}

		state := SubjectState{Subject: subject, ID: latest.ID, Version: latest.Version}
		for _, result := range client.Compatibility(ctx, subject) {
			if result.Err != nil {
				return nil, fmt.Errorf("events: read %s compatibility: %w", subject, result.Err)
			}
			state.Compatibility = result.Level.String()
		}

		states = append(states, state)
	}

	return states, nil
}

// SchemaIDs maps subject to schema ID, the form NewSerde takes.
func SchemaIDs(states []SubjectState) map[string]int {
	ids := make(map[string]int, len(states))
	for _, s := range states {
		ids[s.Subject] = s.ID
	}
	return ids
}
