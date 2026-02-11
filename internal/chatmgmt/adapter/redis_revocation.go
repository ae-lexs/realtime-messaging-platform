package adapter

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	redisclient "github.com/aelexs/realtime-messaging-platform/internal/redis"
)

const (
	// revokedJTIPrefix is the Redis key prefix for revoked JTI entries.
	// Key pattern: revoked_jti:{jti} per ADR-015 §6.2.
	revokedJTIPrefix = "revoked_jti:"

	// revokedJTITTL is the fixed TTL for revoked JTI entries.
	// Set to 3600 seconds (= max access token lifetime of 60 minutes)
	// per PR1-DECISIONS §Revocation TTLs. Fixed rather than dynamic
	// (exp - now) for uniform handling across all revocation paths
	// including admin-initiated revocations.
	revokedJTITTL = 3600 * time.Second
)

// Compile-time check: RevocationStore satisfies app.RevocationStore.
var _ app.RevocationStore = (*RevocationStore)(nil)

// RevocationStore implements JTI revocation backed by Redis.
// All methods follow the fail-closed policy from ADR-013: Redis errors
// on reads result in treating the token as revoked (deny access).
type RevocationStore struct {
	cmd redisclient.Cmdable
}

// NewRevocationStore creates a RevocationStore that uses cmd for Redis operations.
func NewRevocationStore(cmd redisclient.Cmdable) *RevocationStore {
	return &RevocationStore{cmd: cmd}
}

// Revoke marks a JTI as revoked by setting a key with fixed 3600s TTL.
// Written by Chat Mgmt Service on logout, session revoke, and reuse
// detection per ADR-015 §6.2.
func (s *RevocationStore) Revoke(ctx context.Context, jti string) error {
	ctx, span := tracer.Start(ctx, "redis.revocation.revoke")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.operation", "SET"),
	)

	key := revokedJTIPrefix + jti
	err := s.cmd.Set(ctx, key, "1", revokedJTITTL).Err()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("revoke JTI %q: %w", jti, err)
	}

	return nil
}

// IsRevoked checks whether a JTI has been revoked.
// Returns (true, nil) if revoked, (false, nil) if not revoked, and
// (true, err) on Redis failure (fail-closed per ADR-013: treat as
// revoked when the revocation store is unavailable).
func (s *RevocationStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	ctx, span := tracer.Start(ctx, "redis.revocation.is_revoked")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.operation", "EXISTS"),
	)

	key := revokedJTIPrefix + jti
	result, err := s.cmd.Exists(ctx, key).Result()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return true, fmt.Errorf("check revocation %q: %w", jti, err)
	}

	return result > 0, nil
}
