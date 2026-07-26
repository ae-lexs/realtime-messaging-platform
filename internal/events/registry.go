package events

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/twmb/franz-go/pkg/sr"
)

// Compatibility is the evolution policy every event subject is pinned to
// (ADR-011 §5.1, unchanged by ADR-022). FULL is affordable under Protobuf
// because proto3 retains unknown fields, so an older consumer round-trips a
// record carrying a newer optional field. `buf breaking` enforces the same
// contract at compile time; this pins it at the registry, for producers that
// never went through CI.
const Compatibility = sr.CompatFull

// requestTimeout bounds a single registry call.
const requestTimeout = 30 * time.Second

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

	client, err := sr.NewClient(
		sr.URLs(baseURL),
		sr.BearerToken(token),
		// The library default is 5s, which GCP's registry exceeds often enough
		// to fail a publish run on a slow call ("context deadline exceeded ...
		// while awaiting headers"). Schema publication is a deploy step, not a
		// request path — waiting is cheaper than a half-registered registry.
		sr.HTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	if err != nil {
		return nil, fmt.Errorf("events: schema registry client: %w", err)
	}
	return client, nil
}

// SchemaText reads a .proto source. The registry stores schema text, not
// descriptors, so registration reads the files the generated types were built
// from.
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

// Publish registers every schema in Sources under its subject and pins each to
// FULL. root is the repository root the source paths are relative to.
//
// Sources are registered in dependency order and each one declares its direct
// imports as references to the subjects already registered — the registry
// resolves nothing on its own, so an unreferenced import fails with
// INVALID_PROTO_SCHEMA rather than silently registering a broken schema.
//
// Registration is idempotent: re-registering identical text returns the
// existing ID and version rather than creating a new one.
//
// Only the subjects this repository owns are touched; the registry-wide default
// is left alone, since it governs subjects other producers may add.
func Publish(ctx context.Context, client *sr.Client, root string) ([]SubjectState, error) {
	sources := Sources()
	states := make([]SubjectState, 0, len(sources))
	byPath := make(map[string]sr.SchemaReference, len(sources))

	for _, source := range sources {
		text, err := SchemaText(filepath.Join(root, source.File))
		if err != nil {
			return nil, err
		}

		references := make([]sr.SchemaReference, 0, len(source.Imports))
		for _, imported := range source.Imports {
			reference, ok := byPath[imported]
			if !ok {
				return nil, fmt.Errorf("events: %s imports %s, which is not registered before it", source.File, imported)
			}
			references = append(references, reference)
		}

		registered, err := client.CreateSchema(ctx, source.Subject, sr.Schema{
			Schema:     text,
			Type:       sr.TypeProtobuf,
			References: references,
		})
		if err != nil {
			return nil, fmt.Errorf("events: register %s: %w", source.Subject, err)
		}

		byPath[source.Path] = sr.SchemaReference{
			Name:    source.Path,
			Subject: source.Subject,
			Version: registered.Version,
		}

		// Compatibility is set after the first version exists — there is
		// nothing to be incompatible with until then.
		for _, result := range client.SetCompatibility(ctx, sr.SetCompatibility{Level: Compatibility}, source.Subject) {
			if result.Err != nil {
				return nil, fmt.Errorf("events: set %s compatibility on %s: %w", Compatibility, source.Subject, result.Err)
			}
		}

		states = append(states, SubjectState{
			Subject:       source.Subject,
			ID:            registered.ID,
			Version:       registered.Version,
			Compatibility: Compatibility.String(),
		})
	}

	return states, nil
}

// Inspect reads back the latest registered version of every subject and its
// compatibility level — the counterpart to Publish, used to confirm what a
// registry actually holds rather than what was sent to it.
func Inspect(ctx context.Context, client *sr.Client) ([]SubjectState, error) {
	sources := Sources()
	states := make([]SubjectState, 0, len(sources))

	for _, source := range sources {
		subject := source.Subject

		latest, err := client.SchemaByVersion(ctx, subject, -1)
		if err != nil {
			return nil, fmt.Errorf("events: read %s: %w", subject, err)
		}

		compatibility, err := subjectCompatibility(ctx, client, subject)
		if err != nil {
			return nil, err
		}

		states = append(states, SubjectState{
			Subject:       subject,
			ID:            latest.ID,
			Version:       latest.Version,
			Compatibility: compatibility,
		})
	}

	return states, nil
}

// subjectCompatibility reads GET /config/{subject} directly instead of through
// the client's typed helper: Confluent returns the level as `compatibilityLevel`
// on a GET, GCP's registry returns it as `compatibility`, and the typed helper
// only understands the former — so it silently reports an empty level against a
// registry that has the policy set. Both spellings are accepted here.
func subjectCompatibility(ctx context.Context, client *sr.Client, subject string) (string, error) {
	var config struct {
		Compatibility      string `json:"compatibility"`
		CompatibilityLevel string `json:"compatibilityLevel"`
	}

	if err := client.Do(ctx, http.MethodGet, "/config/"+subject, nil, &config); err != nil {
		return "", fmt.Errorf("events: read %s compatibility: %w", subject, err)
	}

	if config.Compatibility != "" {
		return config.Compatibility, nil
	}
	return config.CompatibilityLevel, nil
}

// SchemaIDs maps subject to schema ID, the form NewSerde takes.
func SchemaIDs(states []SubjectState) map[string]int {
	ids := make(map[string]int, len(states))
	for _, s := range states {
		ids[s.Subject] = s.ID
	}
	return ids
}
