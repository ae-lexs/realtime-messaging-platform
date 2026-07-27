// Package secrets provides the shared GCP Secret Manager client — the source
// of the JWT signing key, the JWT public keys, and the OTP pepper (ADR-015
// §3.2 as amended for GCP in its Appendix F).
//
// Only this package may import cloud.google.com/go/secretmanager; consumers
// take the narrow interfaces they need, exactly as service adapters consume
// internal/firestore rather than the Firestore SDK (CONTRIBUTING.md §Shared
// packages). `make check-secretmanager-boundary` enforces it.
//
// AWS split this material across Secrets Manager (private key, pepper) and SSM
// Parameter Store (public keys, current key ID). GCP has one service, so the
// two-tier naming collapses: Secret Manager has no path hierarchy — a secret
// ID is a flat name — and the SSM path segments become underscore- or
// hyphen-separated components of that name.
//
// This package reads secrets; it never writes them. Terraform creates the
// secret containers and scripts/auth-keys.sh adds the versions, so key
// material never enters Terraform state.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/iterator"
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

// ListIDsWithPrefix returns the IDs of the project's secrets whose ID begins
// with prefix. It is how the set of valid JWT `kid` values is discovered: the
// public keys are named jwt-public-key-{KEY_ID}, so the prefix enumerates
// every key a token may legitimately be signed with, including the one being
// phased out during a rotation.
func (c *Client) ListIDsWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Secret Manager's filter language matches on the full resource name, so
	// the prefix is applied here rather than server-side; the project holds a
	// handful of secrets, not a page-worth.
	iter := c.sm.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{
		Parent: "projects/" + c.projectID,
	})

	var ids []string
	for {
		secret, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			return ids, nil
		}
		if err != nil {
			return nil, fmt.Errorf("secrets: list with prefix %q: %w", prefix, err)
		}

		id := secret.GetName()[strings.LastIndexByte(secret.GetName(), '/')+1:]
		if strings.HasPrefix(id, prefix) {
			ids = append(ids, id)
		}
	}
}
