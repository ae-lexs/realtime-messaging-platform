# RTM-04 — Lock What Isn't There

**Essay:** *Lock What Isn't There* — Realtime Messaging Series, Artifact 04 (draft, pending publication)
**Subject:** uniqueness under concurrency in a document store; the auth re-home of Module 1.2
**Decisions annotated:** [ADR-015](../adr/ADR-015.md) (authentication and OTP), [ADR-023](../adr/ADR-023.md) v1.2 / v1.3 (where the state landed)
**Work:** PR [#15](https://github.com/ae-lexs/realtime-messaging-platform/pull/15) (M1.2, merged `05ae733`), PR [#16](https://github.com/ae-lexs/realtime-messaging-platform/pull/16) (ADR-023 v1.3, merged `a74aaed`)
**Pinned at:** `a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71` — every line reference below resolves at this commit.

---

## Claims

### RTM-04-C1 — A query that matches nothing acquires no lock

**Status:** Implemented (the mechanism), Measured (its consequence — see C2)

A Firestore transaction detects conflicts over the documents it *reads*. A query returning zero documents leaves the transaction with no claim on the absence it observed, so two concurrent first-time registrations of one phone number both observe "no user", both commit, and produce two accounts with no error raised anywhere. Strong consistency does not prevent this: it governs staleness, not serialization.

| | |
|---|---|
| **Decision** | [ADR-023](../adr/ADR-023.md) — Firestore model, the `phone_index` paragraph; [ADR-015](../adr/ADR-015.md) §5.1 |
| **Pinned** | [`internal/firestore/auth_tx.go#L136-L184`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/auth_tx.go#L136-L184) — the read-then-write ordering the store forces, and why it happens to be the ordering the invariant wants |
| **Living** | `internal/firestore/auth_tx.go` → `AuthTx.run` |
| **Note** | The claim is about what the store does *not* do, so the code is evidence of the workaround rather than of the defect. C2 is where it becomes measurable. |

### RTM-04-C2 — Materialising the conflict makes exactly one registration win

**Status:** **Measured**

A document whose ID is derived from the contended value — `phone_index/{sha256(phone)}` — created with `tx.Create` inside the registration transaction, converts an unlockable absence into a document ID the store can refuse. Five concurrent registrations of one phone number, all holding the same valid OTP and released from a shared barrier, yield **exactly one** committed user. Losers return either "phone already claimed" or "OTP already consumed" depending on which document their transaction reached first, and leave no user behind.

| | |
|---|---|
| **Decision** | [ADR-015](../adr/ADR-015.md) §5.1 (the `PHONE#` sentinel), re-homed by [ADR-023](../adr/ADR-023.md) v1.2 |
| **Pinned** | [`internal/firestore/auth_tx.go#L83-L112`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/auth_tx.go#L83-L112) — `Register`, the four-document write set<br>[`internal/firestore/documents.go#L139-L144`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/documents.go#L139-L144) — `PhoneIndexDoc`, written and never read<br>[`internal/firestore/collections.go#L18-L21`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/collections.go#L18-L21) — the collection, and why the name it inherits is misleading |
| **Living** | `internal/firestore/auth_tx.go` → `AuthTx.Register` |
| **Proof** | [`internal/firestore/auth_integration_test.go#L294-L351`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/auth_integration_test.go#L294-L351) — `TestConcurrentRegistrationYieldsExactlyOneUser`, `racers = 5`, build tag `integration`, run against a live database via `make auth-test` |
| **Captured run** | [`evidence/m1.2-store.log`](evidence/m1.2-store.log) — `--- PASS: TestConcurrentRegistrationYieldsExactlyOneUser (2.91s)`, captured 2026-08-04 against `messaging-dev` in `us-central1`, code at `cdf3c09` |

### RTM-04-C3 — The same construction generalises to direct-pair uniqueness

**Status:** **Specified** — not implemented until Module 1.3

"Is there a direct chat containing A and B?" has the same shape as "is there a user with this phone number?": before the chat exists, the query matches nothing. Two concurrent creates would produce two chat IDs for one conversation, each accumulating its own sequence numbers and message history. `direct_chats/{min(a,b)}__{max(a,b)}` applies the same construction, with the pair sorted lexicographically so both argument orders address one document.

| | |
|---|---|
| **Decision** | [ADR-023](../adr/ADR-023.md) v1.3, binding [ADR-016](../adr/ADR-016.md) §2.1 (the mechanism) and ADR-006 §4.1 (the canonical ordering) |
| **Pinned** | [`docs/adr/ADR-023.md`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/docs/adr/ADR-023.md) — the `direct_chats` paragraph in the Firestore model |
| **Living** | `docs/adr/ADR-023.md` → Firestore model → `direct_chats` |
| **Proof** | None yet. Essays citing this claim must label it a design claim. |

### RTM-04-C4 — A documented race recovery was never reachable, on either substrate

**Status:** **Corrected** — [ADR-015](../adr/ADR-015.md) v1.4

[ADR-015](../adr/ADR-015.md) §10.2 specified that a caller losing the phone-sentinel race should re-read the winner and "proceed as existing user". It cannot: the winner consumed the OTP inside the same all-or-nothing write set, so the loser presents a code whose status is already `verified` to a transaction that asserts `status = pending` by design. The redirect always terminated in `INVALID_OTP`, one round trip later than a direct refusal.

The defect was **not introduced by the migration**. The original DynamoDB design placed the same condition on the same item in the same transaction, so the row was unimplementable when it was written in February and stayed that way for five months. The unit test that covered the path passed because its fake transactor accepted a consumed OTP — **the fake was more permissive than the store it stood for**, and so certified a path the real store refuses.

| | |
|---|---|
| **Decision** | [ADR-015](../adr/ADR-015.md) §10.2 as corrected in v1.4 |
| **Pinned** | [`internal/chatmgmt/app/auth_verify_otp.go#L191-L239`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/chatmgmt/app/auth_verify_otp.go#L191-L239) — the refusal that replaced the redirect<br>[`internal/firestore/auth_tx.go#L188-L205`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/auth_tx.go#L188-L205) — `OTPCondition.assert`, the `status = pending` assertion that makes the redirect impossible |
| **Living** | `internal/chatmgmt/app/auth_verify_otp.go` → `AuthService.verifyOTPNewUser` |
| **Proof** | `internal/chatmgmt/app/auth_verify_otp_test.go` → *"phone sentinel race: the spent OTP is refused, not retried as a login"* — asserts the refusal, that the login transaction is never reached with a consumed code, and that the loser makes no second lookup. The assertions fail against the previous implementation. |

### RTM-04-C5 — TTL is garbage collection, never the correctness gate

**Status:** **Implemented**, with one sub-claim **Corrected** ([ADR-015](../adr/ADR-015.md) v1.2)

Firestore deletes expired documents within roughly 24 hours of the timestamp, not at it. For a five-minute OTP the document outlives its validity by nearly a day, so presence carries no information about validity and every read must consult `expires_at` in code. The consequence for issuing: ADR-015 §1.2's conditional write has three disjuncts — absent, **already verified**, or past expiry — and cannot be expressed as `Create()`, which covers only the first. The shipped implementation had two of the three; the missing `verified` disjunct meant a user who had just logged in could not request a new code until the spent one aged out.

| | |
|---|---|
| **Decision** | [ADR-015](../adr/ADR-015.md) §1.2 (three disjuncts; the missing one corrected in v1.2); [ADR-023](../adr/ADR-023.md) v1.2 (the TTL-lag consequence) |
| **Pinned** | [`internal/firestore/stores.go#L414-L449`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/stores.go#L414-L449) — `OTPRequests.Create` as a transaction, with the reasoning for each disjunct<br>[`internal/firestore/documents.go#L223-L225`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/documents.go#L223-L225) — `OTPRequestDoc.IsExpired`, the gate TTL is not |
| **Living** | `internal/firestore/stores.go` → `OTPRequests.Create` |
| **Proof** | [`internal/firestore/auth_integration_test.go#L47-L91`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/auth_integration_test.go#L47-L91) — `TestOTPCreateRefusesALiveOTPButNotAnExpiredOne`, whose second half is precisely the lockout case |
| **Captured run** | [`evidence/m1.2-store.log`](evidence/m1.2-store.log) — `--- PASS: TestOTPCreateRefusesALiveOTPButNotAnExpiredOne (2.30s)`, with `TestOTPCreateReplacesAConsumedOTP (2.09s)` covering the third disjunct that was missing before v1.2 |

---

## Evidence provenance

Module 1.2's original gates ran in July against a database that was destroyed at end of session, and their output was not captured — the capture mechanism landed with this ledger, not before. Rather than cite those lost runs, **the store gate was re-run on 2026-08-04** against freshly provisioned infrastructure, with capture in place, and that run is the evidence behind C2 and C5.

Two things follow, and both are properties of the deploy-and-destroy discipline rather than accidents:

- The captured log's provenance header names commit `cdf3c09` — the ledger branch, not the M1.2 merge — because that is the code that actually executed. The claims are about `internal/firestore`, which is byte-identical between the two; the capture mechanism is the only difference.
- The infrastructure is destroyed again. This log is not a convenience copy of something re-runnable; it is the record, and re-running requires re-provisioning (~2 minutes, Firestore and secrets only).

Nine of the ten tests in that file exercise live Firestore; the remaining pair are pure doc-ID functions that run without a database. The whole gate took 20.8s of wall clock, which is itself evidence it was not served from Go's test cache — `-count=1` in `scripts/auth.sh` forbids that, for exactly this reason.

## Reproducing these claims

```bash
# Unit tests — no cloud, hermetic
make test PKG=./internal/firestore/... ./internal/chatmgmt/...

# C2 and C5 — live Firestore, provisions and destroys
PROJECT_ID=<your-project> BILLING_ACCOUNT_ID=<your-billing> make auth-up
PROJECT_ID=<your-project> make auth-test     # writes evidence/m1.2-store.log
PROJECT_ID=<your-project> make auth-down
```

`make auth-test` runs `internal/firestore` with the `integration` build tag and `-count=1`, so Go's test cache cannot report a pass without touching the database.

## Changelog

| Version | Date | Changes |
|---|---|---|
| v0.2 | 2026-08-04 | Evidence gap closed. The store gate was re-provisioned and re-run with capture in place; C2 and C5 now cite `evidence/m1.2-store.log` rather than test source alone, and the *Evidence gap* section became *Evidence provenance*, recording why the log's header names the ledger branch instead of the M1.2 merge. Infrastructure destroyed after the run. |
| v0.1 | 2026-08-04 | Initial ledger for the *Lock What Isn't There* draft. Five claims: the phantom (C1), the measured five-racer uniqueness gate (C2), the direct-chat generalisation as an explicit design claim (C3), the unreachable race recovery and the over-permissive fake (C4), and TTL-as-garbage-collection with the missing third disjunct (C5). Evidence gap for M1.2's live runs recorded rather than hidden; capture lands with this commit for M1.3 onward. |
