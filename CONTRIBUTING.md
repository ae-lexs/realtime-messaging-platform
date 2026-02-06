# Contributing Guide

This document defines the code standards, development workflow, and architectural conventions for the Realtime Messaging Platform. Every convention here is traceable to an Architecture Decision Record (ADR) — if a rule seems arbitrary, the linked ADR explains why it exists.

For project overview, architecture summary, getting started instructions, repository structure, and Makefile reference, see [README.md](README.md).

## Our Standards

This project follows the [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook) as our authoritative Go style guide. The handbook emphasizes three core concepts:

- **Invariants** — Rules that must never be violated
- **Lifecycle** — How things start, run, and stop
- **Ownership** — Who is responsible for what

Before contributing, familiarize yourself with the handbook's philosophy: *clarity over cleverness, explicit over implicit, composition over inheritance*.

## Who This Is For

These guidelines favor long-term maintainability over onboarding speed. New contributors are welcome, but we expect familiarity with:

- `context.Context` and cancellation propagation
- Error handling patterns (wrapping, sentinel vs typed errors)
- Goroutine ownership and lifecycle management
- Interface design (small, consumer-defined)
- The Architecture Decision Records in `docs/`

If these concepts are unfamiliar, the [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook) is an excellent starting point.

## Non-Goals

This project does **not** optimize for:

- **Maximum abstraction** — Indirection only when it solves a concrete problem
- **Framework-driven design** — Standard library and explicit wiring preferred
- **Micro-optimizations without evidence** — Profile first, optimize second
- **Consensus-driven style** — `gofmt` decides formatting; the handbook decides patterns

## Go Proverbs (Non-Negotiable)

These proverbs from the Go community inform every decision in this project. They are not guidelines — they are non-negotiable.

1. Don't communicate by sharing memory; share memory by communicating.
2. The bigger the interface, the weaker the abstraction.
3. Make the zero value useful.
4. `interface{}` says nothing.
5. Clear is better than clever.
6. A little copying is better than a little dependency.
7. Gofmt's style is no one's favorite, yet gofmt is everyone's favorite.

> 📖 See [Go Proverbs](https://go-proverbs.github.io/) by Rob Pike

## Quick Start for Contributors

**What will CI block?** Lint failures (`golangci-lint`), architectural boundary violations (`go-arch-lint`, `depguard`), failing tests (unit + integration), proto breaking changes (`buf breaking`), and non-compiling code. Run `make ci-local` before pushing — if it passes locally, CI will pass. See [CI/CD Pipeline](#cicd-pipeline).

**Where do I put new code?** Each service has three layers: `port/` (entry points), `app/` (orchestration), `adapter/` (I/O). Business types go in `internal/domain/`. Dependencies flow inward only — `domain` imports nothing, `app` imports `domain`, ports and adapters import `app`. See [Clean Architecture](#clean-architecture).

**What must every function that does I/O accept?** `context.Context` as its first parameter. No exceptions — this is CI-enforced. See [Go Conventions](#go-conventions).

**Where do ADRs live?** In `docs/ADR-NNN.md`. There are currently 17. Read them before proposing changes to architecture, data flow, or consistency guarantees. See [ADR Process](#adr-process).

**What if I need to break a rule?** If a contribution would violate an existing convention but improves correctness or observability, don't work around it — [propose an ADR first](#when-to-write-an-adr). Rules are changed explicitly, never silently.

## Development Workflow

### Docker-Only Toolchain

Every Makefile target delegates to a Docker container. The `toolbox` service contains Go, `golangci-lint`, `buf`, `arch-go`, and Delve — all pinned to specific versions in `docker/dev.Dockerfile`.

```
make lint            # golangci-lint inside Docker
make test            # go test -race inside Docker
make proto           # buf generate inside Docker
make build           # go build inside Docker
```

To run an ad-hoc command inside the toolbox:

```bash
docker compose -f docker-compose.yaml -f docker-compose.dev.yaml \
  run --rm toolbox "go doc ./internal/domain"
```

### Hot Reload (Air)

`make dev` starts each service with Air file watching. Per-service configs live in `.air/`:

```
.air/
├── gateway.toml
├── ingest.toml
├── fanout.toml
└── chatmgmt.toml
```

Air watches `.go` files, excludes `_test.go` and `gen/`, and rebuilds the relevant `cmd/` binary on change. Named Docker volumes (`go-mod-cache`, `go-build-cache`) persist between restarts for sub-second incremental builds.

### Debugging

Delve is available inside the dev containers on port 2345. Connect your IDE debugger to `localhost:2345` (adjust per service if running multiple simultaneously).

## Clean Architecture

Each service follows the Three Dots Labs interpretation of Clean Architecture with four layers: `port`, `app`, `adapter`, and a shared `domain`. This maps directly to the three-plane separation (ADR-002) and makes architectural boundaries visible in the directory tree — important for a project "intended to be read, reviewed, and reasoned about" (MVP Definition).

### Layer Definitions

**`domain/`** — Pure business logic and types. No external dependencies. Value objects, entities with behavior, domain error types, and business rule constants live here. This is the innermost ring. See [Domain Modeling](#domain-modeling-ddd-lite) below for how domain types are structured.

**`app/`** — Use cases and orchestration. Depends on `domain/` and on interfaces it defines. Coordinates calls to adapters via injected interfaces. Contains no I/O — all external calls go through interfaces.

**`port/`** — Entry points into the service. HTTP handlers, gRPC servers, WebSocket handlers, Kafka consumer entrypoints. Translates external protocols into `app/` calls. Performs request validation, serialization/deserialization, and error mapping to protocol-specific responses.

**`adapter/`** — Implementations of interfaces defined in `app/` or `domain/`. DynamoDB clients, Kafka producers, Redis operations, gRPC clients to other services. This is where I/O lives.

### Boundary vs Core Model

The handbook's boundary/core framing is the conceptual model that our layer definitions implement. Internalize this separation — it drives every architectural decision.

```
┌─────────────────────────────────────────────────────────────────┐
│                     BOUNDARY LAYER                              │
│  HTTP handlers, gRPC servers, CLI commands, DB adapters         │
│  • Creates context with timeouts                                │
│  • Translates errors (domain → HTTP status)                     │
│  • Handles serialization/deserialization                        │
│  • Implements interfaces defined in core                        │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                       CORE LAYER                                │
│  Domain logic, business rules, pure functions                   │
│  • Receives context, respects cancellation                      │
│  • Returns domain errors                                        │
│  • Defines interfaces for dependencies                          │
│  • No knowledge of HTTP, SQL, wire formats                      │
└─────────────────────────────────────────────────────────────────┘
```

**Mapping to this project:** boundary = `port/` + `adapter/`, core = `app/` + `domain/`.

**Dependency rule:** Boundary imports core. Core never imports boundary.

> 📖 See handbook: [Package and Project Design](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/08_PACKAGE_AND_PROJECT_DESIGN.md)

### Dependency Rule

Dependencies flow inward only:

```
port → app → domain
adapter → app → domain
adapter → domain
```

The following imports are **prohibited** and enforced by `go-arch-lint` + `depguard` in CI:

- 🚫 `domain` must not import `app`, `port`, or `adapter` — **CI-enforced**
- 🚫 `app` must not import `port` or `adapter` — **CI-enforced**
- 🚫 `port` must not import `adapter` directly (always goes through `app`) — **CI-enforced**
- 🚫 Only `internal/dynamo/` may import `aws-sdk-go-v2/service/dynamodb` — **CI-enforced**
- 🚫 Only `internal/kafka/` may import `franz-go` — **CI-enforced**
- 🚫 Only `internal/redis/` may import `go-redis` — **CI-enforced**

### Interface Placement

Interfaces are defined **near the consumer**, following idiomatic Go convention:

- `app/` defines interfaces for the adapters it calls (e.g., `MessageRepository`, `EventPublisher`)
- `adapter/` implements those interfaces
- `port/` calls `app/` directly via concrete application service types

Constructor injection in `main.go` wires adapters to application services. No dependency injection frameworks.

### Shared Packages

`internal/domain/`, `internal/dynamo/`, `internal/kafka/`, `internal/redis/`, `internal/auth/`, and `internal/observability/` are shared across services. These are infrastructure adapters and cross-cutting concerns that multiple services depend on. They live outside individual service directories to avoid duplication but remain in `internal/` to prevent external import.

### Domain Modeling (DDD Lite)

The project follows a pragmatic subset of Domain-Driven Design — what Three Dots Labs calls "DDD Lite" — adapted for a distributed systems context. We adopt the tactical patterns that earn their keep in this codebase without the full strategic DDD apparatus. The guiding principle: **types with behavior, not data bags**.

#### Value Objects

All domain identifiers are value objects — types with validation constructors that guarantee "always valid in memory." Never pass raw `string` or `uint64` where a domain concept exists:

```go
// domain/chat_id.go

type ChatID struct {
    value string
}

func NewChatID(raw string) (ChatID, error) {
    if raw == "" {
        return ChatID{}, ErrEmptyChatID
    }
    if _, err := uuid.Parse(raw); err != nil {
        return ChatID{}, fmt.Errorf("invalid chat ID %q: %w", raw, ErrInvalidChatID)
    }
    return ChatID{value: raw}, nil
}

func (id ChatID) String() string { return id.value }
func (id ChatID) IsZero() bool  { return id.value == "" }
```

Key types that **must** be modeled as value objects: `ChatID`, `UserID`, `DeviceID`, `MessageID`, `Sequence`, `ClientMessageID`, `SessionID`. Each carries its own validation — callers cannot construct an invalid instance without explicitly ignoring an error. 🔍

#### Entities with Behavior

Domain entities expose behavior-oriented methods, not setters. Model the language of the domain:

```go
// Good — reflects how the business describes the action
msg, err := chat.SendMessage(userID, content, clientMsgID)
err := membership.Leave(userID)

// Bad — exposes internal state mutation
chat.SetLastSequence(seq + 1)
membership.SetStatus("left")
```

Private fields with public methods enforce invariants at the type level. If a struct can be put into an invalid state by setting a field, that field must be unexported. 🔍

#### Domain Errors

Error types carry failure semantics from ADR-009 (Tier 1/2/3 failures). Domain errors are not raw strings — they are queryable types:

```go
// domain/errors.go

var (
    ErrChatNotFound     = errors.New("chat not found")
    ErrNotMember        = errors.New("user is not a member of this chat")
    ErrDuplicateMessage = errors.New("duplicate client_message_id")
)

func IsRetryable(err error) bool { /* ADR-009 Tier classification */ }
func IsPermissionDenied(err error) bool { /* membership/auth errors */ }
```

#### What We Intentionally Skip

The following DDD patterns are **not adopted** for MVP — the project's complexity lives in distributed systems coordination (sequence allocation, exactly-once delivery, three-plane failure isolation), not in business rule complexity:

- **Full aggregate roots** with transactional consistency boundaries — our transactional boundaries are DynamoDB conditional writes (ADR-004) and Kafka consumer offsets (ADR-011), not domain aggregates
- **Domain events** as first-class types emitted by entities — our events are Kafka messages produced by adapters after DynamoDB writes succeed (ADR-003)
- **CQRS command/query separation** — our read/write split is architectural (REST reads, WebSocket writes per ADR-005/ADR-006), making a separate command/query layer redundant
- **Domain services** — our "processes" are distributed system choreography (persist → produce → consume → fanout), orchestrated by the `app/` layer

These patterns may become valuable post-MVP if business rules grow in complexity (e.g., role-based permissions, message editing with time windows, reaction aggregation). The signal to revisit: when `app/` layer tests start testing business logic instead of orchestration.

## Go Invariants

These rules must never be violated. PRs that break these will not be merged. Each invariant is rooted in the [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook) and reinforced by this project's CI tooling where possible.

### Interfaces

| Rule | Rationale |
|------|-----------|
| The bigger the interface, the weaker the abstraction | Small interfaces are easy to implement, fake, and reason about |
| Accept interfaces, return structs | Decouples callers; they define their own interfaces as needed |
| Define interfaces at the consumer, not the producer | The package that uses a capability defines what it needs |
| Don't design interfaces upfront — discover them | Wait for concrete need: multiple implementations, testing, decoupling |

See [Interface Placement](#interface-placement) for how this project applies these rules in its Clean Architecture layers.

> 📖 See handbook: [Types and Composition](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/02_TYPES_AND_COMPOSITION.md), [Interface Patterns](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/DD_INTERFACE_PATTERNS.md)

### Errors

| Rule | Rationale |
|------|-----------|
| Handle an error or return it — never both | Logging and returning causes duplicate handling |
| Wrap with context: `fmt.Errorf("...: %w", err)` | Error chains should tell a story |
| Translate errors at boundaries | Domain errors → HTTP codes; internals stay hidden |

```go
// ✓ Wrapping with context
if err != nil {
    return fmt.Errorf("processing order %s: %w", orderID, err)
}

// ✓ Boundary translation
if errors.Is(err, order.ErrNotFound) {
    http.Error(w, "order not found", http.StatusNotFound)
    return
}
```

CI enforcement: `errcheck` blocks unchecked errors. `errorlint` enforces `errors.Is`/`errors.As` usage. See [golangci-lint Configuration](#golangci-lint-configuration).

> 📖 See handbook: [Error Philosophy](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/03_ERROR_PHILOSOPHY.md)

### Context

| Rule | Rationale |
|------|-----------|
| Context is the first parameter, named `ctx` | Go convention; enables grep-ability |
| Never store context in structs | Context is request-scoped, not instance-scoped |
| Create at boundaries, propagate through core | Handlers create; domain logic receives |
| Respect cancellation | Check `ctx.Done()` in long-running operations |

```go
// ✓ Correct signature
func (s *Service) Process(ctx context.Context, id string) error

// ✗ Never do this
type Service struct {
    ctx context.Context // Wrong: storing context
}
```

CI enforcement: `contextcheck` and `noctx` linters catch violations. See [golangci-lint Configuration](#golangci-lint-configuration).

> 📖 See handbook: [Context and Lifecycle](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/04_CONTEXT_AND_LIFECYCLE.md)

### Concurrency

| Rule | Rationale |
|------|-----------|
| Every goroutine must have an owner | The starter ensures it can stop |
| Share memory by communicating | Channels for coordination; mutexes for protection |
| Sender owns the channel; receivers never close | Only producers know when there are no more values |
| Use `errgroup` for structured concurrency | Groups goroutines, propagates errors, enables cancellation |

```go
// ✓ Structured concurrency
g, ctx := errgroup.WithContext(ctx)

g.Go(func() error {
    return processA(ctx)
})

g.Go(func() error {
    return processB(ctx)
})

return g.Wait()
```

> 📖 See handbook: [Concurrency Architecture](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/05_CONCURRENCY_ARCHITECTURE.md)

### Graceful Shutdown

| Rule | Rationale |
|------|-----------|
| Shutdown order is reverse of startup order | Dependencies must outlive dependents |
| Every component must have a shutdown path | If it can start, it must be stoppable |
| Shutdown must complete within bounded time | Open-ended shutdown = hanging; SIGKILL is the backstop |

> 📖 See handbook: [Graceful Shutdown](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/06_GRACEFUL_SHUTDOWN.md)

### Slices, Maps & Aliasing

| Rule | Rationale |
|------|-----------|
| Maps are reference types — copying a map copies the header, not the data | Two variables pointing to the same map mutate shared state |
| Slices alias underlying arrays; `append` may or may not reallocate | Mutations through one slice can affect another |
| Never expose internal slices or maps without copying | Callers can mutate your internal state. Return copies |

In this project, this is especially relevant to domain value objects (e.g., chat membership lists in `internal/domain/`). Domain types must return defensive copies of any internal collection. 🔍

> 📖 See handbook: [Types and Composition](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/02_TYPES_AND_COMPOSITION.md)

### Package Design

| Rule | Rationale |
|------|-----------|
| Name packages by responsibility, not by type | `order`, not `models`. Purpose over form |
| Dependencies point inward: boundary → core | Domain logic must not import HTTP, database drivers, etc. |
| `internal/` protects your right to change | Compiler-enforced privacy. Use it aggressively |

This project enforces inward dependencies via `go-arch-lint` + `depguard` in CI — see [Clean Architecture](#clean-architecture) and [Architectural Enforcement](#architectural-enforcement). 🚫

> 📖 See handbook: [Package and Project Design](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/08_PACKAGE_AND_PROJECT_DESIGN.md)

## Decision Matrices

These quick-reference tables help choose between common Go patterns. Each maps to an invariant or enforcement mechanism in this project.

### When to Use What

| If You Need... | Use... | Not... |
|----------------|--------|--------|
| Coordination between goroutines | Channels | Shared memory + mutex |
| Protection of shared state | Mutex | Channel (overkill) |
| Cancellation propagation | `context.Context` — enforced by `contextcheck` linter 🚫 | Custom done channels |
| Multiple implementations | Interface at consumer | Interface at producer |
| Optional parameters | Functional options | Config struct with zero-value ambiguity |
| Required parameters | Explicit constructor args | Functional options |

### Error Type Selection

| Situation | Error Type | Example |
|-----------|------------|---------|
| Expected condition, callers check identity | Sentinel | `var ErrNotFound = errors.New("not found")` — see `internal/domain/errors.go` |
| Callers need structured data | Typed | `type ValidationError struct { Field, Reason string }` |
| Implementation detail, no caller action | Opaque | `fmt.Errorf("internal: %w", err)` — see `internal/errmap/` for boundary translation |

> 📖 See handbook: [Error Philosophy](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/03_ERROR_PHILOSOPHY.md)

### Interface Size Guide

| Methods | Verdict | Examples |
|---------|---------|----------|
| 1 | Ideal | `io.Reader`, `fmt.Stringer`, `http.Handler` |
| 2-3 | Good if cohesive | `io.ReadWriter`, `sort.Interface` |
| 4+ | Needs justification | Split or accept coupling |

> 📖 See handbook: [Interface Patterns](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/DD_INTERFACE_PATTERNS.md)

## Common Mistakes

These mistakes appear frequently in Go codebases. The third column shows how this project catches or prevents each one.

| Mistake | Correct Approach | Project Enforcement |
|---------|------------------|---------------------|
| Fire-and-forget goroutines | Every goroutine has an owner who ensures it stops | `goleak` in tests |
| `time.Sleep` in tests | Use channels, timeouts, synchronization primitives | Code review 🔍 |
| Storing context in structs | Pass context to each method | `contextcheck` linter 🚫 |
| Logging and returning errors | Handle OR return, never both | Code review 🔍 |
| Large interfaces upfront | Discover small interfaces at consumers | Code review 🔍 |
| `pkg/` for everything | Use `internal/`; anything outside is implicitly public | Project convention 📐 |
| Packages named `utils`, `models` | Name by responsibility: `order`, `auth`, `postgres` | `revive` linter 🚫 |
| Core importing boundary | Dependencies point inward only | `go-arch-lint` + `depguard` 🚫 |
| Closing channels from receiver | Sender owns the channel lifecycle | Code review 🔍 |
| Mock-heavy tests | Fakes verify contracts; mocks verify implementation | Code review 🔍 |
| Returning internal slices/maps | Return copies to prevent caller mutation | Code review 🔍 |
| String keys in `context.WithValue` | Use unexported struct types as keys | `staticcheck` 🚫 |

> 📖 See handbook: [Common Mistakes](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/CONTRIBUTING.md)

## Code Standards

Throughout this section, each rule is marked with its enforcement level:

- 🚫 **CI-enforced** — Automated tooling blocks the PR. No exceptions without changing the tool config.
- 🔍 **Review-enforced** — Not caught by CI, but expected in code review. Reviewers should flag violations.
- 📐 **Guideline** — Architectural intent. Follow unless you have a clear reason not to, and document the exception.

### Go Conventions

**Formatting:** `gofmt` via `golangci-lint fmt`. No exceptions. 🚫

**Naming:** Follow [Effective Go](https://go.dev/doc/effective_go) and the Go standard library as the primary style guide. 🔍

| Element | Convention | Example |
|---------|------------|---------|
| Packages | Lowercase, single-word, by responsibility | `order`, `auth`, `postgres` |
| Interfaces | `-er` suffix for single-method | `Reader`, `Handler`, `Validator` |
| Exported | MixedCaps | `ProcessOrder`, `ValidateInput` |
| Unexported | mixedCaps | `parseConfig`, `handleError` |
| Acronyms | All caps | `HTTPServer`, `UserID` |

**Avoid:** `utils`, `common`, `helpers`, `models`, `types` — these reveal nothing about responsibility.

**Avoid stuttering:** `order.Service`, not `order.OrderService`.

> 📖 See handbook: [Package and Project Design](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/08_PACKAGE_AND_PROJECT_DESIGN.md)

**Error handling:** Always check errors. `errcheck` is enabled in CI and blocks PRs on unchecked errors. 🚫 Wrap errors with `fmt.Errorf("context: %w", err)` to preserve the error chain. 🔍 Use sentinel errors (defined in `domain/`) for expected failure conditions that callers need to match. Use `errors.Is()` and `errors.As()` for matching — never compare error strings. 🚫 (`errorlint` enforces this.)

**Context propagation:** Every function that performs I/O or calls downstream services must accept `context.Context` as its first parameter. This is critical for timeout enforcement (ADR-009), graceful shutdown (ADR-014 §4.1), and trace propagation (ADR-012). 🚫 (`contextcheck` and `noctx` linters catch violations.)

**Struct initialization:** Use named fields. Positional initialization is prohibited by `govet`'s `composites` check. 🚫

### golangci-lint Configuration

The project targets **golangci-lint v2** with a strict configuration. The full config lives in `.golangci.yml` at the repo root. Version is pinned in `docker/dev.Dockerfile`.

**Core linters (non-negotiable):**

| Linter | Why | ADR Alignment |
|--------|-----|---------------|
| `errcheck` | Unchecked errors are fatal in failure-heavy distributed code | ADR-009 |
| `staticcheck` | Gold standard Go static analysis | — |
| `govet` | Catches subtle issues (struct tags, printf args, composites) | — |
| `gosec` | Security vulnerabilities | ADR-013 |
| `contextcheck` | Missing or wrong context propagation | ADR-009, ADR-012 |
| `bodyclose` | HTTP response body leaks | — |
| `noctx` | HTTP requests missing context | ADR-009 |

**Strongly recommended (enabled):**

| Linter | Purpose |
|--------|---------|
| `errname` | Error type naming conventions (`ErrFoo`) |
| `errorlint` | Proper `errors.Is`/`errors.As` usage, wrapping |
| `exhaustive` | Exhaustive switch/select on enums |
| `revive` | Configurable style enforcement (successor to golint) |
| `gocritic` | Opinionated but catches real bugs |
| `misspell` | Typos in strings, comments, identifiers |
| `unconvert` | Unnecessary type conversions |
| `unparam` | Unused function parameters |
| `depguard` | Package import restrictions (layer enforcement) |

**Complexity thresholds:**

- `gocyclo` max complexity: **15** — Kafka consumer loops and WebSocket state machines may legitimately exceed 10, but 15 is the ceiling before refactoring is required.
- `gocognit` max complexity: **20** — cognitive complexity is more permissive since deeply nested but linear code reads differently than cyclomatic branching.

**Exclusion presets (v2 format):**

```yaml
linters:
  exclusions:
    presets:
      - comments
      - std-error-handling
      - common-false-positives
    rules:
      - path: _test\.go
        linters: [gocyclo, errcheck, gosec, dupl]
      - path: gen/
        linters: [ALL]
```

Test files get relaxed complexity and error-checking rules. Generated code in `gen/` is excluded entirely.

### Architectural Enforcement

Two complementary tools enforce the dependency rule in CI:

**`go-arch-lint`** validates the Clean Architecture layer boundaries using `.arch-go.yml`. It checks that import paths follow the allowed dependency directions (e.g., `port` may import `app` but not `adapter`). Runs per-service via the monorepo workaround (dynamically adjusting `workdir` per changed service).

**`depguard`** (via golangci-lint) restricts which packages can import specific infrastructure libraries. For example, only `internal/dynamo/` may import `aws-sdk-go-v2/service/dynamodb`. This prevents domain or application code from accidentally depending on infrastructure types.

Both run in CI and block PR merges on violations.

## Proto and API Conventions

### Proto-First Development

All inter-service API changes start with proto file modifications (ADR-014 Decision Driver #4). The workflow:

1. Modify `.proto` files in `proto/messaging/v1/`
2. Run `make proto` to regenerate Go stubs
3. Run `make proto-lint` to validate style
4. Run `make proto-breaking` to check backward compatibility against `main`
5. Implement the service changes
6. Commit everything except `gen/` (git-ignored)

### buf Configuration

Proto tooling uses `buf` CLI, configured in `proto/buf.yaml`. All `buf` commands run from the `proto/` directory (ADR-014 §5.1). The config enforces `STANDARD` lint rules and `FILE`-level breaking change detection.

Generated code lands in `gen/` at the repo root and is **git-ignored** — generated code is not committed. CI regenerates it from proto sources to ensure consistency.

### Proto Style

- Package names follow `messaging.v1` (not `messaging.v1beta1`)
- Field naming: `snake_case`
- Enum values: `SCREAMING_SNAKE_CASE` with `UNSPECIFIED` as the zero value
- One service definition per `.proto` file
- Comments on every RPC method and message field

## Testing

This project follows the [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook)'s testing philosophy. These testing invariants apply to all test code:

| Rule | Rationale |
|------|-----------|
| Fakes over mocks | Fakes verify contracts; mocks verify calls. Fakes survive refactoring |
| Assert behavioral contracts, not call order | Test *what happened*, not *how* |
| Never `time.Sleep` for synchronization | Use channels, polling with timeout, `goleak` |
| Time is a dependency; inject it | Direct `time.Now()` calls are untestable |

> 📖 See handbook: [Testing Philosophy](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/07_TESTING_PHILOSOPHY.md)

### Test Pyramid

ADR-017 defines a four-layer test pyramid. Each layer lives in a specific location:

| Layer | Location | Build Tag | Trigger | Blocking? |
|-------|----------|-----------|---------|-----------|
| Unit tests | `*_test.go` alongside source | none | Every PR | Yes |
| L1: Protocol conformance | `test/conformance/` | `conformance` | Every PR | Yes |
| L2: Contract tests | `test/contract/` | `contract` | Every PR | Yes |
| L3: End-to-end | `test/e2e/` | `e2e` | Every PR | Yes |
| L4: Chaos tests | `test/chaos/` | `chaos` | Nightly + pre-release | Pre-release only |
| Integration tests | `*_test.go` alongside source | `integration` | Every PR | Yes |

### Unit Test Conventions

**Arrange-Act-Assert** is the internal structure of every test. Separate the three phases with blank lines for visual clarity:

```go
func TestNewChatID(t *testing.T) {
    // Arrange
    raw := "550e8400-e29b-41d4-a716-446655440000"

    // Act
    id, err := domain.NewChatID(raw)

    // Assert
    require.NoError(t, err)
    assert.Equal(t, raw, id.String())
}
```

**Table-driven tests** are the default pattern for testing multiple scenarios with identical logic:

```go
func TestAllocateSequence(t *testing.T) {
    tests := []struct {
        name     string
        chatID   domain.ChatID
        wantSeq  uint64
        wantErr  error
    }{
        {
            name:    "first message in chat",
            chatID:  domain.ChatID("chat-1"),
            wantSeq: 1,
        },
        // ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

**BDD-style `t.Run` nesting** for behavior-oriented specs. Use nested subtests to express Given/When/Then without external frameworks — `t.Run` produces hierarchical output with `go test -v`:

```go
func TestChat_SendMessage(t *testing.T) {
    t.Run("given a valid chat membership", func(t *testing.T) {
        // Arrange — shared setup for this context
        chat := domain.NewChat(chatID, "general")
        chat.AddMember(userID)

        t.Run("when sending a message with valid client_message_id", func(t *testing.T) {
            // Act
            msg, err := chat.SendMessage(userID, "hello", clientMsgID)

            // Assert
            t.Run("it assigns a sequence number", func(t *testing.T) {
                require.NoError(t, err)
                assert.Greater(t, msg.Sequence(), uint64(0))
            })
        })

        t.Run("when sending a message as a non-member", func(t *testing.T) {
            // Act
            _, err := chat.SendMessage(otherUserID, "hello", clientMsgID)

            // Assert
            t.Run("it returns a permission error", func(t *testing.T) {
                require.Error(t, err)
                assert.True(t, domain.IsPermissionDenied(err))
            })
        })
    })
}
```

Use **table-driven** when many inputs share identical assertion logic. Use **nested `t.Run`** when different scenarios have distinct setup or assertion structures. Both patterns can coexist in the same package.

**Black-box testing** for domain packages — use `package domain_test` to test only the exported API. This ensures the domain layer's public contract is tested independently of implementation details. 🔍

**Testify** (`github.com/stretchr/testify`) for assertions. Use `require` for fatal assertions (stop the test) and `assert` for non-fatal assertions (continue and report). 🔍

**Integration tests** use the `//go:build integration` build tag and run against the docker-compose infrastructure (`make test-integration` requires `make up` first). 🚫 (build tag enforced by CI — integration tests do not run during `make test`.)

### What to Test

- **Domain layer:** Highest coverage target. Pure functions and value objects — **no mocks, no interfaces, no infrastructure. Ever.** If a domain test requires a mock or test double, the domain layer has leaked concerns — fix the design, not the test. Test constructor validation, behavior methods, error conditions, and business invariants. Domain tests should be the simplest to write and fastest to run in the entire codebase. 🔍
- **Application layer:** Mock adapter interfaces (via `app/`-defined interfaces). Test orchestration logic, error handling paths, and edge cases. Watch for business logic creeping into this layer — if `app/` tests start verifying domain invariants, promote that logic to `domain/`. 🔍
- **Port layer:** Test request/response mapping, validation, and error translation. Use httptest for HTTP handlers. 🔍
- **Adapter layer:** Integration tests against real infrastructure (LocalStack, Redpanda, Redis via docker-compose). 📐

## Pull Request Process

### Self-Review Checklist

Before opening a PR, verify your changes locally and run through this checklist. The commands map to our Docker-based toolchain:

```bash
make lint            # go fmt + go vet + golangci-lint (all linters)
make test            # go test -race ./...
make ci-local        # full CI pipeline locally
```

Then confirm each item:

- [ ] No fire-and-forget goroutines
- [ ] Context passed explicitly, not stored in structs
- [ ] Errors handled OR returned, never both
- [ ] Interfaces defined at consumers, not producers
- [ ] Dependencies point inward (boundary → core)
- [ ] Tests use fakes, not mocks (where applicable)
- [ ] No `time.Sleep` in tests
- [ ] Internal slices/maps not exposed directly

### Before Opening

Run `make ci-local` to verify your changes pass all checks locally. This runs the same pipeline as CI:

```bash
make ci-local        # lint + test + build — if it passes, CI will pass
```

### PR Description Template

```markdown
## What
[One sentence: what does this change?]

## Why
[Context: why is this needed?]

## How Tested
[Manual steps or test coverage]

## Trade-offs
[Any alternatives considered or accepted costs]
```

### Review Expectations

**Authors:**
- Respond to all comments
- Small, focused PRs get reviewed faster
- If you disagree, explain reasoning — be open to being wrong

**Reviewers:**
- Be specific: "This could leak goroutines because..." not "This looks wrong"
- Distinguish blocking issues from suggestions
- Review within 24 hours when possible

## Code Review Checklist

### Correctness
- [ ] Does the code do what it claims?
- [ ] Are error cases handled?
- [ ] Are edge cases considered?

### Clarity
- [ ] Could someone understand this in 6 months?
- [ ] Is there unnecessary cleverness?

### Lifecycle & Ownership
- [ ] Do all goroutines have termination paths?
- [ ] Is context propagated correctly?
- [ ] Are resources cleaned up?

### Invariants
- [ ] Interfaces small and consumer-defined?
- [ ] Errors handled or returned, not both?
- [ ] Dependencies point inward?

## Commit Conventions

This project uses [Conventional Commits](https://www.conventionalcommits.org/). Every commit message follows this format:

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

### Types

| Type | When |
|------|------|
| `feat` | New functionality |
| `fix` | Bug fix |
| `refactor` | Code change that neither fixes a bug nor adds a feature |
| `docs` | Documentation only |
| `test` | Adding or updating tests |
| `chore` | Build, CI, tooling changes |
| `perf` | Performance improvement |

### Scopes

Use the service name or component as scope:

```
feat(gateway): implement WebSocket connection handler
fix(ingest): handle DynamoDB conditional check failure on duplicate sequence
refactor(domain): extract ChatID value object validation
docs(adr): add ADR-018 for rate limiting strategy
test(fanout): add integration test for Kafka consumer rebalance
chore(ci): update golangci-lint to v2.3
chore(docker): optimize dev.Dockerfile layer caching
```

For changes spanning multiple services, use a broader scope (`core`, `infra`, `proto`) or omit the scope.

### Breaking Changes

Append `!` after the type/scope for breaking changes:

```
feat(proto)!: rename PersistMessage to IngestMessage

BREAKING CHANGE: Requires regenerating all proto stubs and updating
gRPC client calls in gateway service.
```

### Unacceptable Commit Messages

Commits must describe *what changed*, not *why you touched the code*:

```
fix stuff
WIP
addressing review comments
```

These will not pass review. Branch commits can be informal since PRs use squash-merge, but the squashed commit message must follow Conventional Commits format.

### Merge Strategy

PRs use **squash-merge** into `main`. The squashed commit message must follow Conventional Commits format. This keeps `main` history linear and each commit deployable. Branch commits can be informal — only the squash message matters.

## Terraform Standards

### File Structure Per Module

Every Terraform module follows HashiCorp's standard layout:

```
terraform/modules/<module-name>/
├── main.tf           # Resources
├── variables.tf      # Input variables
├── outputs.tf        # Output values
├── versions.tf       # Provider + Terraform version constraints
└── README.md         # Module documentation (inputs, outputs, usage)
```

### Naming Conventions

- Resource names: `snake_case` (e.g., `aws_ecs_service.gateway_service`)
- Variable names: `snake_case` (e.g., `gateway_task_cpu`)
- Module references: `snake_case` (e.g., `module.ecs_gateway`)
- No provider configuration inside modules — providers are configured in `environments/`

### Variable Strategy

- No `terraform.tfvars` committed to version control
- Per-environment variable files in `environments/dev/` and `environments/prod/`
- Sensitive values come from Secrets Manager or SSM Parameter Store (ADR-014 §7), never from `.tfvars` files

### Module Versioning

Terraform modules in `terraform/modules/` are internal and consumed via relative paths — not versioned independently. The source of truth is the current commit on `main`. If modules are later extracted to a shared registry, they will be pinned by semantic version.

### Validation Tooling

```
make terraform-fmt        # terraform fmt -recursive
make terraform-validate   # terraform validate
```

All Terraform tooling runs in Docker via the official `hashicorp/terraform` image. No local Terraform installation required.

## CI/CD Pipeline

### Pull Request Checks

Every PR triggers the CI pipeline (ADR-014 §8.2). All stages must pass before merge:

1. `buf lint` + `buf breaking` — proto style and backward compatibility
2. `golangci-lint run` — static analysis (all linters above)
3. `go-arch-lint` — architectural boundary enforcement
4. `go test -race` — unit tests with race detection
5. `go test -tags=integration` — integration tests against docker-compose
6. `go build ./cmd/...` — compilation check
7. `docker build` — production image build verification

### Immutable Artifacts

Docker images are tagged with the Git commit SHA. Images built in CI are the same images deployed to production — no rebuild between environments (ADR-014 §8.1).

## ADR Process

Every non-trivial design decision is documented as an Architecture Decision Record. See [README.md](README.md#architecture-decision-records) for the full ADR index.

### When to Write an ADR

- Introducing a new dependency or replacing an existing one
- Changing architectural boundaries or data flow
- Adding a new service or splitting an existing one
- Modifying consistency, ordering, or delivery guarantees
- Any change that could silently violate an existing ADR

### ADR Format

Follow the [MADR](https://adr.github.io/madr/) template with these required sections: Status, Date, Context and Problem Statement, Decision Drivers, Considered Options (with trade-off analysis), Decision Outcome, Consequences (positive, negative, deferred), and Confirmation (how to validate the implementation).

### Amending Existing ADRs

Later stages may extend but never silently violate earlier decisions (MVP Definition). If a new requirement conflicts with an existing ADR, the resolution is a new ADR that explicitly supersedes or amends the earlier one, documenting the rationale for the change.

### When a Contribution Appears to Violate a Rule

If a change improves correctness, observability, or failure handling but would violate an existing convention or ADR — **stop and propose an ADR first**. Do not work around the rule or submit a PR with a justification comment. Rules in this document are changed explicitly through the ADR process, never silently through precedent. This applies equally to the original author and to contributors.

## Quick Reference

| Category | Invariant |
|----------|-----------|
| Proverbs | Make the zero value useful |
| Proverbs | Gofmt's style is no one's favorite, yet gofmt is everyone's favorite |
| Philosophy | Clear is better than clever |
| Philosophy | A little copying is better than a little dependency |
| Interfaces | The bigger the interface, the weaker the abstraction |
| Interfaces | Accept interfaces, return structs |
| Errors | Handle or return — never both |
| Errors | Translate at boundaries, don't leak internals |
| Context | First parameter, named `ctx`; never store in structs |
| Concurrency | Every goroutine has an owner responsible for termination |
| Concurrency | Share memory by communicating |
| Data Safety | Never expose internal slices or maps without copying |
| Shutdown | Reverse of startup order; bounded time |
| Testing | Fakes over mocks; behavioral contracts over call order |
| Packages | Name by responsibility; dependencies point inward |

## Further Reading

- [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook) — Our authoritative style guide
- [Effective Go](https://go.dev/doc/effective_go) — Official language patterns
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) — Common review feedback
- [Uber Go Style Guide](https://github.com/uber-go/guide) — Additional production patterns
