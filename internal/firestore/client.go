// Package firestore provides the shared Firestore client and the typed
// documents and stores for the identity tier — users, chats, memberships and
// sessions (ADR-023 "Firestore model", ADR-021 Decision A).
//
// Only this package may import cloud.google.com/go/firestore; service adapters
// consume the stores defined here, exactly as they consumed the retired
// internal/dynamo (CONTRIBUTING.md §Shared packages). `make
// check-firestore-boundary` enforces it.
//
// Two conventions run through the package:
//
//   - **Identity is typed, payload is primitive.** Stores and document-ID
//     helpers take domain value objects (ChatID, UserID, SessionID) because
//     they are validated identifiers the app layer already holds. Document
//     fields are plain Go types, because these structs are storage shapes;
//     mapping them to domain entities belongs in the service adapters that own
//     the ports (M1.2, M1.3).
//   - **A document's ID is not one of its fields.** Firestore stores the ID on
//     the reference, so every document struct carries `ID string` tagged
//     `firestore:"-"`, populated on read and used to address writes.
package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// Config holds the parameters needed to reach a Firestore database.
type Config struct {
	// ProjectID is the GCP project owning the database.
	ProjectID string

	// DatabaseID is the named database (Terraform creates "messaging-dev",
	// not "(default)"). Required — an empty value would silently target the
	// default database, which this project never provisions.
	DatabaseID string

	// Timeout bounds a single Firestore operation. The SDK takes no timeout
	// option, so every store method derives its context from it.
	Timeout time.Duration
}

// Client wraps a Firestore client. The FS field is the handle stores use.
type Client struct {
	FS *firestore.Client

	timeout time.Duration
}

// NewClient connects to Firestore using Application Default Credentials.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("firestore: project ID is required")
	}
	if cfg.DatabaseID == "" {
		return nil, fmt.Errorf("firestore: database ID is required")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("firestore: timeout must be positive")
	}

	fs, err := firestore.NewClientWithDatabase(ctx, cfg.ProjectID, cfg.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("firestore: connect to %s/%s: %w", cfg.ProjectID, cfg.DatabaseID, err)
	}

	return &Client{FS: fs, timeout: cfg.Timeout}, nil
}

// Close releases the underlying gRPC connections.
func (c *Client) Close() error {
	if err := c.FS.Close(); err != nil {
		return fmt.Errorf("firestore: close: %w", err)
	}
	return nil
}

// withTimeout bounds one operation. Callers must call the cancel func.
func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}

// withTxTimeout bounds a whole transaction, which is several operations plus
// the backoff between the attempts the store forces.
//
// It is deliberately not derived from Config.Timeout: that value is the budget
// for one round trip, and spending it on a transaction that the library may
// legitimately re-run three times produces DeadlineExceeded on a healthy path
// (see domain.FirestoreTxTimeout, which was set from a measured failure).
func (c *Client) withTxTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, domain.FirestoreTxTimeout)
}
