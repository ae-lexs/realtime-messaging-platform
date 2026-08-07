# RTM-04 — The Lock That Was Already There

**Essay:** *The Lock That Was Already There* — Realtime Messaging Series, Artifact 04 (draft, pending publication). Supersedes the *Lock What Isn't There* draft, whose central claim this ledger now records as **Withdrawn**.
**Subject:** uniqueness under concurrency in a document store; the auth re-home of Module 1.2
**Decisions annotated:** [ADR-015](../adr/ADR-015.md) (authentication and OTP), [ADR-023](../adr/ADR-023.md) v1.2 / v1.3 (where the state landed)
**Work:** PR [#15](https://github.com/ae-lexs/realtime-messaging-platform/pull/15) (M1.2, merged `05ae733`), PR [#16](https://github.com/ae-lexs/realtime-messaging-platform/pull/16) (ADR-023 v1.3, merged `a74aaed`), PR [#17](https://github.com/ae-lexs/realtime-messaging-platform/pull/17) (this ledger, merged `c6ee34b`)
**Pinned at:** `a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71` for C1–C5. **C6 and C7 pin to the negative-control commit**, named in their own rows.

---

## Claims

### RTM-04-C1 — A query that matches nothing acquires no lock

**Status:** **Withdrawn** — falsified by [C6](#rtm-04-c6--a-transaction-is-aborted-by-a-write-to-a-range-its-query-read) on 2026-08-07. The row stays so the citation resolves.

~~A Firestore transaction detects conflicts over the documents it *reads*. A query returning zero documents leaves the transaction with no claim on the absence it observed, so two concurrent first-time registrations of one phone number both observe "no user", both commit, and produce two accounts with no error raised anywhere.~~

**Why it was wrong.** The claim was never measured. It was inferred from Firestore's documented locking scope — *"Transactions place locks on the documents they read"* — and from the genealogy of document stores, both of which are accurate and neither of which is evidence about the unguarded path. The five-racer gate that appeared to support it (C2) only ever ran *with* the sentinel in place. When the negative control was finally built, a transaction whose only read was a query matching **zero** documents was aborted by a concurrent insert, in every repetition. Strong consistency does govern staleness rather than serialization — that distinction survives — but it is not what was happening here.

| | |
|---|---|
| **Decision** | [ADR-023](../adr/ADR-023.md) — Firestore model, the `phone_index` paragraph; [ADR-015](../adr/ADR-015.md) §5.1 |
| **Superseded by** | [C6](#rtm-04-c6--a-transaction-is-aborted-by-a-write-to-a-range-its-query-read) — measured, opposite result |
| **Note** | Withdrawing this does **not** withdraw the sentinel. See C6's *Consequence* row for the justification that measurement supports. |

### RTM-04-C2 — Five concurrent registrations of one phone number yield exactly one user

**Status:** **Corrected** — the result holds; the mechanism named in the original wording does not. See [C7](#rtm-04-c7--the-five-racer-gate-never-exercises-the-sentinel).

Five concurrent registrations of one phone number, all holding the same valid OTP and released from a shared barrier, yield **exactly one** committed user. Losers return either "phone already claimed" or "OTP already consumed", and leave no user behind. That much is measured and stands.

**The correction.** The claim was originally worded *"Materialising the conflict makes exactly one registration win"*, attributing the outcome to `phone_index`. Attribution was never measured: the gate's loser assertion accepts either error and records neither. When the refusals were counted (C7), the sentinel refused **zero** losers across five repetitions and the OTP document refused all twenty. `AuthTx.run` reads *and* writes `otp_requests/{phoneHash}` on every path, so every racer for one phone number contends on that single document — it is a second materialised conflict, keyed by the same value, and on this path it is the one doing the work.

| | |
|---|---|
| **Decision** | [ADR-015](../adr/ADR-015.md) §5.1 (the `PHONE#` sentinel), re-homed by [ADR-023](../adr/ADR-023.md) v1.2 |
| **Pinned** | [`internal/firestore/auth_tx.go#L83-L112`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/auth_tx.go#L83-L112) — `Register`, the four-document write set<br>[`internal/firestore/auth_tx.go#L136-L184`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/auth_tx.go#L136-L184) — `AuthTx.run`, the OTP read-and-write that surrounds it<br>[`internal/firestore/documents.go#L139-L144`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/documents.go#L139-L144) — `PhoneIndexDoc`, written and never read |
| **Living** | `internal/firestore/auth_tx.go` → `AuthTx.Register` |
| **Proof** | [`internal/firestore/auth_integration_test.go#L294-L351`](https://github.com/ae-lexs/realtime-messaging-platform/blob/a74aaed4a9ddd10ecb195a2a61dec9ec801c7f71/internal/firestore/auth_integration_test.go#L294-L351) — `TestConcurrentRegistrationYieldsExactlyOneUser`, `racers = 5`, build tag `integration`, run against a live database via `make auth-test`. **Proves the outcome, not the mechanism** — see C7 |
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
| **Re-examine** | The wording above inherits C1's premise — *"the query matches nothing"* was offered as the reason two creates would both succeed, and C6 falsified that. Direct chats carry no OTP in their transaction, so the confound behind C2/C7 does not apply and C6 transfers **directly**: on this configuration the naive in-transaction query would hold. The construction is still worth building, but for what survives C8: the mobile and web client libraries cannot express a range, and a behaviour outside the written contract cannot be cited. That is an argument, not a measurement, and nothing has been measured on this path. |

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

### RTM-04-C6 — A transaction is aborted by a write to a range its query read

**Status:** **Measured** — 2026-08-07. Falsifies [C1](#rtm-04-c1--a-query-that-matches-nothing-acquires-no-lock).

Five transactions, released from one barrier, each query `users` by `phone_number`, each see zero documents, each create a user at a freshly generated UUID. Nothing is shared between them but the absence: no OTP, no sentinel, no common write target, no possible collision on a document path. **One user is created. Four of the five racers are aborted by the store and retried**, and on the retry their query returns the winner, so they decline. Identical in all five repetitions; twenty of twenty-five racers aborted; the winner varied (racers 4, 3, 1, 4, 0).

The control is what licenses the reading. **Arm D** is the same test with the query deleted and nothing else changed — same barrier, same racer count, same collection, same phone number, same fresh IDs. It produces **five users and zero retries**, every repetition. The query is the only difference between the arms, so the aborts are the query's doing: Firestore detected a conflict on a range that no document satisfied.

**Scope.** Measured on the Go **server** SDK against a Standard-edition database with `concurrency_mode` unset, therefore PESSIMISTIC. Not measured: Enterprise edition (documented to default OPTIMISTIC), the mobile and web SDKs (documented to "always emulate optimistic concurrency" using per-document version preconditions, which cannot express a range), or query shapes other than one equality predicate with `Limit(1)` on the automatic single-field index.

| | |
|---|---|
| **Decision** | None — this claim is about the substrate, not a decision. It revises the premise of [ADR-015](../adr/ADR-015.md) §5.1 and of [ADR-023](../adr/ADR-023.md)'s `phone_index` paragraph |
| **Pinned** | [`internal/firestore/phantom_integration_test.go#L104-L210`](https://github.com/ae-lexs/realtime-messaging-platform/blob/2ff47ce2b532574e17dab89a503a334fad8c3d8a/internal/firestore/phantom_integration_test.go#L104-L210) — arm A<br>[`internal/firestore/phantom_integration_test.go#L222-L294`](https://github.com/ae-lexs/realtime-messaging-platform/blob/2ff47ce2b532574e17dab89a503a334fad8c3d8a/internal/firestore/phantom_integration_test.go#L222-L294) — arm D, the control |
| **Living** | `internal/firestore/phantom_integration_test.go` → `TestConcurrentInsertsBehindAnEmptyQuery`, `TestConcurrentInsertsWithNoQueryAtAll` |
| **Proof** | `racers = 5`, `REPS=5`, build tag `integration`, run via `make auth-negative-control`. The instrument is `attempts[i]++` inside the transaction callback: the SDK re-invokes the callback per retry, so the counter distinguishes *the store detected a conflict* from *the racers never overlapped*. Without it, "one user" is uninterpretable |
| **Captured run** | [`evidence/rtm-04-negative-control.log`](evidence/rtm-04-negative-control.log) — captured 2026-08-07 against `messaging-dev` in `us-central1`, infrastructure destroyed the same evening |
| **Consequence** | The sentinel is **not** withdrawn. The protection exceeds the documented contract (*"Transactions place locks on the documents they read"*), so it cannot be cited, and it is absent by construction from the mobile and web client libraries. ⚠ This row originally also claimed the protection rode on a concurrency-mode default nobody selected — **[C8](#rtm-04-c8--the-range-protection-is-invariant-to-the-databases-concurrency-mode) measured that and withdrew it** |

### RTM-04-C7 — The five-racer gate never exercises the sentinel

**Status:** **Measured** — 2026-08-07. Corrects [C2](#rtm-04-c2--five-concurrent-registrations-of-one-phone-number-yield-exactly-one-user).

The shipped gate was re-run unmodified with every loser's refusal attributed to the document that produced it. Across five repetitions: one winner each time, **zero refusals from `phone_index`**, and twenty from the OTP assertion. `AuthTx.run` reads and then writes `otp_requests/{phoneHash}`; every racer for one phone number addresses that same document, and its key is derived from the contended value. It is a second materialised conflict that nobody designed as one, and on the registration path it arbitrates the race before the sentinel is ever reached.

The gate is not wrong to accept either error — which one a loser sees genuinely depends on which document its transaction reached first, and pinning it to one would make the test flaky for a correct system. The defect is that a tolerant assertion was never paired with a count, so the test tolerated a state (the sentinel firing zero times) that invalidates the claim it was cited for.

| | |
|---|---|
| **Decision** | [ADR-015](../adr/ADR-015.md) §5.1 — the sentinel whose effect this fails to observe |
| **Pinned** | [`internal/firestore/phantom_integration_test.go#L379-L444`](https://github.com/ae-lexs/realtime-messaging-platform/blob/2ff47ce2b532574e17dab89a503a334fad8c3d8a/internal/firestore/phantom_integration_test.go#L379-L444) — arm C, refusals attributed<br>[`internal/firestore/phantom_integration_test.go#L308-L369`](https://github.com/ae-lexs/realtime-messaging-platform/blob/2ff47ce2b532574e17dab89a503a334fad8c3d8a/internal/firestore/phantom_integration_test.go#L308-L369) — arm B, `Register` minus the sentinel: one user, all losers on the OTP |
| **Living** | `internal/firestore/phantom_integration_test.go` → `TestConcurrentRegistrationLoserErrors`, `TestConcurrentRegistrationWithoutTheSentinel` |
| **Captured run** | [`evidence/rtm-04-negative-control.log`](evidence/rtm-04-negative-control.log) — `sentinel refusals: 0` / `OTP refusals: 4` in each of five repetitions |

### RTM-04-C8 — The range protection is invariant to the database's concurrency mode

**Status:** **Measured** — 2026-08-07. **Corrects the *Consequence* row of [C6](#rtm-04-c6--a-transaction-is-aborted-by-a-write-to-a-range-its-query-read) as first written**, which asserted that the protection rode on a concurrency-mode default this project never selected.

Concurrency mode is a database-level setting, and server client libraries use whatever the database is set to: Standard edition defaults to PESSIMISTIC, Enterprise edition to OPTIMISTIC. That made "the guarantee depends on a switch nobody chose" an obvious inference — and it was an inference, of exactly the kind C1 was withdrawn for. The switch was flipped and arm A re-run unchanged, then flipped back, so the comparison is **A-B-A within one database** rather than across sessions.

| Mode | Reps | Users created | Racers aborted and retried |
|---|---|---|---|
| PESSIMISTIC — as created, `concurrency_mode` unset in Terraform | 5 | **1**, every rep | 4 of 5, every rep |
| OPTIMISTIC — switch flipped | 5 | **1**, every rep | 4 of 5 in four reps; one rep where the racers never overlapped |
| PESSIMISTIC — flipped back | 5 | **1**, every rep | 1–4, varying with overlap |

**Fifteen repetitions, seventy-five racers, one user every time.** The range is defended under both modes. The documentation describes neither case: pessimistic transactions *"place locks on the documents they read"*, optimistic ones *"don't use database locks to block other operations from changing data"*, and no range appears in either sentence.

Also confirmed by query rather than inference: the module's unset `concurrency_mode` produces `PESSIMISTIC`.

**What remains unmeasured** is a client library, not a setting — the mobile and web SDKs *"always emulate optimistic concurrency"* using per-document version preconditions, which structurally cannot express a range. Enterprise edition as a product tier was not tested; only its default mode was.

| | |
|---|---|
| **Decision** | None. It narrows C6's consequence and, with it, the surviving justification for `phone_index` and for [C3](#rtm-04-c3--the-same-construction-generalises-to-direct-pair-uniqueness) |
| **Living** | `scripts/auth.sh` → `mode` (read or set the concurrency mode); `internal/firestore/phantom_integration_test.go` → `TestConcurrentInsertsBehindAnEmptyQuery` |
| **Proof** | `REPS=5` per mode, arm A and its control unchanged between runs. `LOG_SUFFIX` keeps each mode's capture in its own file so neither overwrites the other |
| **Captured run** | [`evidence/rtm-04-negative-control-optimistic.log`](evidence/rtm-04-negative-control-optimistic.log) and [`evidence/rtm-04-negative-control-pessimistic-recheck.log`](evidence/rtm-04-negative-control-pessimistic-recheck.log), both 2026-08-07; the first PESSIMISTIC run is [`evidence/rtm-04-negative-control.log`](evidence/rtm-04-negative-control.log) |
| **Consequence** | The sentinel still stands, on two grounds and no longer on three: the mobile and web client path, and the fact that a behaviour absent from the written contract cannot be cited or held anyone to. "It depends on a setting" is withdrawn |

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

# C6 and C7 — the negative control, same infrastructure
PROJECT_ID=<your-project> REPS=5 make auth-negative-control   # writes evidence/rtm-04-negative-control.log

# C8 — the same arms under the other concurrency mode
PROJECT_ID=<your-project> ./scripts/auth.sh mode optimistic
PROJECT_ID=<your-project> REPS=5 LOG_SUFFIX=-optimistic make auth-negative-control
PROJECT_ID=<your-project> ./scripts/auth.sh mode pessimistic

PROJECT_ID=<your-project> BILLING_ACCOUNT_ID=<your-billing> make auth-down
```

`make auth-negative-control` is a **separate target from `make auth-test` on purpose**: it writes its own log, so re-running the experiment cannot overwrite the captured run that C2 and C5 cite. Two of its four arms assert only that the harness was valid and report the quantity under test — the outcomes were registered before the run, both directions were informative, and a test that failed on one of them would delete the finding it exists to record.

`make auth-test` runs `internal/firestore` with the `integration` build tag and `-count=1`, so Go's test cache cannot report a pass without touching the database.

## Changelog

| Version | Date | Changes |
|---|---|---|
| v0.4 | 2026-08-07 | **C8 added, and it corrects C6's own consequence.** C6 was recorded with the claim that the measured protection rode on a `concurrency_mode` default this project never selected — an inference from the documented Standard/Enterprise split, with no measurement under it, which is the defect C1 was withdrawn for. The switch was flipped and arm A re-run unchanged, then flipped back: **A-B-A in one database, fifteen repetitions, seventy-five racers, one user every time under both modes.** C8 records that; C6's *Consequence* row is corrected in place with a pointer rather than rewritten silently; C3's *Re-examine* row drops the same retracted reason. What survives is the mobile/web client path and citability. Apparatus: `scripts/auth.sh mode` to read or set the mode, and `LOG_SUFFIX` so a second run cannot overwrite the first. |
| v0.3 | 2026-08-07 | **The negative control ran, and two claims did not survive it.** C1 → **Withdrawn**: a transaction whose only read was a query matching zero documents was aborted by a concurrent insert, in five of five repetitions, with the query-deleted control producing five users and zero retries. C2 → **Corrected**: the outcome stands, the mechanism named in its wording does not — the sentinel refused zero losers and the OTP document refused all twenty, so the original run proves uniqueness on the registration path but not the sentinel's part in it. C3 gains a **Re-examine** row, since its stated justification inherited C1's premise and direct chats have no OTP to confound them. **C6** (the range is defended, with arm D as the control and per-racer abort counts as the instrument) and **C7** (the gate never exercises the sentinel) added as **Measured**. Reproduction gains `make auth-negative-control`, deliberately a separate target so the experiment cannot overwrite the log C2 and C5 cite. The sentinel survives all of it, on the ground stated in C6's *Consequence* row: the protection is a platform default this project never selected and exceeds the documented contract, so it cannot be cited. |
| v0.2 | 2026-08-04 | Evidence gap closed. The store gate was re-provisioned and re-run with capture in place; C2 and C5 now cite `evidence/m1.2-store.log` rather than test source alone, and the *Evidence gap* section became *Evidence provenance*, recording why the log's header names the ledger branch instead of the M1.2 merge. Infrastructure destroyed after the run. |
| v0.1 | 2026-08-04 | Initial ledger for the *Lock What Isn't There* draft. Five claims: the phantom (C1), the measured five-racer uniqueness gate (C2), the direct-chat generalisation as an explicit design claim (C3), the unreachable race recovery and the over-permissive fake (C4), and TTL-as-garbage-collection with the missing third disjunct (C5). Evidence gap for M1.2's live runs recorded rather than hidden; capture lands with this commit for M1.3 onward. |
