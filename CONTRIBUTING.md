# Contributing Guide

This document defines the code standards, development workflow, and architectural conventions for the Realtime Messaging Platform. Every convention here is traceable to an Architecture Decision Record (ADR) — if a rule seems arbitrary, the linked ADR explains why it exists.

For project overview, architecture summary, getting started instructions, repository structure, and Makefile reference, see [README.md](README.md).

## Table of Contents

- [Our Standards](#our-standards)
- [Quick Start for Contributors](#quick-start-for-contributors)
- [Development Workflow](#development-workflow)
- [Clean Architecture](#clean-architecture)
- [Go Standards](#go-standards) *(detailed guide: [docs/STANDARDS-GO.md](docs/STANDARDS-GO.md))*
- [Terraform Standards](#terraform-standards) *(detailed guide: [docs/STANDARDS-TERRAFORM.md](docs/STANDARDS-TERRAFORM.md))*
- [Pull Request Process](#pull-request-process)
- [CI/CD Pipeline](#cicd-pipeline)
- [ADR Process](#adr-process)
- [Quick Reference](#quick-reference)

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

**What must every function that does I/O accept?** `context.Context` as its first parameter. No exceptions — this is CI-enforced. See [Go Conventions](docs/STANDARDS-GO.md#go-conventions).

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

## Go Standards

This project follows the [Go Senior-Level Handbook](https://github.com/ae-lexs/go-senior-level-handbook) as our authoritative Go style guide, enforced by strict CI tooling.

For the complete Go standards — invariants, decision matrices, common mistakes, code conventions, golangci-lint configuration, proto/API conventions, and testing philosophy — see **[docs/STANDARDS-GO.md](docs/STANDARDS-GO.md)**.

**Key invariants** (see full list in the standards doc):

| Category | Rule |
|----------|------|
| Interfaces | Accept interfaces, return structs; define at consumer |
| Errors | Handle or return — never both; wrap with context |
| Context | First parameter, named `ctx`; never store in structs |
| Concurrency | Every goroutine has an owner; use `errgroup` |
| Testing | Fakes over mocks; behavioral contracts over call order |

Enforcement: `golangci-lint` (v2, strict config), `go-arch-lint`, `depguard` — all CI-blocking.

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

Terraform conventions mirror our Go standards: clarity over cleverness, explicit over implicit, minimal complexity for the current task.

For the complete Terraform standards — invariants, file structure, naming, variable/output design, tagging, state management, versioning, security, and validation tooling — see **[docs/STANDARDS-TERRAFORM.md](docs/STANDARDS-TERRAFORM.md)**.

**Key invariants** (see full list in the standards doc):

| Rule | Enforcement |
|------|-------------|
| No credentials in code or `.tfvars` — ever | Code review, trivy |
| Every variable/output has a `description` and `type` | tflint |
| Providers configured only in `environments/`, never in `modules/` | Code review |
| `.terraform.lock.hcl` committed; `.tfstate` never committed | `.gitignore` |

Validation: `terraform fmt`, `tflint`, `trivy`, `terraform validate` — all CI-blocking.

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
8. `terraform fmt -check -recursive` — formatting
9. `tflint --recursive` — linting + documentation enforcement
10. `trivy config terraform/` — security scanning
11. `terraform validate` — configuration validity

### Production Apply Requirements

Production `terraform apply` requires:
1. `terraform plan` output reviewed by infrastructure owner
2. Plan saved to file, applied from that file (not re-planned)
3. Post-apply drift detection (scheduled nightly `terraform plan`)

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
