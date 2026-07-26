// Package main is the entrypoint for schemactl, the event-schema publish flow.
//
// The Managed Kafka schema registry has no Terraform resource in either Google
// provider, so schema publication is a deploy step rather than an apply:
//
//	schemactl register   register proto/events/v1 under every event subject
//	schemactl verify     read back what the registry actually holds
//
// Configuration is environment-only, because the caller (scripts/schema.sh)
// already resolves the registry URL and mints the token:
//
//	SCHEMA_REGISTRY_URL   .../schemaRegistries/{id}   (required)
//	SR_TOKEN              OAuth access token         (required)
//	SCHEMA_FILE           path to events.proto       (optional)
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aelexs/realtime-messaging-platform/internal/events"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: schemactl {register|verify}")
	}

	client, err := events.NewRegistryClient(os.Getenv("SCHEMA_REGISTRY_URL"), os.Getenv("SR_TOKEN"))
	if err != nil {
		return err
	}

	var states []events.SubjectState

	switch args[0] {
	case "register":
		path := os.Getenv("SCHEMA_FILE")
		if path == "" {
			path = events.DefaultSchemaPath
		}

		schema, readErr := events.SchemaText(path)
		if readErr != nil {
			return readErr
		}

		if states, err = events.Publish(ctx, client, schema); err != nil {
			return err
		}

	case "verify":
		if states, err = events.Inspect(ctx, client); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown command %q: expected register or verify", args[0])
	}

	for _, s := range states {
		fmt.Printf("%-26s id=%-4d version=%-3d compatibility=%s\n", s.Subject, s.ID, s.Version, s.Compatibility)
	}

	return nil
}
