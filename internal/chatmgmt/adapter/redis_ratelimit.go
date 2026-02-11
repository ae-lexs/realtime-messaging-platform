package adapter

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	redisclient "github.com/aelexs/realtime-messaging-platform/internal/redis"
)

// rateLimitScript is the Lua script from ADR-015 §9.2. It atomically
// increments a counter and sets a TTL on the first write. This avoids
// the MULTI/EXEC approach which cannot conditionally EXPIRE only on
// the first increment, and avoids depending on EXPIRE ... NX (Redis 7.0+).
const rateLimitScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`

// RateLimiter implements rate-limiting operations backed by Redis.
// All methods follow the fail-closed policy from ADR-013: Redis errors
// result in denial (never silent allow).
type RateLimiter struct {
	cmd redisclient.Cmdable
}

// NewRateLimiter creates a RateLimiter that uses cmd for Redis operations.
func NewRateLimiter(cmd redisclient.Cmdable) *RateLimiter {
	return &RateLimiter{cmd: cmd}
}

// CheckAndIncrement atomically increments the counter for key and checks
// whether the count exceeds limit within a fixed window of windowSeconds.
// Returns (true, nil) if the request is allowed, (false, nil) if the
// limit is exceeded, and (false, err) on Redis failure (fail-closed per
// ADR-013).
func (r *RateLimiter) CheckAndIncrement(ctx context.Context, key string, limit, windowSeconds int) (bool, error) {
	ctx, span := tracer.Start(ctx, "redis.ratelimit.check")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.operation", "EVAL"),
	)

	count, err := r.cmd.Eval(ctx, rateLimitScript, []string{key}, windowSeconds).Int64()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false, fmt.Errorf("rate limit check %q: %w", key, err)
	}

	return count <= int64(limit), nil
}

// CheckLockout checks whether a lockout key exists in Redis.
// Returns (true, nil) if the key exists (user is locked out), (false, nil)
// if no lockout is active, and (true, err) on Redis failure (fail-closed
// per ADR-013: treat error as locked).
func (r *RateLimiter) CheckLockout(ctx context.Context, key string) (bool, error) {
	ctx, span := tracer.Start(ctx, "redis.ratelimit.check_lockout")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.operation", "EXISTS"),
	)

	result, err := r.cmd.Exists(ctx, key).Result()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return true, fmt.Errorf("lockout check %q: %w", key, err)
	}

	return result > 0, nil
}

// SetLockout sets a lockout key in Redis with the given TTL.
// The key signals that a user has been locked out for the specified duration.
func (r *RateLimiter) SetLockout(ctx context.Context, key string, ttlSeconds int) error {
	ctx, span := tracer.Start(ctx, "redis.ratelimit.set_lockout")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.operation", "SET"),
	)

	err := r.cmd.Set(ctx, key, "1", time.Duration(ttlSeconds)*time.Second).Err()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("set lockout %q: %w", key, err)
	}

	return nil
}
