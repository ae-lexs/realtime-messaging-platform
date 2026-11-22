# Captured gate output

Live-infrastructure gate runs land here, written by the gate scripts themselves (`scripts/auth.sh`, `scripts/chat.sh` → `capture`).

They are committed because they cannot be regenerated. Infrastructure in this project is provisioned and destroyed within a session ([ADR-021](../../adr/ADR-021.md)); the database a July gate ran against does not exist in November, so re-running to satisfy a reader is not an option. The log is the durable record.

Each file carries a provenance header: capture time, the commit the code was at, the database and region, and a note that re-running requires re-provisioning. **Project identifiers are redacted at capture time**, because these logs are published under [CC-BY-4.0](../../LICENSE).

A log here is what upgrades a claim in the ledger from *Implemented* to *Measured-and-logged*. See [../README.md](../README.md) for the status vocabulary. Which capability each log evidences, and how to re-check it on a live stack, is in the capability documents: [`docs/features/`](../../features/README.md).

Not every log here is a gate. An **experiment** log records a measurement whose
outcome was not known in advance, and its arms assert only that the harness was
valid — the quantity under test is reported, never asserted, because an
assertion on it would delete the finding. The distinction matters when reading
a log: a passing gate means the system behaved as specified, while a passing
experiment means only that the measurement was validly taken.

| File | Gate / experiment | Captured | Claims it evidences |
|---|---|---|---|
| `m1.1-store.log` | `go test -tags=integration` (the four `*RoundTrip` tests) + `gcloud firestore fields ttls list` — gate, operator-driven: the M1.1 Firestore tier against the live database | 2026-11-22 | M1.1 gate — both TTL policies `ACTIVE`; CRUD round trip passes for users, chats, memberships and sessions; the derived membership ID is 74 bytes; an expired session reads back with `IsExpired` true; unknown IDs wrap `domain.ErrNotFound`. "Destroy removes the database" is logged separately in `m1.1-teardown.log` |
| `m1.1-teardown.log` | timed `scripts/teardown.sh`, phase by phase, with `terraform state rm` of the two TTL fields before destroy — experiment, plus an API verification read | 2026-11-22 | M1.1 gate's "destroy removes the database" (no database listed, state at 0, nothing billable); the database destroy took **1 s** once the TTL fields left state; total teardown 1,606 s, bound by the subnetwork (15m47s) |
| `m1.2-store.log` | `make auth-test` — Firestore auth semantics | 2026-08-04 | [RTM-04](../RTM-04.md) C2, C5 — ten tests, all PASS, 20.8s against `messaging-dev` |
| `m1.2-flow.log` | `scripts/auth.sh flow` — gate: full OTP → registration → replay refused → second-device login → refresh → reuse detected, session revoked → logout → per-phone rate limit, against the deployed pod | 2026-11-22 | M1.2 deployed-flow gate — all 7 steps pass. Previously *pending*; captured in the PR #32 live session |
| `m1.2-otp-reissue.log` | `scripts/auth.sh otp-reissue` — experiment: does reissuing an OTP reset the failed-attempt count? (NIST SP 800-63B) | 2026-11-22 | M1.2 defect — the reissue **resets** `attempt_count` (4 → 0) and the lockout never fires; the Redis per-phone verify limit still stops guessing at 5 failures per 15 minutes |
| `m1.3-listchats.log` | `scripts/chat.sh listchats` + `listchats-cost` — gate: per-caller `ListChats` fields against the deployed pod, plus the `chat.list` span tree from Cloud Trace | 2026-11-22 | M1.3 open item 1 — myMembership on every item, otherMember on both direct items, mutedUntil on the muted group; **3 Firestore round trips** for the page (50.7 ms) |
| `m1-telemetry.log` | `scripts/chat.sh telemetry` — raw Cloud Trace v1 and Managed Prometheus API responses | 2026-11-22 | Module 1 telemetry proof — an `auth.verify_otp` span tree with k8s attributes; `auth_otp_requests_total` and `chat_created_total` series whose values match the traffic sent |
| `rtm-04-negative-control.log` | `make auth-negative-control` — experiment: does a transaction lock a query that matched nothing? | 2026-08-07 | [RTM-04](../RTM-04.md) C6, C7 — and the run that withdrew C1 |
| `rtm-04-negative-control-optimistic.log` | the same experiment under `OPTIMISTIC` concurrency | 2026-08-07 | [RTM-04](../RTM-04.md) C8 — the mode switch that corrected C6's consequence |
| `rtm-04-negative-control-pessimistic-recheck.log` | the same experiment, mode restored — the second `A` of an A-B-A | 2026-08-07 | [RTM-04](../RTM-04.md) C8 |
| `m1.3-store.log` | `make chat-test` — gate: chat creation, membership mutation, and the direct-pair invariant through the shipped path | 2026-08-10 | M1.3 — 18 tests, all PASS; the concurrency gate records `created 1 / replayed 4 / refused 0` |
| `m1.3-direct-pair-pessimistic.log` | `make chat-direct-pair` — experiment: the same question on the direct-chat path, which carries no OTP document to confound it | 2026-08-10 | M1.3 — 15 repetitions × 3 arms, 45/45 PASS |
| `m1.3-direct-pair-optimistic.log` | the same experiment under `OPTIMISTIC` concurrency | 2026-08-10 | M1.3 — 15 repetitions × 3 arms, 45/45 PASS |
