# PR-0 TBD Decisions: Configuration, Error Taxonomy & Clock Semantics

- **Status**: Approved
- **Date**: 2026-02-01
- **Related ADRs**: ADR-009 (Failure Handling), ADR-012 (Observability), ADR-013 (Security), ADR-014 (Technology Stack)
- **Execution Plan Reference**: TBD-PR0-1, TBD-PR0-2, TBD-PR0-3

---

## Purpose

This document resolves the three TBD notes identified in the Execution Plan for PR-0. Each decision establishes foundational patterns that all subsequent PRs build upon. These are not ADR-level architectural decisions — they are implementation specifications for patterns already mandated by the ADRs.

> **📋 Normative Policy Layer**: This document defines **mandatory implementation conventions**. All code in this repository must conform to these specifications. Deviations require an ADR or explicit justification in the PR description with reviewer approval.

---

## TBD-PR0-1: Configuration & Secrets Injection Parity

### Problem Statement

Services reference AWS Secrets Manager and SSM Parameter Store (ADR-014 §7), but the local development parity mechanism and configuration precedence are not pinned. Additionally, secrets must never appear in logs (ADR-013), requiring explicit redaction in the structured logging pipeline (ADR-012 §1.4).

### Decision

#### Configuration Precedence

Configuration is resolved in the following order (highest to lowest precedence):

```
1. Environment variables     (deployment-specific overrides)
2. AWS SDK (Secrets Manager / SSM Parameter Store)  (shared configuration)
3. Compiled defaults         (normative limits from ADR-009)
```

**Rationale**: This follows the [12-Factor App methodology](https://12factor.net/config) which recommends environment variables for config that varies between deploys. Environment variables take highest precedence because they are the standard mechanism for per-deployment overrides in containerized environments (ECS task definitions, docker-compose). AWS SDK sources provide shared configuration managed outside the codebase. Compiled defaults ensure the system runs with correct values even if external configuration is unavailable, matching ADR-009's normative limits.

#### Configuration Failure Semantics (Normative Rule)

> **Rule: Required vs Optional Configuration Keys**
>
> Configuration keys are classified as **required** or **optional**:
> - **Required keys**: Failure to load causes **process startup failure** (exit code 1, logged error). The service must not start in a partially-configured state.
> - **Optional keys**: Failure to load falls back to compiled defaults with a warning-level log entry.
>
> Keys are marked in code via struct tags:
> ```go
> type KafkaConfig struct {
>     Brokers    []string `koanf:"brokers" required:"true"`   // Required — no default
>     ClientID   string   `koanf:"client_id" required:"false"` // Optional — has default
> }
> ```
>
> **AWS SDK partial availability**: If Secrets Manager or SSM Parameter Store is unreachable:
> - Required secrets from AWS → startup failure
> - Optional config from AWS → fallback to defaults, warn log
>
> This prevents silent misconfiguration while allowing graceful degradation for non-critical settings.

#### Configuration Library

**Decision**: Use [koanf](https://github.com/knadh/koanf) for configuration management.

**Rationale**: koanf was selected over Viper for the following reasons:

| Criterion | koanf | Viper |
|-----------|-------|-------|
| Key case preservation | Preserves original case | Forces lowercase (breaks JSON/YAML/TOML specs) |
| Dependency footprint | Minimal, modular providers | Heavy, pulls many transitive dependencies |
| Precedence control | Explicit, caller-controlled | Arbitrary built-in ordering |
| AWS integration | Provider available via `koanf/providers/env` + custom | Requires custom integration |

The key case preservation is critical — DynamoDB attribute names and Kafka topic names are case-sensitive, and a configuration library that lowercases keys would silently corrupt these values.

**Source**: [koanf vs Viper comparison](https://itnext.io/golang-configuration-management-library-viper-vs-koanf-eea60a652a22)

#### Local Development Parity

**Development Environment**:
- `.env.dev` file (git-ignored via `.gitignore`) contains local configuration
- `docker-compose.dev.yaml` injects these as environment variables via `env_file`
- LocalStack provides Secrets Manager / SSM API compatibility at `http://localstack:4566`
- Services detect local environment via `ENVIRONMENT=local` and configure AWS SDK endpoint override

**Production Environment**:
- ECS task definitions reference Secrets Manager ARNs for sensitive values
- SSM Parameter Store provides non-sensitive configuration overrides
- No `.env` files exist in production — all config comes from AWS or environment variables

**Configuration Structure**:

```go
// internal/config/config.go

type Config struct {
    Environment string        // "local", "dev", "prod"
    LogLevel    string        // "debug", "info", "warn", "error"

    // Service-specific sections
    Gateway  GatewayConfig
    Ingest   IngestConfig
    Fanout   FanoutConfig
    ChatMgmt ChatMgmtConfig

    // Infrastructure
    DynamoDB DynamoDBConfig
    Kafka    KafkaConfig
    Redis    RedisConfig
}

// Load applies precedence: env vars → AWS SDK → defaults
func Load(ctx context.Context) (*Config, error)
```

#### Secret Redaction in Structured Logging

Secrets must never appear in logs (ADR-013). The structured logging pipeline (ADR-012 §1.4) must redact sensitive fields before emission.

**Implementation**: Use `slog.HandlerOptions.ReplaceAttr` to redact fields matching sensitive patterns.

```go
// internal/observability/logging.go

var sensitivePatterns = []string{
    "_key",
    "_secret",
    "_token",
    "_password",
    "_pepper",
    "_credential",
    "authorization",
    "bearer",
}

func NewRedactingHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
    if opts == nil {
        opts = &slog.HandlerOptions{}
    }

    originalReplace := opts.ReplaceAttr
    opts.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
        // Apply original replacer first if present
        if originalReplace != nil {
            a = originalReplace(groups, a)
        }

        // Redact sensitive fields by name pattern
        keyLower := strings.ToLower(a.Key)
        for _, pattern := range sensitivePatterns {
            if strings.Contains(keyLower, pattern) {
                return slog.String(a.Key, "[REDACTED]")
            }
        }
        return a
    }

    return slog.NewJSONHandler(w, opts)
}
```

**Alternative Considered**: The [masq library](https://github.com/m-mizutani/masq) provides comprehensive redaction via struct tags, regex patterns, and field names. However, it adds a dependency for functionality achievable with `ReplaceAttr`. For MVP, the built-in approach is sufficient. masq can be adopted later if more sophisticated redaction (regex-based PII detection, nested struct traversal) becomes necessary.

**Sources**:
- [Arcjet - Redacting sensitive data in slog](https://blog.arcjet.com/redacting-sensitive-data-from-logs-with-go-log-slog/)
- [Go slog documentation - LogValuer interface](https://pkg.go.dev/log/slog#LogValuer)
- [masq - slog redaction utility](https://github.com/m-mizutani/masq)

#### Secret Wrapper Types

For defense-in-depth, define wrapper types for secrets that implement `slog.LogValuer`:

```go
// internal/domain/secrets.go

// SecretString wraps sensitive string values.
// Implements slog.LogValuer to prevent accidental logging.
// Implements fmt.Stringer to return redacted value.
type SecretString string

func (s SecretString) String() string {
    return "[REDACTED]"
}

func (s SecretString) LogValue() slog.Value {
    return slog.StringValue("[REDACTED]")
}

// Expose returns the actual secret value.
// Use sparingly — only when the secret must be used (e.g., JWT signing).
func (s SecretString) Expose() string {
    return string(s)
}
```

**Rationale**: Even if `ReplaceAttr` is misconfigured or bypassed, the `LogValuer` interface ensures secrets wrapped in `SecretString` are never logged in plaintext. This aligns with the defense-in-depth principle from ADR-013.

#### Validation

- **Golden test**: Assert that logging a struct containing `SecretString` fields produces `[REDACTED]` in output
- **CI check**: `gosec` detects potential secret leakage patterns
- **Manual review**: Any log statement in security-sensitive code paths (auth, token handling) requires explicit review for secret exposure

---

## TBD-PR0-2: Error Taxonomy — Domain Error → Wire Mapping

### Problem Statement

`internal/domain/errors.go` defines canonical error types, but the mapping to wire protocols is unspecified:
- Domain error → gRPC status code (for Ingest, Chat Mgmt)
- Domain error → HTTP status + JSON body (for grpc-gateway REST)
- Domain error → WebSocket error frame + close code (for Gateway)

### Decision

#### Domain Error Types

Define sentinel errors in `internal/domain/errors.go` following Go conventions and the project's DDD Lite approach (CONTRIBUTING.md §Domain Errors):

```go
// internal/domain/errors.go

package domain

import "errors"

// Sentinel errors — use errors.Is() for matching
var (
    // Resource errors
    ErrNotFound      = errors.New("resource not found")
    ErrAlreadyExists = errors.New("resource already exists")

    // Authorization errors
    ErrUnauthorized = errors.New("authentication required")
    ErrForbidden    = errors.New("permission denied")
    ErrNotMember    = errors.New("user is not a member of this chat")

    // Validation errors
    ErrInvalidInput     = errors.New("invalid input")
    ErrMessageTooLarge  = errors.New("message exceeds size limit")
    ErrInvalidContentType = errors.New("unsupported content type")

    // Operational errors
    ErrRateLimited   = errors.New("rate limit exceeded")
    ErrUnavailable   = errors.New("service temporarily unavailable")
    ErrSlowConsumer  = errors.New("client not consuming messages fast enough")

    // Idempotency signal (not semantically a failure — indicates successful deduplication)
    // Returns HTTP 200/gRPC OK with the original message; included here for mapper completeness
    ErrDuplicateMessage = errors.New("duplicate client_message_id")
)

// Error classification for ADR-009 failure tiers
func IsRetryable(err error) bool {
    return errors.Is(err, ErrUnavailable) || errors.Is(err, ErrRateLimited)
}

func IsClientError(err error) bool {
    return errors.Is(err, ErrInvalidInput) ||
           errors.Is(err, ErrMessageTooLarge) ||
           errors.Is(err, ErrInvalidContentType) ||
           errors.Is(err, ErrNotFound) ||
           errors.Is(err, ErrNotMember) ||
           errors.Is(err, ErrForbidden) ||
           errors.Is(err, ErrUnauthorized)
}
```

**Rationale**: Following the guidance from [Go error handling best practices](https://jayconrod.com/posts/116/error-handling-guidelines-for-go), we use a small set of sentinel errors (approximately 10) for conditions callers need to distinguish. This is fewer than "one error per failure mode" (which leads to error proliferation) but more than "just use string errors" (which prevents programmatic handling).

#### Wire Protocol Mappings

Each wire protocol has a dedicated mapper in a separate package to keep domain clean:

**gRPC Status Mapping** (`internal/errors/grpc.go`):

| Domain Error | gRPC Status Code | Rationale |
|--------------|------------------|-----------|
| `ErrNotFound` | `codes.NotFound` | Standard mapping per [gRPC status codes](https://grpc.github.io/grpc/core/md_doc_statuscodes.html) |
| `ErrAlreadyExists` | `codes.AlreadyExists` | Resource creation conflict |
| `ErrUnauthorized` | `codes.Unauthenticated` | Missing or invalid credentials |
| `ErrForbidden` | `codes.PermissionDenied` | Valid credentials, insufficient permissions |
| `ErrNotMember` | `codes.PermissionDenied` | Membership is a permission concept |
| `ErrInvalidInput` | `codes.InvalidArgument` | Client sent malformed request |
| `ErrMessageTooLarge` | `codes.InvalidArgument` | Validation failure |
| `ErrInvalidContentType` | `codes.InvalidArgument` | Validation failure |
| `ErrRateLimited` | `codes.ResourceExhausted` | Standard for rate limiting |
| `ErrUnavailable` | `codes.Unavailable` | Transient failure, client should retry |
| `ErrSlowConsumer` | `codes.ResourceExhausted` | Client-side resource issue |
| `ErrDuplicateMessage` | `codes.AlreadyExists` | Idempotency hit (not an error) |
| (unknown) | `codes.Internal` | Never expose internal details to clients |

```go
// internal/errors/grpc.go

package errors

import (
    "errors"

    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    "github.com/example/messaging/internal/domain"
)

// ToGRPCStatus converts a domain error to a gRPC status.
// The returned status can be sent directly to gRPC clients.
func ToGRPCStatus(err error) *status.Status {
    if err == nil {
        return status.New(codes.OK, "")
    }

    switch {
    case errors.Is(err, domain.ErrNotFound):
        return status.New(codes.NotFound, err.Error())
    case errors.Is(err, domain.ErrAlreadyExists), errors.Is(err, domain.ErrDuplicateMessage):
        return status.New(codes.AlreadyExists, err.Error())
    case errors.Is(err, domain.ErrUnauthorized):
        return status.New(codes.Unauthenticated, err.Error())
    case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrNotMember):
        return status.New(codes.PermissionDenied, err.Error())
    case errors.Is(err, domain.ErrInvalidInput),
         errors.Is(err, domain.ErrMessageTooLarge),
         errors.Is(err, domain.ErrInvalidContentType):
        return status.New(codes.InvalidArgument, err.Error())
    case errors.Is(err, domain.ErrRateLimited), errors.Is(err, domain.ErrSlowConsumer):
        return status.New(codes.ResourceExhausted, err.Error())
    case errors.Is(err, domain.ErrUnavailable):
        return status.New(codes.Unavailable, err.Error())
    default:
        // Never expose internal error details to clients
        return status.New(codes.Internal, "internal error")
    }
}
```

**Source**: [gRPC error handling guide](https://grpc.io/docs/guides/error/), [gRPC status codes reference](https://grpc.github.io/grpc/core/md_doc_statuscodes.html)

**HTTP Status Mapping** (`internal/errors/http.go`):

grpc-gateway provides [default gRPC→HTTP mapping](https://github.com/grpc-ecosystem/grpc-gateway/blob/main/runtime/errors.go). We use this default mapping rather than customizing:

| gRPC Code | HTTP Status | Notes |
|-----------|-------------|-------|
| `OK` | 200 | Success |
| `InvalidArgument` | 400 | Bad Request |
| `Unauthenticated` | 401 | Unauthorized |
| `PermissionDenied` | 403 | Forbidden |
| `NotFound` | 404 | Not Found |
| `AlreadyExists` | 409 | Conflict |
| `ResourceExhausted` | 429 | Too Many Requests |
| `Unavailable` | 503 | Service Unavailable |
| `Internal` | 500 | Internal Server Error |

**Rationale**: grpc-gateway's default mapping is well-established and matches HTTP semantics. Customization adds complexity without benefit. The REST API (ADR-006) uses grpc-gateway, so this mapping is applied automatically.

**Source**: [grpc-gateway error handling](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/customizing_your_gateway/)

**WebSocket Close Code Mapping** (`internal/errors/websocket.go`):

[RFC 6455](https://datatracker.ietf.org/doc/html/rfc6455#section-7.4) defines standard close codes. The range 4000-4999 is reserved for application-specific codes:

| Domain Error | WebSocket Close Code | Close Reason String |
|--------------|---------------------|---------------------|
| `ErrUnauthorized` | 4001 | `unauthorized` |
| `ErrForbidden` | 4003 | `forbidden` |
| `ErrNotMember` | 4003 | `not_a_member` |
| `ErrNotFound` | 4004 | `not_found` |
| `ErrInvalidInput` | 4000 | `invalid_message` |
| `ErrMessageTooLarge` | 4013 | `message_too_large` |
| `ErrRateLimited` | 4029 | `rate_limited` |
| `ErrSlowConsumer` | 4029 | `slow_consumer` |
| `ErrUnavailable` | 1013 | `service_unavailable` (standard code) |
| (token expired) | 4001 | `token_expired` |
| (server shutdown) | 1001 | `server_shutdown` (standard "going away") |
| (protocol error) | 1002 | `protocol_error` (standard) |

```go
// internal/errors/websocket.go

package errors

import (
    "errors"

    "github.com/example/messaging/internal/domain"
)

// WebSocket close codes (RFC 6455 + application-specific 4xxx range)
const (
    // Standard codes (RFC 6455)
    CloseNormalClosure    = 1000
    CloseGoingAway        = 1001
    CloseProtocolError    = 1002
    ClosePolicyViolation  = 1008
    CloseInternalError    = 1011
    CloseServiceRestart   = 1012
    CloseTryAgainLater    = 1013

    // Application-specific codes (4000-4999)
    CloseInvalidMessage   = 4000
    CloseUnauthorized     = 4001
    CloseForbidden        = 4003
    CloseNotFound         = 4004
    CloseAlreadyExists    = 4009
    CloseMessageTooLarge  = 4013
    CloseRateLimited      = 4029
)

// WebSocketClose represents a close code and reason for WebSocket termination.
type WebSocketClose struct {
    Code   int
    Reason string
}

// ToWebSocketClose converts a domain error to a WebSocket close code and reason.
func ToWebSocketClose(err error) WebSocketClose {
    if err == nil {
        return WebSocketClose{Code: CloseNormalClosure, Reason: "normal_closure"}
    }

    switch {
    case errors.Is(err, domain.ErrUnauthorized):
        return WebSocketClose{Code: CloseUnauthorized, Reason: "unauthorized"}
    case errors.Is(err, domain.ErrForbidden):
        return WebSocketClose{Code: CloseForbidden, Reason: "forbidden"}
    case errors.Is(err, domain.ErrNotMember):
        return WebSocketClose{Code: CloseForbidden, Reason: "not_a_member"}
    case errors.Is(err, domain.ErrNotFound):
        return WebSocketClose{Code: CloseNotFound, Reason: "not_found"}
    case errors.Is(err, domain.ErrInvalidInput):
        return WebSocketClose{Code: CloseInvalidMessage, Reason: "invalid_message"}
    case errors.Is(err, domain.ErrMessageTooLarge):
        return WebSocketClose{Code: CloseMessageTooLarge, Reason: "message_too_large"}
    case errors.Is(err, domain.ErrRateLimited):
        return WebSocketClose{Code: CloseRateLimited, Reason: "rate_limited"}
    case errors.Is(err, domain.ErrSlowConsumer):
        return WebSocketClose{Code: CloseRateLimited, Reason: "slow_consumer"}
    case errors.Is(err, domain.ErrUnavailable):
        return WebSocketClose{Code: CloseTryAgainLater, Reason: "service_unavailable"}
    default:
        return WebSocketClose{Code: CloseInternalError, Reason: "internal_error"}
    }
}
```

**Source**: [RFC 6455 - WebSocket Close Codes](https://datatracker.ietf.org/doc/html/rfc6455#section-7.4), [WebSocket close codes reference](https://websocket.org/reference/close-codes/)

#### Golden Tests

Ship table-driven tests that assert each domain error maps to the expected wire format:

```go
// internal/errors/grpc_test.go

func TestToGRPCStatus(t *testing.T) {
    tests := []struct {
        name     string
        err      error
        wantCode codes.Code
    }{
        {"nil error", nil, codes.OK},
        {"not found", domain.ErrNotFound, codes.NotFound},
        {"not member", domain.ErrNotMember, codes.PermissionDenied},
        {"unauthorized", domain.ErrUnauthorized, codes.Unauthenticated},
        {"rate limited", domain.ErrRateLimited, codes.ResourceExhausted},
        {"unavailable", domain.ErrUnavailable, codes.Unavailable},
        {"wrapped not found", fmt.Errorf("context: %w", domain.ErrNotFound), codes.NotFound},
        {"unknown error", errors.New("something else"), codes.Internal},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := ToGRPCStatus(tt.err)
            assert.Equal(t, tt.wantCode, got.Code())
        })
    }
}
```

Similar tests exist for HTTP and WebSocket mappings.

#### Error Taxonomy Extension Rule (Normative Rule)

> **Rule of Extension: Adding New Domain Errors**
>
> Adding a new sentinel error to `internal/domain/errors.go` **requires** updating **all** wire mappers in the same PR:
> 1. `internal/errors/grpc.go` — add case to `ToGRPCStatus()`
> 2. `internal/errors/http.go` — verify grpc-gateway mapping (usually automatic)
> 3. `internal/errors/websocket.go` — add case to `ToWebSocketClose()`
> 4. Golden tests for all three mappers — add test cases for the new error
>
> **Enforcement**: CI runs a linter that ensures every sentinel error in `domain/errors.go` has a corresponding case in each mapper's switch statement. PRs that add errors without mapper updates will fail CI.
>
> **Rationale**: This prevents "orphan errors" that fall through to the `default` case (returning opaque `Internal` errors to clients). Every domain error must have an explicit, documented wire representation.

---

## TBD-PR0-3: Clock Semantics

### Problem Statement

Timestamps appear throughout the system: persisted message `created_at`, token expiration, heartbeat intervals, timeout enforcement. Without explicit clock semantics:
- Tests become non-deterministic (time-dependent logic varies between runs)
- Serialization may include Go's monotonic clock reading (corrupting comparisons)
- Clock skew between components affects latency SLIs (ADR-012 §2.6)

### Decision

#### Timestamp Representation

**All persisted timestamps are wall clock UTC milliseconds since epoch.**

```go
// Persisting a timestamp
createdAt := time.Now().UTC().UnixMilli()  // int64

// Reading a timestamp
t := time.UnixMilli(createdAt).UTC()  // time.Time with no monotonic reading
```

**Rationale**:
- Millisecond precision is sufficient for message ordering (ADR-001 uses per-chat sequence numbers for total ordering)
- Unix milliseconds are portable across languages and systems
- UTC eliminates timezone ambiguity
- `int64` is directly storable in DynamoDB Number type

**Never persist `time.Time` directly** — Go's `time.Time` includes a monotonic clock reading that has no meaning outside the current process. Serializing `time.Time` via JSON or storing in DynamoDB preserves only the wall clock, but the behavior is implicit and easy to misuse.

**Source**: [Go time package - Monotonic Clocks](https://pkg.go.dev/time#hdr-Monotonic_Clocks), [Victoria Metrics - Go monotonic vs wall clock](https://victoriametrics.com/blog/go-time-monotonic-wall-clock/)

#### Monotonic Clock for Durations

**All in-process duration measurements use the monotonic clock.**

Go's `time.Since()` and `time.Until()` automatically use the monotonic clock reading when comparing `time.Time` values that contain one. This is the correct behavior for:
- Timeout enforcement (ADR-009)
- Latency measurement (ADR-012)
- Heartbeat intervals (ADR-005 §3.10)

```go
start := time.Now()  // Contains both wall clock and monotonic reading
// ... do work ...
elapsed := time.Since(start)  // Uses monotonic clock — immune to wall clock adjustments
```

**Source**: [Go monotonic clock proposal](https://go.googlesource.com/proposal/+/master/design/12914-monotonic.md)

#### Time Comparison

**Always use `t.Equal()` for time comparisons, never `==`.**

The `==` operator compares both wall clock and monotonic readings. Two `time.Time` values representing the same instant may compare unequal if one has a monotonic reading and the other doesn't (e.g., after serialization round-trip).

```go
// Bad — may fail after serialization
if t1 == t2 { ... }

// Good — compares wall clock only
if t1.Equal(t2) { ... }

// Also good — compare epoch values
if t1.UnixMilli() == t2.UnixMilli() { ... }
```

#### Stripping Monotonic Readings

When a `time.Time` will be serialized, persisted, or compared with a value from another process, strip the monotonic reading:

```go
t = t.Round(0)  // Strips monotonic reading, preserves wall clock
```

**Source**: [Go time package documentation](https://pkg.go.dev/time#Time.Round)

#### Injectable Clock (Clean Architecture Alignment)

**Define a `Clock` interface in `internal/domain/` for dependency injection.**

This aligns with the Clean Architecture principle that the domain layer has no external dependencies (CONTRIBUTING.md §Clean Architecture). The domain defines the interface; adapters provide implementations.

```go
// internal/domain/clock.go

package domain

import "time"

// Clock provides the current time. Implementations may be real (production)
// or deterministic (testing).
type Clock interface {
    // Now returns the current time. The returned time includes both wall clock
    // and monotonic readings when using RealClock.
    Now() time.Time
}

// RealClock implements Clock using the system clock.
type RealClock struct{}

// Now returns time.Now().
func (RealClock) Now() time.Time {
    return time.Now()
}

// NowUTCMillis returns the current wall clock as UTC milliseconds since epoch.
// Use this for all persisted timestamps.
func NowUTCMillis(c Clock) int64 {
    return c.Now().UTC().UnixMilli()
}

// FromMillis converts epoch milliseconds to time.Time.
// The returned time has no monotonic reading (safe for serialization/comparison).
func FromMillis(ms int64) time.Time {
    return time.UnixMilli(ms).UTC()
}
```

**Rationale**:
- The `Clock` interface enables deterministic testing without time-dependent flakiness
- `RealClock` is a zero-allocation implementation (empty struct)
- Helper functions `NowUTCMillis` and `FromMillis` encapsulate the "always persist as epoch millis" rule
- Interface is defined in `domain/` per Clean Architecture (interface near consumer)

**Source**: [testcase/clock package](https://pkg.go.dev/go.llib.dev/testcase/clock) — we don't adopt this library but follow its design principle

#### Mock Clock for Testing

```go
// internal/domain/clock_test.go (or in test packages)

package domain_test

import (
    "sync"
    "time"

    "github.com/example/messaging/internal/domain"
)

// MockClock is a Clock implementation for testing that returns deterministic times.
type MockClock struct {
    mu      sync.Mutex
    current time.Time
}

// NewMockClock creates a MockClock set to the given time.
func NewMockClock(t time.Time) *MockClock {
    return &MockClock{current: t}
}

// Now returns the mock's current time.
func (m *MockClock) Now() time.Time {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.current
}

// Advance moves the mock clock forward by the given duration.
func (m *MockClock) Advance(d time.Duration) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.current = m.current.Add(d)
}

// Set changes the mock clock to a specific time.
func (m *MockClock) Set(t time.Time) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.current = t
}

// Ensure MockClock implements domain.Clock
var _ domain.Clock = (*MockClock)(nil)
```

**Usage in Tests**:

```go
func TestTokenExpiration(t *testing.T) {
    // Arrange
    clock := NewMockClock(time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC))
    token := auth.NewToken(clock, userID, 1*time.Hour)

    // Act & Assert — token valid initially
    assert.False(t, token.IsExpired(clock))

    // Advance time past expiration
    clock.Advance(2 * time.Hour)

    // Assert — token now expired
    assert.True(t, token.IsExpired(clock))
}
```

#### Clock Injection in Services

Application services receive the `Clock` via constructor injection:

```go
// internal/ingest/app/service.go

type PersistService struct {
    clock      domain.Clock
    repo       MessageRepository
    publisher  EventPublisher
}

func NewPersistService(
    clock domain.Clock,
    repo MessageRepository,
    publisher EventPublisher,
) *PersistService {
    return &PersistService{
        clock:     clock,
        repo:      repo,
        publisher: publisher,
    }
}

func (s *PersistService) Persist(ctx context.Context, msg domain.Message) error {
    msg.SetCreatedAt(domain.NowUTCMillis(s.clock))
    // ...
}
```

**Wiring in `main.go`**:

```go
// cmd/ingest/main.go

func main() {
    clock := domain.RealClock{}

    // ... create adapters ...

    persistService := app.NewPersistService(clock, repo, publisher)

    // ... wire up ports ...
}
```

#### Clock Authority Boundary (Normative Rule)

> **Rule: First-Persisting Service is Time Authority**
>
> For any logical event (message creation, membership change, etc.), the service that **first persists** the event is the **time authority** for that event's timestamp.
>
> | Event | Time Authority | Consumers |
> |-------|----------------|-----------|
> | Message `created_at` | Ingest Service | Fanout, Chat Mgmt, Clients |
> | Chat `created_at` | Chat Mgmt Service | Gateway, Clients |
> | Membership `joined_at` | Chat Mgmt Service | Gateway, Fanout |
>
> **Downstream services must treat timestamps as data, not regenerate them.**
>
> ```go
> // ❌ Wrong — Fanout regenerating timestamp
> func (f *FanoutService) Process(event MessageEvent) {
>     event.CreatedAt = f.clock.Now().UnixMilli()  // NEVER DO THIS
> }
>
> // ✅ Correct — Fanout preserving Ingest's timestamp
> func (f *FanoutService) Process(event MessageEvent) {
>     // event.CreatedAt is authoritative from Ingest
>     f.publish(event)  // Forward unchanged
> }
> ```
>
> **Rationale**: Without a single time authority, clock skew between services causes ordering anomalies (message appears "from the future" relative to another service's clock). The first-persister rule ensures a single source of truth and makes latency SLIs (ADR-012 §2.6) meaningful.

---

## Summary of Decisions

| TBD | Decision | Key Points |
|-----|----------|------------|
| **TBD-PR0-1** | Config: koanf with env → AWS SDK → defaults | 12-Factor compliant; koanf preserves key case |
| **TBD-PR0-1** | Secrets: `ReplaceAttr` redaction + `SecretString` type | Defense-in-depth; slog-native |
| **TBD-PR0-1** | **Normative**: Required vs optional config keys | Required key failure → startup failure; optional → defaults |
| **TBD-PR0-2** | Errors: Sentinel errors with per-protocol mappers | Clean separation; golden tests enforce correctness |
| **TBD-PR0-2** | WebSocket codes: 4000-4999 range for app errors | RFC 6455 compliant |
| **TBD-PR0-2** | **Normative**: Rule of Extension for new errors | All mappers + tests updated in same PR |
| **TBD-PR0-3** | Timestamps: `int64` epoch milliseconds UTC | Portable; no monotonic clock in persistence |
| **TBD-PR0-3** | Clock: Injectable `Clock` interface in domain | Clean Architecture; deterministic testing |
| **TBD-PR0-3** | **Normative**: First-persister is time authority | Downstream services preserve, don't regenerate timestamps |

---

## Validation Checklist

### Configuration (TBD-PR0-1)
- [ ] Configuration loads with correct precedence (env overrides AWS SDK overrides defaults)
- [ ] `.env.dev` is git-ignored
- [ ] Required config key missing → process startup failure (exit 1)
- [ ] Optional config key missing → fallback to default + warning log
- [ ] AWS SDK unavailable for required secret → startup failure
- [ ] Logging a struct with `SecretString` fields outputs `[REDACTED]`

### Error Taxonomy (TBD-PR0-2)
- [ ] Golden tests pass for all domain error → gRPC/HTTP/WebSocket mappings
- [ ] Wrapped errors (via `%w`) map to correct wire codes
- [ ] Unknown errors map to `Internal`/500/1011 (never expose details)
- [ ] `ErrDuplicateMessage` returns success with original message (not error response)
- [ ] CI linter verifies every sentinel error has mapper coverage

### Clock Semantics (TBD-PR0-3)
- [ ] `MockClock` enables deterministic time-dependent tests
- [ ] No `time.Time` values are persisted to DynamoDB — only `int64` epoch millis
- [ ] All time comparisons use `Equal()` or epoch comparison, never `==`
- [ ] Downstream services (Fanout, Gateway) never regenerate `created_at` timestamps
- [ ] Ingest Service is sole authority for message `created_at`

---

## References

### Configuration
- [12-Factor App - Config](https://12factor.net/config)
- [koanf - Go configuration library](https://github.com/knadh/koanf)
- [koanf vs Viper comparison](https://itnext.io/golang-configuration-management-library-viper-vs-koanf-eea60a652a22)
- [godotenv - .env file loading](https://github.com/joho/godotenv)

### Secret Redaction
- [Arcjet - Redacting sensitive data in slog](https://blog.arcjet.com/redacting-sensitive-data-from-logs-with-go-log-slog/)
- [masq - slog redaction utility](https://github.com/m-mizutani/masq)
- [Go slog documentation](https://pkg.go.dev/log/slog)

### Error Handling
- [gRPC Error Handling Guide](https://grpc.io/docs/guides/error/)
- [gRPC Status Codes Reference](https://grpc.github.io/grpc/core/md_doc_statuscodes.html)
- [grpc-gateway Error Handling](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/customizing_your_gateway/)
- [Go error handling best practices](https://jayconrod.com/posts/116/error-handling-guidelines-for-go)

### WebSocket
- [RFC 6455 - WebSocket Protocol](https://datatracker.ietf.org/doc/html/rfc6455)
- [WebSocket Close Codes Reference](https://websocket.org/reference/close-codes/)

### Clock & Time
- [Go time package - Monotonic Clocks](https://pkg.go.dev/time#hdr-Monotonic_Clocks)
- [Go monotonic clock proposal](https://go.googlesource.com/proposal/+/master/design/12914-monotonic.md)
- [Victoria Metrics - Go monotonic vs wall clock](https://victoriametrics.com/blog/go-time-monotonic-wall-clock/)
- [testcase/clock package](https://pkg.go.dev/go.llib.dev/testcase/clock)
