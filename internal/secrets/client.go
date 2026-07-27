// Package secrets provides the shared GCP Secret Manager client — the source
// of the JWT signing key, the JWT public keys, and the OTP pepper (ADR-015
// §3.2 as amended for GCP in its Appendix F).
//
// Only this package may import cloud.google.com/go/secretmanager; consumers
// take the narrow interfaces they need, exactly as service adapters consume
// internal/firestore rather than the Firestore SDK (CONTRIBUTING.md §Shared
// packages). `make check-secretmanager-boundary` enforces it.
//
// Secret Manager has no path hierarchy: an ID is a flat name, so the naming
// scheme encodes structure with hyphens (jwt-signing-key-{KEY_ID}) rather than
// path segments. ADR-015 Appendix F has the full mapping and the reasoning.
//
// This package reads secrets; it never writes them. Terraform creates the
// secret containers and scripts/auth-keys.sh adds the versions, so key
// material never enters Terraform state.
package secrets

import (
	"context"
	"fmt"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// Config holds the parameters needed to reach Secret Manager.
type Config struct {
	// ProjectID is the GCP project holding the secrets.
	ProjectID string

	// Timeout bounds a single Secret Manager operation.
	Timeout time.Duration
}

// Client wraps a Secret Manager client.
type Client struct {
	sm *secretmanager.Client

	projectID string
	timeout   time.Duration
}

// NewClient connects to Secret Manager using Application Default Credentials.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("secrets: project ID is required")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("secrets: timeout must be positive")
	}

	sm, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets: connect to project %s: %w", cfg.ProjectID, err)
	}

	return &Client{sm: sm, projectID: cfg.ProjectID, timeout: cfg.Timeout}, nil
}

// Close releases the underlying gRPC connections.
func (c *Client) Close() error {
	if err := c.sm.Close(); err != nil {
		return fmt.Errorf("secrets: close: %w", err)
	}
	return nil
}

// Latest returns the payload of a secret's latest enabled version, or
// domain.ErrNotFound.
//
// The "latest" alias is deliberate: rotation adds a version rather than
// replacing a secret, so a refreshing process picks up the new material
// without being told a new name.
func (c *Client) Latest(ctx context.Context, secretID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", c.projectID, secretID)

	result, err := c.sm.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: name})
	if err != nil {
		if code := status.Code(err); code == codes.NotFound || code == codes.FailedPrecondition {
			// FailedPrecondition is what an existing secret with no enabled
			// version returns — Terraform created the container but
			// scripts/auth-keys.sh has not run. Same remedy as NotFound, so
			// the caller should not have to tell them apart.
			return nil, fmt.Errorf("secrets: access %s: %w", secretID, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("secrets: access %s: %w", secretID, err)
	}

	return result.GetPayload().GetData(), nil
}

// This client deliberately offers no way to enumerate secrets.
//
// An earlier version discovered JWT key IDs by listing secrets with the
// jwt-public-key- prefix. That cannot be granted at the right scope, and the
// first deploy is what proved it: secretmanager.secrets.list is a *project*
// permission — listing is a query over the project's secret collection, so
// there is no such thing as "may list this one secret". Supporting it would
// have meant granting roles/secretmanager.viewer project-wide, widening the
// service's reach from four named secrets to the metadata of every secret in
// the project, in exchange for a rotation path ADR-015 v1.1 had already
// deferred. Keys are addressed by name instead (see auth.RemoteKeyStore).
