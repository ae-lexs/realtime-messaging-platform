# Published artifacts — the claim ledger

Essays in the **Realtime Messaging Series** make claims about this system. This directory is where each claim is tied to the decision that made it, the code that implements it, and the run that proves it.

The point is falsifiability. A published claim that cannot be checked against a specific commit is an opinion with footnotes.

| Artifact | Title | Claims | Status |
|---|---|---|---|
| RTM-04 | The Lock That Was Already There | [RTM-04.md](RTM-04.md) | Draft — pending publication |

## How to cite

Every claim has a permanent ID of the form `RTM-<artifact>-C<n>` — `RTM-04-C2`, for example. **IDs are never reused or renumbered**, including across revisions of the essay, because a published post cites them forever. If a claim is withdrawn, its row stays and its status becomes `Withdrawn`, with the reason.

Each claim carries two kinds of pointer, and both matter:

- **Pinned** — a permalink at an explicit commit SHA: `blob/a74aaed/internal/firestore/auth_tx.go#L83-L112`. Immutable. This is the evidence, and it stays correct forever.
- **Living** — a path and a symbol name: `internal/firestore/auth_tx.go` → `AuthTx.Register`. Survives refactoring and points a reader at current reality.

Never cite `blob/main/…` with line numbers. It resolves after the file changes and silently points at the wrong code, which is worse than not citing at all.

And the opposite failure, which is easy to miss because the link looks correct when you write it: **never leave a claim pinned to a pre-merge branch commit.** This repository squash-merges, so the SHA a feature branch carried never becomes reachable from `main` and is eventually garbage-collected. Such a link is immutable *and* dead — it 404s with no hint that it once worked. Pinning is therefore a two-step operation: cite the branch SHA while the work is in review, then **re-pin to the merged SHA as the first commit after the merge**, before the essay citing it is published.

It is expected that a claim's pinned SHA **predates** its ledger entry. The ledger is written when an essay is drafted; the evidence was produced when the work was done.

## Status vocabulary

Claims are graded, because not all of them are equally earned:

| Status | Meaning |
|---|---|
| **Measured** | A test or gate ran against live infrastructure and produced the stated result. The strongest grade; the evidence column names the run. |
| **Implemented** | The code exists and unit tests cover it, but the claim has not been exercised against real infrastructure. |
| **Specified** | Decided and recorded in an ADR; not yet built. A design claim, and essays must label it as one. |
| **Corrected** | A previously published or recorded claim found to be wrong, with the correction and its reasoning. |
| **Withdrawn** | Retracted. The row stays so the citation still resolves. |

An essay may cite any grade. It may not cite a `Specified` claim in language that implies a `Measured` one.

## Evidence, and why it is committed

`evidence/` holds captured output from live gates.

This is not belt-and-braces. Infrastructure in this project is provisioned and destroyed within a session ([ADR-021](../adr/ADR-021.md)) — `make teardown` is mandatory, and nothing survives to be re-queried. A gate that ran in July against a Firestore database that no longer exists cannot be re-run to satisfy a reader in November. **The captured log is the only durable evidence there is**, so gate scripts write it and it is committed deliberately.

Captured logs are redacted of project identifiers before they are written, because they are published. They carry a provenance header: capture time, the commit the code was at, and a note that re-running requires re-provisioning.

## Licence

This directory is documentation, and `docs/` is [CC-BY-4.0](../LICENSE). Quote it, cite it, republish it — with attribution.
