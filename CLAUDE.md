# Claude Code — Working Agreement

This file defines how Claude works on this project. For code standards, architecture, and conventions, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Session Bootstrapping

At the start of every session, read these two files before doing any work:

1. **CONTRIBUTING.md** — hub for code standards, Clean Architecture, process, CI/CD
2. **docs/STANDARDS-GO.md** — Go invariants, code conventions, testing philosophy
3. **docs/STANDARDS-TERRAFORM.md** — Terraform invariants, state management, security
4. **docs/EXECUTION_PLAN.md** — current PR scope, acceptance criteria, correctness invariants

Then, based on the task, read the relevant ADRs using the traceability matrix in EXECUTION_PLAN.md. Also reference **docs/TBD-PR0-DECISIONS.md** for config, error taxonomy, and clock conventions.

## Workflow

### 1. Understand the task

Read the relevant documentation before proposing anything. When the user asks to explore best practices or industry standards, research thoroughly — web search, documentation, existing patterns in the codebase — before recommending an approach.

### 2. Plan before implementing

Prefer **plan mode** for non-trivial changes. A change is non-trivial if it touches multiple files, introduces new patterns, or involves architectural decisions.

During planning:
- Decompose the work into **commit-sized units**. Each commit should ship working functionality — compiles, lints, tests pass.
- Identify decisions that need the user's attention, guidance, or approval. Surface these early, not mid-implementation.
- Reference specific ADRs, CONTRIBUTING.md sections, or EXECUTION_PLAN acceptance criteria that govern the task.

Skip planning for trivial changes: one-liner fixes, CI config, typos, simple bug fixes with obvious solutions.

### 3. Implement with quality gates

Before every commit, verify:
- `make lint` passes
- `make test` passes
- Code follows Clean Architecture boundaries (domain <- app <- port/adapter)
- Errors are wrapped with context or handled — never both, never ignored
- Every I/O function accepts `context.Context` as first parameter
- Tests ship with the code, not after
- Observability (traces, metrics, structured logging) is woven in, not deferred

### 4. Commit and push

Follow Conventional Commits format (see CONTRIBUTING.md). Each commit is a coherent, working unit.

## Build & Test

No local Go toolchain. All commands run via Docker:

```
make lint       # golangci-lint (v2, strict config)
make test       # go test -race -v ./...
make ci-local   # full CI pipeline locally
```

Ad-hoc commands: `docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox <cmd>`

## What to Avoid

- **Empty stubs or placeholder implementations** — if code exists, it must be called and tested (EXECUTION_PLAN Principle 1)
- **Skipping error wrapping** — always `fmt.Errorf("context: %w", err)`
- **Deferring tests** — tests are part of the implementation, not a follow-up task
- **Ignoring ADR constraints** — if a task conflicts with an ADR, flag it to the user instead of working around it
- **Over-engineering** — solve the current task, not hypothetical future requirements
