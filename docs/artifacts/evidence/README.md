# Captured gate output

Live-infrastructure gate runs land here, written by the gate scripts themselves (`scripts/auth.sh`, `scripts/chat.sh` → `capture`).

They are committed because they cannot be regenerated. Infrastructure in this project is provisioned and destroyed within a session ([ADR-021](../../adr/ADR-021.md)); the database a July gate ran against does not exist in November, so re-running to satisfy a reader is not an option. The log is the durable record.

Each file carries a provenance header: capture time, the commit the code was at, the database and region, and a note that re-running requires re-provisioning. **Project identifiers are redacted at capture time**, because these logs are published under [CC-BY-4.0](../../LICENSE).

A log here is what upgrades a claim in the ledger from *Implemented* to *Measured-and-logged*. See [../README.md](../README.md) for the status vocabulary.

Not every log here is a gate. An **experiment** log records a measurement whose
outcome was not known in advance, and its arms assert only that the harness was
valid — the quantity under test is reported, never asserted, because an
assertion on it would delete the finding. The distinction matters when reading
a log: a passing gate means the system behaved as specified, while a passing
experiment means only that the measurement was validly taken.

| File | Gate / experiment | Captured | Claims it evidences |
|---|---|---|---|
| `m1.2-store.log` | `make auth-test` — Firestore auth semantics | 2026-08-04 | [RTM-04](../RTM-04.md) C2, C5 — ten tests, all PASS, 20.8s against `messaging-dev` |
| `m1.2-flow.log` | `make auth-flow` — full OTP → token → refresh → logout against the deployed pod | *pending* — needs a full `make deploy` (GKE + Memorystore), unlike the store half |
| `rtm-04-negative-control.log` | `make auth-negative-control` — experiment: does a transaction lock a query that matched nothing? | 2026-08-07 | [RTM-04](../RTM-04.md) C6, C7 — and the run that withdrew C1 |
| `rtm-04-negative-control-optimistic.log` | the same experiment under `OPTIMISTIC` concurrency | 2026-08-07 | [RTM-04](../RTM-04.md) C8 — the mode switch that corrected C6's consequence |
| `rtm-04-negative-control-pessimistic-recheck.log` | the same experiment, mode restored — the second `A` of an A-B-A | 2026-08-07 | [RTM-04](../RTM-04.md) C8 |
| `m1.3-direct-pair-pessimistic.log` | `make chat-direct-pair` — experiment: the same question on the direct-chat path, which carries no OTP document to confound it | 2026-08-10 | M1.3 — 15 repetitions × 3 arms, 45/45 PASS |
| `m1.3-direct-pair-optimistic.log` | the same experiment under `OPTIMISTIC` concurrency | 2026-08-10 | M1.3 — 15 repetitions × 3 arms, 45/45 PASS |
