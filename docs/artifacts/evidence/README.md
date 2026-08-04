# Captured gate output

Live-infrastructure gate runs land here, written by the gate scripts themselves (`scripts/auth.sh` → `capture`).

They are committed because they cannot be regenerated. Infrastructure in this project is provisioned and destroyed within a session ([ADR-021](../../adr/ADR-021.md)); the database a July gate ran against does not exist in November, so re-running to satisfy a reader is not an option. The log is the durable record.

Each file carries a provenance header: capture time, the commit the code was at, the database and region, and a note that re-running requires re-provisioning. **Project identifiers are redacted at capture time**, because these logs are published under [CC-BY-4.0](../../LICENSE).

A log here is what upgrades a claim in the ledger from *Implemented* to *Measured-and-logged*. See [../README.md](../README.md) for the status vocabulary.

| File | Gate | Claims it evidences |
|---|---|---|
| `m1.2-store.log` | `make auth-test` — Firestore auth semantics | [RTM-04](../RTM-04.md) C2, C5 — *pending capture; M1.2 ran before this mechanism existed* |
| `m1.2-flow.log` | `make auth-flow` — full OTP → token → refresh → logout against the deployed pod | *pending capture* |
