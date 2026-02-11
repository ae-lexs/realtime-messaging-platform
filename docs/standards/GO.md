# Go Standards

This document covers both Go philosophy and the technical deep-dive for the Realtime Messaging Platform — invariants, decision matrices, common mistakes, code conventions, proto/API conventions, and testing philosophy.

For project architecture, development workflow, and CI/CD, see [CONTRIBUTING.md](../../CONTRIBUTING.md). For Terraform conventions, see [TERRAFORM.md](TERRAFORM.md).

---

## Our Standards

This project follows the [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook) as our authoritative Go style guide. The handbook emphasizes three core concepts:

- **Invariants** — Rules that must never be violated
- **Lifecycle** — How things start, run, and stop
- **Ownership** — Who is responsible for what

Before contributing Go code, familiarize yourself with the handbook's philosophy: *clarity over cleverness, explicit over implicit, composition over inheritance*.

## Who This Is For

These guidelines favor long-term maintainability over onboarding speed. New contributors are welcome, but we expect familiarity with:

- `context.Context` and cancellation propagation
- Error handling patterns (wrapping, sentinel vs typed errors)
- Goroutine ownership and lifecycle management
- Interface design (small, consumer-defined)
- The Architecture Decision Records in `docs/adr/`

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

## Go Invariants

These rules must never be violated. PRs that break these will not be merged. Each invariant is rooted in the [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook) and reinforced by this project's CI tooling where possible.

### Interfaces

| Rule | Rationale |
|------|-----------|
| The bigger the interface, the weaker the abstraction | Small interfaces are easy to implement, fake, and reason about |
| Accept interfaces, return structs | Decouples callers; they define their own interfaces as needed |
| Define interfaces at the consumer, not the producer | The package that uses a capability defines what it needs |
| Don't design interfaces upfront — discover them | Wait for concrete need: multiple implementations, testing, decoupling |

See [Interface Placement](../../CONTRIBUTING.md#interface-placement) for how this project applies these rules in its Clean Architecture layers.

> See handbook: [Types and Composition](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/02_TYPES_AND_COMPOSITION.md), [Interface Patterns](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/DD_INTERFACE_PATTERNS.md)

### Errors

| Rule | Rationale |
|------|-----------|
| Handle an error or return it — never both | Logging and returning causes duplicate handling |
| Wrap with context: `fmt.Errorf("...: %w", err)` | Error chains should tell a story |
| Translate errors at boundaries | Domain errors -> HTTP codes; internals stay hidden |

```go
// Wrapping with context
if err != nil {
    return fmt.Errorf("processing order %s: %w", orderID, err)
}

// Boundary translation
if errors.Is(err, order.ErrNotFound) {
    http.Error(w, "order not found", http.StatusNotFound)
    return
}
```

CI enforcement: `errcheck` blocks unchecked errors. `errorlint` enforces `errors.Is`/`errors.As` usage. See [golangci-lint Configuration](#golangci-lint-configuration).

> See handbook: [Error Philosophy](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/03_ERROR_PHILOSOPHY.md)

### Context

| Rule | Rationale |
|------|-----------|
| Context is the first parameter, named `ctx` | Go convention; enables grep-ability |
| Never store context in structs | Context is request-scoped, not instance-scoped |
| Create at boundaries, propagate through core | Handlers create; domain logic receives |
| Respect cancellation | Check `ctx.Done()` in long-running operations |

```go
// Correct signature
func (s *Service) Process(ctx context.Context, id string) error

// Never do this
type Service struct {
    ctx context.Context // Wrong: storing context
}
```

CI enforcement: `contextcheck` and `noctx` linters catch violations. See [golangci-lint Configuration](#golangci-lint-configuration).

> See handbook: [Context and Lifecycle](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/04_CONTEXT_AND_LIFECYCLE.md)

### Concurrency

| Rule | Rationale |
|------|-----------|
| Every goroutine must have an owner | The starter ensures it can stop |
| Share memory by communicating | Channels for coordination; mutexes for protection |
| Sender owns the channel; receivers never close | Only producers know when there are no more values |
| Use `errgroup` for structured concurrency | Groups goroutines, propagates errors, enables cancellation |

```go
// Structured concurrency
g, ctx := errgroup.WithContext(ctx)

g.Go(func() error {
    return processA(ctx)
})

g.Go(func() error {
    return processB(ctx)
})

return g.Wait()
```

> See handbook: [Concurrency Architecture](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/05_CONCURRENCY_ARCHITECTURE.md)

### Graceful Shutdown

| Rule | Rationale |
|------|-----------|
| Shutdown order is reverse of startup order | Dependencies must outlive dependents |
| Every component must have a shutdown path | If it can start, it must be stoppable |
| Shutdown must complete within bounded time | Open-ended shutdown = hanging; SIGKILL is the backstop |

> See handbook: [Graceful Shutdown](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/06_GRACEFUL_SHUTDOWN.md)

### Slices, Maps & Aliasing

| Rule | Rationale |
|------|-----------|
| Maps are reference types — copying a map copies the header, not the data | Two variables pointing to the same map mutate shared state |
| Slices alias underlying arrays; `append` may or may not reallocate | Mutations through one slice can affect another |
| Never expose internal slices or maps without copying | Callers can mutate your internal state. Return copies |

In this project, this is especially relevant to domain value objects (e.g., chat membership lists in `internal/domain/`). Domain types must return defensive copies of any internal collection.

> See handbook: [Types and Composition](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/02_TYPES_AND_COMPOSITION.md)

### Package Design

| Rule | Rationale |
|------|-----------|
| Name packages by responsibility, not by type | `order`, not `models`. Purpose over form |
| Dependencies point inward: boundary -> core | Domain logic must not import HTTP, database drivers, etc. |
| `internal/` protects your right to change | Compiler-enforced privacy. Use it aggressively |

This project enforces inward dependencies via `go-arch-lint` + `depguard` in CI — see [Clean Architecture](../../CONTRIBUTING.md#clean-architecture) and [Architectural Enforcement](#architectural-enforcement).

> See handbook: [Package and Project Design](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/08_PACKAGE_AND_PROJECT_DESIGN.md)

## Decision Matrices

These quick-reference tables help choose between common Go patterns. Each maps to an invariant or enforcement mechanism in this project.

### When to Use What

| If You Need... | Use... | Not... |
|----------------|--------|--------|
| Coordination between goroutines | Channels | Shared memory + mutex |
| Protection of shared state | Mutex | Channel (overkill) |
| Cancellation propagation | `context.Context` — enforced by `contextcheck` linter | Custom done channels |
| Multiple implementations | Interface at consumer | Interface at producer |
| Optional parameters | Functional options | Config struct with zero-value ambiguity |
| Required parameters | Explicit constructor args | Functional options |

### Error Type Selection

| Situation | Error Type | Example |
|-----------|------------|---------|
| Expected condition, callers check identity | Sentinel | `var ErrNotFound = errors.New("not found")` — see `internal/domain/errors.go` |
| Callers need structured data | Typed | `type ValidationError struct { Field, Reason string }` |
| Implementation detail, no caller action | Opaque | `fmt.Errorf("internal: %w", err)` — see `internal/errmap/` for boundary translation |

> See handbook: [Error Philosophy](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/03_ERROR_PHILOSOPHY.md)

### Interface Size Guide

| Methods | Verdict | Examples |
|---------|---------|----------|
| 1 | Ideal | `io.Reader`, `fmt.Stringer`, `http.Handler` |
| 2-3 | Good if cohesive | `io.ReadWriter`, `sort.Interface` |
| 4+ | Needs justification | Split or accept coupling |

> See handbook: [Interface Patterns](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/DD_INTERFACE_PATTERNS.md)

## Common Mistakes

These mistakes appear frequently in Go codebases. The third column shows how this project catches or prevents each one.

| Mistake | Correct Approach | Project Enforcement |
|---------|------------------|---------------------|
| Fire-and-forget goroutines | Every goroutine has an owner who ensures it stops | `goleak` in tests |
| `time.Sleep` in tests | Use channels, timeouts, synchronization primitives | Code review |
| Storing context in structs | Pass context to each method | `contextcheck` linter |
| Logging and returning errors | Handle OR return, never both | Code review |
| Large interfaces upfront | Discover small interfaces at consumers | Code review |
| `pkg/` for everything | Use `internal/`; anything outside is implicitly public | Project convention |
| Packages named `utils`, `models` | Name by responsibility: `order`, `auth`, `postgres` | `revive` linter |
| Core importing boundary | Dependencies point inward only | `go-arch-lint` + `depguard` |
| Closing channels from receiver | Sender owns the channel lifecycle | Code review |
| Mock-heavy tests | Fakes verify contracts; mocks verify implementation | Code review |
| Returning internal slices/maps | Return copies to prevent caller mutation | Code review |
| String keys in `context.WithValue` | Use unexported struct types as keys | `staticcheck` |

> See handbook: [Common Mistakes](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/CONTRIBUTING.md)

## Code Standards

Throughout this section, each rule is marked with its enforcement level:

- **CI-enforced** — Automated tooling blocks the PR. No exceptions without changing the tool config.
- **Review-enforced** — Not caught by CI, but expected in code review. Reviewers should flag violations.
- **Guideline** — Architectural intent. Follow unless you have a clear reason not to, and document the exception.

### Go Conventions

**Formatting:** `gofmt` via `golangci-lint fmt`. No exceptions. CI-enforced.

**Naming:** Follow [Effective Go](https://go.dev/doc/effective_go) and the Go standard library as the primary style guide. Review-enforced.

| Element | Convention | Example |
|---------|------------|---------|
| Packages | Lowercase, single-word, by responsibility | `order`, `auth`, `postgres` |
| Interfaces | `-er` suffix for single-method | `Reader`, `Handler`, `Validator` |
| Exported | MixedCaps | `ProcessOrder`, `ValidateInput` |
| Unexported | mixedCaps | `parseConfig`, `handleError` |
| Acronyms | All caps | `HTTPServer`, `UserID` |

**Avoid:** `utils`, `common`, `helpers`, `models`, `types` — these reveal nothing about responsibility.

**Avoid stuttering:** `order.Service`, not `order.OrderService`.

> See handbook: [Package and Project Design](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/08_PACKAGE_AND_PROJECT_DESIGN.md)

**Error handling:** Always check errors. `errcheck` is enabled in CI and blocks PRs on unchecked errors. CI-enforced. Wrap errors with `fmt.Errorf("context: %w", err)` to preserve the error chain. Review-enforced. Use sentinel errors (defined in `domain/`) for expected failure conditions that callers need to match. Use `errors.Is()` and `errors.As()` for matching — never compare error strings. CI-enforced (`errorlint` enforces this.)

**Context propagation:** Every function that performs I/O or calls downstream services must accept `context.Context` as its first parameter. This is critical for timeout enforcement (ADR-009), graceful shutdown (ADR-014 §4.1), and trace propagation (ADR-012). CI-enforced (`contextcheck` and `noctx` linters catch violations.)

**Struct initialization:** Use named fields. Positional initialization is prohibited by `govet`'s `composites` check. CI-enforced.

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

> See handbook: [Testing Philosophy](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/07_TESTING_PHILOSOPHY.md)

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

**Black-box testing** for domain packages — use `package domain_test` to test only the exported API. This ensures the domain layer's public contract is tested independently of implementation details. Review-enforced.

**Testify** (`github.com/stretchr/testify`) for assertions. Use `require` for fatal assertions (stop the test) and `assert` for non-fatal assertions (continue and report). Review-enforced.

**Integration tests** use the `//go:build integration` build tag and run against the docker-compose infrastructure (`make test-integration` requires `make up` first). CI-enforced (build tag enforced by CI — integration tests do not run during `make test`.)

### What to Test

- **Domain layer:** Highest coverage target. Pure functions and value objects — **no mocks, no interfaces, no infrastructure. Ever.** If a domain test requires a mock or test double, the domain layer has leaked concerns — fix the design, not the test. Test constructor validation, behavior methods, error conditions, and business invariants. Domain tests should be the simplest to write and fastest to run in the entire codebase. Review-enforced.
- **Application layer:** Mock adapter interfaces (via `app/`-defined interfaces). Test orchestration logic, error handling paths, and edge cases. Watch for business logic creeping into this layer — if `app/` tests start verifying domain invariants, promote that logic to `domain/`. Review-enforced.
- **Port layer:** Test request/response mapping, validation, and error translation. Use httptest for HTTP handlers. Review-enforced.
- **Adapter layer:** Integration tests against real infrastructure (LocalStack, Redpanda, Redis via docker-compose). Guideline.

## Observability Conventions

This section codifies the tracing and observability patterns used across all services. These conventions ensure consistent instrumentation that is filterable by service and layer in trace backends.

> See handbook: [Context and Lifecycle](https://github.com/ae-lexs/go-senior-level-handbook/blob/main/04_CONTEXT_AND_LIFECYCLE.md)

### Tracer Initialization

Each architectural layer declares a package-level tracer using the naming convention `"{service}/{layer}"`:

```go
var tracer = otel.Tracer("chatmgmt/adapter")
```

- Declared in the layer's `doc.go` or primary file
- Naming convention enables filtering by service (`chatmgmt/*`) or layer (`*/adapter`) in trace backends
- One tracer per package — never per-struct or per-function

Code reference: `internal/chatmgmt/adapter/doc.go:5–7`, `internal/chatmgmt/app/auth_service.go:16`

### Span-per-Operation

Every adapter method and every app-layer flow starts a span:

```go
ctx, span := tracer.Start(ctx, "dynamo.otp.create")
defer span.End()
```

- Naming convention: `"{component}.{operation}"` (e.g., `"dynamo.otp.create"`, `"auth.request_otp"`)
- Always reassign `ctx` from `tracer.Start` — child spans must propagate via context
- Always `defer span.End()` immediately after creation

Code reference: `internal/chatmgmt/adapter/dynamo_otp.go:97–98`

### Semantic Attributes

Infrastructure calls include OpenTelemetry semantic convention attributes:

```go
span.SetAttributes(
    attribute.String("db.system", "dynamodb"),
    attribute.String("db.operation", "PutItem"),
)
```

- `db.system` and `db.operation` are required for all database adapter spans
- Additional attributes (table name, key) may be added when they aid debugging without leaking PII

Code reference: `internal/chatmgmt/adapter/dynamo_otp.go:99–102`

### Error Recording

On infrastructure or unexpected errors, record on the span and set error status:

```go
span.RecordError(err)
span.SetStatus(codes.Error, err.Error())
```

- **Record**: infrastructure errors, marshaling failures, unexpected SDK responses
- **Do not record**: domain errors that are expected flow (e.g., `domain.ErrNotFound` returned by a lookup) — these are returned without span recording since they represent normal business outcomes, not failures

### Context Checkpoints

Multi-step adapter operations must check `ctx.Err()` between steps to respect cancellation:

```go
// After GSI query returns, check context before follow-up GetItem.
if err := ctx.Err(); err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
    return nil, fmt.Errorf("user store: find by phone: %w", err)
}
```

- Check `ctx.Err()` before expensive operations (network calls, CPU-intensive work)
- Especially important between a GSI query and a follow-up consistent read, or between sequential DynamoDB calls

Code reference: `internal/chatmgmt/adapter/dynamo_users.go:144–149`

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
| Observability | Package-level tracer: `var tracer = otel.Tracer("service/layer")` |
| Observability | Every adapter and app method starts a span |
| Observability | Check `ctx.Err()` between multi-step adapter operations |

## Further Reading

- [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook) — Our authoritative style guide
- [Effective Go](https://go.dev/doc/effective_go) — Official language patterns
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) — Common review feedback
- [Uber Go Style Guide](https://github.com/uber-go/guide) — Additional production patterns
