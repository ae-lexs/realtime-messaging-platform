# Feature Dossier — Module 1: Identity & Membership

- **Status**: Living document (v1.0)
- **Created**: 2026-11-22
- **Covers**: M1.1 Firestore identity tier · M1.2 OTP, tokens and sessions · M1.3 chats and membership · `ListChats` membership fields
- **Plan authority**: [`docs/EXECUTION_PLAN.md`](../EXECUTION_PLAN.md) owns milestone status. This dossier explains features; it does not restate the ADRs.

> **What a Feature Dossier is.** Every PR that ships a feature or touches the execution plan carries one (Guiding Principle 9). Its job is to make a feature *understandable*: what problem it solves, which ADR sentences govern it, where the ideas come from outside this repo, what we doubt, what we actually measured, and what we cannot yet measure. ADR passages are quoted, never summarised. External sources are linked, and every link was fetched on 2026-11-22. Statements without a label are verified against code, git, committed evidence or a fetched source. **[Inference]** marks reasoning, and **[Doubt — mine]** marks a question the author proposes rather than one raised in discussion.

M1.1–M1.3 are written **retrospectively**. The `ListChats` section is written **forward-looking from its live gate onward**: the code exists on `feat/m1.3-listchats-membership`, which has no PR and is not merged, and its gate has not run.

---

## Map

```mermaid
flowchart LR
    subgraph M11["M1.1 Firestore tier · done"]
        FS[("Firestore native<br/>users · chats · memberships · sessions<br/>TTL on sessions.expires_at")]
    end
    subgraph M12["M1.2 Auth · done"]
        OTP["OTP request / verify<br/>otp_requests + phone_index"]
        SES["Sessions + token rotation<br/>reuse detection"]
        REDIS[("Memorystore<br/>rate limits · revocation<br/>fail closed")]
    end
    subgraph M13["M1.3 Chats · done"]
        AUTHZ["AuthenticatedChatServer<br/>bearer + revocation check"]
        CHAT["ChatService<br/>authorization ADR-016 §8"]
        DC["direct_chats sentinel<br/>group cap in-transaction"]
    end
    LC["ListChats membership fields<br/>open · branch, gate not run"]
    M14["M1.4 Idempotency-Key<br/>open · not started"]
    M22["M2.2 Ingest<br/>future reader of memberships"]
    OBS["OTel export PR #28<br/>Cloud Trace · Managed Prometheus"]

    OTP --> SES
    SES --> FS
    OTP --> FS
    OTP --> REDIS
    SES --> REDIS
    AUTHZ --> REDIS
    AUTHZ --> CHAT
    CHAT --> DC
    DC --> FS
    CHAT --> LC
    CHAT --> M14
    FS --> M22
    CHAT -.-> OBS
    SES -.-> OBS
    style LC fill:#fff3e0,stroke:#e65100
    style M14 fill:#fff3e0,stroke:#e65100
    style M22 fill:#eceff1,stroke:#607d8b
```

**How the pieces connect.** An OTP verification registers or logs in a user and creates a **session**. The access token (a JWT) carries the user and the session. Every chat RPC passes through `AuthenticatedChatServer`, which validates the token and checks the Redis revocation list; then `ChatService` authorizes the call against the caller's **membership** document. M2.2 will validate each message's sender with the same membership point lookup, `memberships/{chat_id}__{user_id}`, which is why M1.3 → M2.2 is on the critical path.

---

## M1.1 — Firestore identity tier

PR #14, merged 2026-07-26.

### Problem

Without a typed entity store there is nowhere authoritative to keep users, chats, memberships and sessions. Every later feature would read and write ad-hoc documents with no shared ID scheme, and "is X in chat Y?", the question M1.3 asks on every call and M2.2 will ask on every message, would need a query instead of a strongly consistent point lookup. Sessions would accumulate forever, or rely on a sweeper the project chose not to run.

### ADR context

- **ADR-021, Decision A** (Accepted): "**Decision A: a polyglot split** — Cloud SQL for PostgreSQL owns the ordered write path; Firestore (native mode) owns entity/document state."
- **ADR-023 v1.4, "Firestore model (identity & membership)"**: "**`sessions` TTL**: a **Firestore TTL policy on `expires_at`** replaces the DynamoDB `ttl` attribute — native, no sweeper. **But TTL deletion is not instantaneous** — Firestore removes expired documents within ~24 h of `expires_at`, so an expired session can still be *readable* for up to a day. The auth service therefore **must validate `expires_at` in application code** on every `get(sessions/{id})` and reject an expired session; TTL is garbage collection, never the correctness gate."
- **ADR-023 v1.4, "Cross-store integrity (application-enforced)"**: "the auth service **must** check `sessions.expires_at` in code on every `get(sessions/{id})` and reject an expired session. Firestore TTL (≤24 h lag) is garbage collection, never the correctness gate (see Firestore model)."

### Origins

| Source | What it actually says | Where we agree or differ |
|---|---|---|
| Google Cloud, *Firestore TTL* — https://cloud.google.com/firestore/docs/ttl | "Data is typically deleted within 24 hours after its expiration date." "Expired documents continue to appear in queries and lookup requests until the TTL process actually deletes them." | **Agree.** This is exactly why ADR-023 makes expiry an application check and TTL only collection. |
| Google Cloud, *Firestore best practices* — https://cloud.google.com/firestore/docs/best-practices | "Do not use monotonically increasing document IDs such as: `Customer1`, `Customer2`… Such sequential IDs can lead to hotspots that impact latency." | **Agree.** ADR-023 v1.1 chose UUID identifiers over ULIDs (whose time prefix is monotonic). The membership ID `{chat_id}__{user_id}` therefore starts with a random UUID. |
| Google Cloud, *Quotas and limits* — https://cloud.google.com/firestore/docs/quotas | Document IDs "Must be no longer than 1,500 bytes", "Cannot contain a forward slash", "Cannot match the regular expression `__.*__`". | **Agree.** The composite ID is 74 bytes and uses `__` only as an inner separator, never as a prefix/suffix pair. |
| HashiCorp, *`google_firestore_database`* — https://github.com/hashicorp/terraform-provider-google/blob/main/website/docs/r/firestore_database.html.markdown | "`deletion_policy` - (Optional) … Defaults to ABANDON. … When set to "ABANDON", the command will remove the resource from Terraform management" (it leaves the database in place). | **Differ, deliberately.** ABANDON makes `terraform destroy` report success while the database and its data survive. This repo sets `DELETE`, because every session must end at zero resources (ADR-021). |

### Doubts

- **[Doubt — mine]** The ~6-minute Firestore field deletion dominates every teardown. Is that a fixed cost of the TTL field resource, or could the TTL policy be left in place across sessions without leaving a billable database behind? Not investigated.
- **[Doubt — mine]** M1.1's gate (CRUD round-trip, TTL policy `ACTIVE`, destroy removes the database) is recorded in the plan's status line but has **no committed evidence log**. See seed doubt 2 under M1.2. The same question applies here.

### Experiments

- **Live gate** against `messaging-dev` (recorded in `EXECUTION_PLAN.md`, M1.1 status line):
  - the CRUD round-trip passes for all four collections;
  - the 74-byte composite membership ID serves the point lookup and both list directions from one document;
  - an expired session reads back with `IsExpired` true;
  - the TTL policy reports `state: ACTIVE`.
- **Overturned assumption: `terraform destroy` removes the database.** It does not by default. Measured, and fixed with `deletion_policy = DELETE`.
- **Overturned assumption: the ID is 54 bytes.** ADR-023 computed 54 bytes from ULIDs; with UUIDs it is 74. The plan's gate line carried the stale 54 until v2.8.
- **Measured: teardown time.** Deleting a `google_firestore_field` takes **~6 minutes**.

### Measurement

- **What shows it works:** unit tests for document shapes and tags (`internal/firestore/documents_test.go`), and the integration suites that every later module runs against the live database. These *use* M1.1's stores, so a broken store fails them.
- **Not measured:**
  - no committed M1.1 gate log;
  - no metric for Firestore read/write counts or latency from the application;
  - no measurement of TTL lag (how long expired sessions actually linger).

---

## M1.2 — OTP, tokens and sessions

PR #15, merged 2026-07-26.

### Problem

Without authentication no other RPC can know who is calling, and every membership check downstream is meaningless. Each auth weakness has a specific consequence:
- **Unthrottled OTPs** can be brute-forced: six digits is a 10⁶ space.
- **Unrotated refresh tokens** turn one leaked token into a session that lasts 30 days, with no way to notice the theft.
- **A non-atomic registration** lets two first-time verifications for one phone number create two users.
- **A revocation check that "fails open"** when Redis is down silently honours logged-out tokens.

### ADR context

- **ADR-015 v1.4, §1.2 OTP Generation** (the procedure; storage re-homed by ADR-023 v1.2):
  > "1. otp = crypto/rand → uniform integer in [000000, 999999] … 3. expires_at = now + 5 minutes … ConditionExpression: attribute_not_exists(phone_hash) OR #status = "verified" OR #expires_at < :now (Prevents overwriting an active, unverified OTP)"
- **ADR-015 v1.4, §4.2 Rotation with Reuse Detection**:
  > "IF old_hash = session.prev_token_hash: → REUSE DETECTED: This is the immediately previous token being replayed. This likely indicates token theft … → REVOKE ENTIRE SESSION"
- **ADR-013, Security Authority Invariant**:
  > "Redis failures in security-critical paths MUST fail closed (deny access)"
- **ADR-023 v1.4, Firestore model:** the session-expiry rule quoted under M1.1 applies here too. Expiry is enforced in code; TTL only collects.

### Origins

| Source | What it actually says | Where we agree or differ |
|---|---|---|
| NIST SP 800-63B (Rev. 4) — https://pages.nist.gov/800-63-4/sp800-63b.html | For out-of-band authentication, "the authentication SHALL be considered invalid unless completed within 10 minutes." "If the authentication secret is less than 64 bits long, the verifier SHALL implement a rate-limiting mechanism that effectively limits the total number of consecutive failed authentication attempts… Generating a new authentication secret SHALL NOT reset the failed authentication count." It sets specific requirements for PSTN (SMS/voice) delivery in Sec. 3.1.3.3. | **Agree** on validity: our 5 minutes is inside the 10-minute ceiling. **Agree** that a 6-digit secret needs rate limiting: there are per-OTP attempt caps plus Redis phone/IP limits and a phone lockout. **Differ, possibly:** reissuing an OTP **resets** the per-OTP `attempt_count` (`internal/firestore/stores.go`), which NIST forbids for "the failed authentication count". Whether the Redis lockout bounds total consecutive failures across reissues is not verified (see Doubts). We send no SMS at all today (ADR-015 v1.3, `LogSMSProvider` only), so the PSTN requirements are not yet live. |
| RFC 9700, *OAuth 2.0 Security Best Current Practice*, §4.14 — https://www.rfc-editor.org/rfc/rfc9700 | §2.2.2: "Refresh tokens for public clients MUST be sender-constrained or use refresh token rotation." §4.14: "If a refresh token is compromised and subsequently used by both the attacker and the legitimate client, one of them will present an invalidated refresh token, which will inform the authorization server of the breach… it will revoke the active refresh token." | **Agree** on rotation and on revoking after a detected reuse; we revoke the whole session. **Differ in scope:** we detect reuse only for the *immediately previous* token (`prev_token_hash`). An older invalidated token is rejected with 401 but does **not** revoke the session. ADR-015 §4.2 justifies this by bounded storage. [Inference] This is a narrower reading than RFC 9700's "an invalidated refresh token". |
| RFC 7519, *JSON Web Token*, §4.1.4 — https://www.rfc-editor.org/rfc/rfc7519 | Defines the `"exp"` (Expiration Time) claim. | **Agree.** Access tokens carry `exp`; lifetime is `AccessTokenLifetime = 1 * time.Hour` (`internal/domain/constants.go`). |
| RFC 8725, *JWT Best Current Practices*, §3.1 — https://www.rfc-editor.org/rfc/rfc8725 | §3.1, "Perform Algorithm Verification": do not trust the token's own `alg` header. | **Agree.** The verifier pins `jwt.WithValidMethods([]string{"RS256"})` and type-checks the method (`internal/auth/jwt.go`). |

### Doubts

- **From voice discussion, 2026-11-22: what is a gate claim worth without a committed artifact?** The plan's M1.2 status says the deployed flow gate (OTP → registration → login → refresh → reuse-revokes-session → logout → rate limit) was met live. But `docs/artifacts/evidence/README.md` lists `m1.2-flow.log` as *pending*. A claim with no artifact can't be re-checked, and the evidence README's own vocabulary only upgrades a claim to *Measured* with a log. [Inference] Until a log exists, the claim should be graded *Implemented*, not *Measured*. Part of the flow (registration, then authenticated calls) is re-exercised by `m1.3-flow.log`, but refresh, reuse detection and logout are not.
- **[Doubt — mine]** Does reissuing an OTP let an attacker reset the guess budget? The per-OTP counter resets (verified). The phone-level lockout and request-rate limit live in Redis. Do they bound *total* consecutive failures the way NIST 800-63B requires? Needs a test that alternates reissue and wrong guesses.
- **[Doubt — mine]** RFC 9700 treats any invalidated refresh token as evidence of a breach; we act only on the immediately previous one. Is the bounded-storage argument still right now that sessions live in Firestore, which could hold a short history?

### Experiments

- **`m1.2-store.log`** (captured 2026-08-04, live `messaging-dev`): **28 distinct top-level tests, 0 failures, 20.8 s.** That's the package's hermetic unit tests plus the live auth tests; the evidence README counts ten live ones. Shown:
  - a live OTP is refused while an expired one is replaced (the TTL-lag window);
  - attempt increments are atomic;
  - registration commits everything or nothing;
  - **concurrent registrations yield exactly one user**;
  - rotation is single-use;
  - `expires_at` survives the round trip byte-for-byte.
- **Overturned premise: "a query matching nothing locks nothing."** `rtm-04-negative-control*.log` (2026-08-07) measured the opposite on the registration path; RTM-04 claim C1 was withdrawn. `phone_index` is kept on other grounds (ADR-023 v1.4).
- **Defects only the live deploy found** (all fixed in PR #15):
  1. **Key discovery was ungrantable.** `secretmanager.secrets.list` can only be granted project-wide, so the pod crash-looped. Keys are now resolved by name.
  2. **A consumed OTP blocked reissue** for the rest of its 5-minute window. ADR-015's condition has three disjuncts; the code had two.
  3. **protojson dropped `false` booleans** from REST responses. The bridge now sets `EmitUnpopulated`.

### Measurement

- **What shows it works:**
  - The live store gate above.
  - The unit tests for every auth branch.
  - Six business counters:
    - `auth_otp_requests_total`, `auth_token_minted_total` and `auth_session_created_total`, labelled by `flow`/`status`;
    - `security_auth_failures_total`, with 9 `reason` values;
    - `security_rate_limits_total`;
    - `security_session_revocations_total`.
  - Spans `auth.request_otp`, `auth.verify_otp`, `auth.refresh_tokens` and `auth.logout`, with nested `firestore.*` and `redis.*` spans.
- **Not measured:**
  - no committed deployed-flow log;
  - no auth latency metric (no histogram exists anywhere);
  - no test that Redis being down really denies access end-to-end (unit-tested only);
  - the OTP reissue/lockout interaction above.

---

## M1.3 — Chats and membership

PRs #22, #24–#29 (2026-08-12 to 2026-09-20); #23 is the ADR-023 v1.4 amendment. Plan status: ✅ Done, gate met (v2.8).

### Problem

Without a chat lifecycle there is nothing to send messages *to*, and without membership there is nothing to authorize against. Each missing piece has a specific failure:
- **Two clients opening the same direct chat concurrently** would create two chat IDs. Each would get its own message sequence and history, while both users believe they are talking to each other. That failure can't be repaired after the fact.
- **A group cap enforced outside a transaction** can be overrun by concurrent adds.
- **Authorization based on the caller's claims** rather than the stored membership lets anyone mutate any chat.
- **M2.2 has nothing to read:** it needs a cheap, strongly consistent "is this sender a member?".

### ADR context

- **ADR-016 v1.2, §8 Authorization Decision Matrix**:
  > "All checks are performed by the Chat Mgmt Service with `ConsistentRead=true` on the caller's membership record"

  The same matrix records "Leave chat | ❌ (must transfer first)" for the owner. The DynamoDB term `ConsistentRead` maps to Firestore's strongly consistent point reads.
- **ADR-023 v1.4, Firestore model, `direct_chats`**:
  > "**It is a lookup, not only a sentinel.** It carries `chat_id`, which is what makes "which chat does this pair have?" a single strongly consistent `get()` on a deterministic path."

  And:
  > "**The sentinel is measured not to fire, and that is recorded rather than hidden.** In the same run, `tx.Create` on the sentinel refused **zero** racers across all thirty repetitions."
- **ADR-006 v1.2, §4.1 Create Chat**: an existing direct chat "Returns **200 OK** with the existing chat (not 409)".

### Origins

| Source | What it actually says | Where we agree or differ |
|---|---|---|
| Google Cloud, *Transaction serializability and isolation* — https://cloud.google.com/firestore/docs/transaction-data-contention | "With pessimist concurrency controls, transactions place locks on the documents they read. A transaction's lock on a document blocks other transactions, batched writes, and non-transactional writes from changing that document." Also: "The mobile and web SDKs behave independently of this setting as they always emulate optimistic concurrency." | **Agree.** The group cap reads `chats/{chat_id}` inside the transaction, which relies on exactly this documented lock. **Differ, knowingly:** we measured that a query matching nothing was *also* protected, which the page neither promises nor denies. ADR-023 v1.4 therefore does not rely on it. |

[Inference] The direct-pair sentinel follows the uniqueness-claim pattern ("insert a key whose existence is the lock"). I found no single primary source that names it, so none is cited.

### Doubts

- **From voice discussion, 2026-11-22: Chat Mgmt fits none of ADR-002's three planes. Which plane owns it, and what does that mean for metric naming?**
  - ADR-002's design sections are only "Connection Plane", "Durability Plane" and "Fanout Plane", and the document **never mentions Chat Mgmt** (verified by text search).
  - ADR-012 §1.3 requires `<plane>_<subsystem>_<metric>_<unit>` with prefixes `gateway_`, `durability_`, `fanout_` and `messaging_`.
  - Today's chat and auth metrics (`chat_created_total`, `auth_otp_requests_total`, `security_auth_failures_total`) fit none of them.
  - **[Doubt — mine]** Options: name a fourth "control" or "identity" plane in ADR-002; fold Chat Mgmt into Durability, since it owns entity state; or treat it as outside the planes, with an ADR-012 prefix of its own. Each implies a rename, or a documented exception, for 10 existing counters.
- **[Doubt — mine]** The sentinel is measured never to fire. Should the gate keep a *negative control*, a run that removes the protection and shows it fails, so a future Firestore change would be noticed?
- **[Doubt — mine]** Owner transfer is out of scope ("owner cannot leave"). What happens to a group whose owner deletes their account? No user-deletion flow exists yet.

### Experiments

- **`m1.3-direct-pair-{pessimistic,optimistic}.log`** (2026-08-10, experiment, not a gate):
  - the naive design produced **exactly one chat in 15/15 repetitions in both concurrency modes**;
  - the query-deleted control produced five chats every time;
  - the sentinel's `tx.Create` refused **zero** racers in 30.

  **Overturned premise:** ADR-023 v1.3's reason for the sentinel was measured false. The sentinel was kept on grounds that don't depend on the race (ADR-023 v1.4).
- **`m1.3-store.log`** (2026-08-11): the chat-store gate, 30 PASS lines, 0 failures. Concurrent direct creation gives exactly one chat, with the refusing mechanism recorded.
- **`m1.3-store-membership.log`** (2026-09-20): membership operations and the service-to-adapter seam, 58 PASS lines, 0 failures. Covers the group cap with the count still exact, direct-chat immutability, and the indefinite-mute sentinel surviving a live round trip.
- **`m1.3-flow.log`** (2026-09-20, against the **deployed pod** at commit `4ecf4c9`): 22 passing steps over REST, including "member left; the owner was refused".
- **Found by the measurement discipline itself:** the gate now has to *record which mechanism refused*. A gate that accepts any refusal would certify the sentinel while never exercising it (RTM-04 C7).

### Measurement

- **What shows it works:**
  - the four live logs above;
  - unit tests for authorization branches, validation and error mapping (190 unit tests on `main`, repo-wide);
  - counters `chat_created_total`, `direct_chat_dedup_total`, `chat_authz_denials_total` and `membership_mutation_total`;
  - spans `chat.*` with nested `firestore.chat.*` and `firestore.membership.*`.
- **Not measured:**
  - per-RPC latency, request rate and error rate (no server instrumentation; `grpc.NewServer()` is called bare);
  - the read cost of `GetChat` (one user read per member);
  - any load behaviour (no traffic generator beyond the flow script).

---

## `ListChats` membership fields (M1.3 open item 1)

**Status:** implemented on `feat/m1.3-listchats-membership`, with no PR yet and not merged. Local CI passes there (198 tests, lint clean, `buf breaking` clean). **The live gate has not run.** This section is written from the gate onward.

### Problem

ADR-006 §4.2's chat list shows each chat *as the caller sees it*: their role, when they joined, whether they muted it, and, for a direct chat, who the other person is. Without those fields:
- a client can't render a chat list without an extra `GetChat` per row;
- a direct chat has no name to display, since direct chats are unnamed;
- the mute state is invisible.

Today's `ListChats` also reads one chat document per round trip.

### ADR context

- **ADR-006 v1.2, §4.2 List Chats, notes:**
  > "For direct chats, `other_member` provides the other participant's info"

  And:
  > "Results ordered by `updated_at` descending (most recent first)"

  The second is not yet true (see Open items).
- **ADR-023 v1.4, `direct_chats` point 1, on `main`:**
  > "a query needs the pair key stored on the chat document, which is the disclosure problem below."

  The approved design stores the pair as a **field**, `participant_ids`. The branch's ADR-023 v1.5 adds a clarification: the "disclosure problem" is about a chat *ID* derived from its members, while a non-key field that is never queried, never leaves the service and repeats what `memberships` already holds discloses nothing new. That reasoning lands when the branch merges.

### Origins

| Source | What it actually says | Where we agree or differ |
|---|---|---|
| Google Cloud, *`projects.databases.documents.batchGet`* — https://cloud.google.com/firestore/docs/reference/rest/v1/projects.databases.documents/batchGet | "Gets multiple documents. Documents returned by this method are not guaranteed to be returned in the same order that they were requested." | **Agree, by construction.** The design keys results by document ID and re-walks the membership order, so it never depends on the API's order. |
| Go client, *`Client.GetAll`* — https://pkg.go.dev/cloud.google.com/go/firestore#Client.GetAll | "The DocumentSnapshots are returned in the order of the given DocumentRefs. The return value will always contain the same number of DocumentSnapshots as the number of DocumentRefs." A missing document is reported through `Exists()`, not as an error. | **Agree.** "A missing chat is skipped and logged" is implemented as filtering out `!Exists()`. The ordering guarantee belongs to the client library, not the wire API, which is why we don't rely on it. |

### Doubts

- **[Doubt — mine]** Neither document above states a maximum number of documents per batch get. A user can hold up to 500 chats (`MaxConcurrentChats`). Has a 500-document batch been exercised? No; the gate uses a small page.
- **[Doubt — mine]** `participant_ids` exists only on direct chats created after this change. That is safe in dev, which is destroyed every session. Would a persistent environment need a backfill, or is "drift is logged, no `other_member`" enough?
- **[Doubt — mine]** The approved design keeps the deprecated `chats` field populated beside `items`, which doubles the payload. When is it removed, and what tells us no client still reads it?

### Experiments (planned: the gate)

1. Create Firestore only (`scripts/chat.sh apply`; billable; about 6 minutes to tear down).
2. Run the store suite with `LOG_SUFFIX=-listchats`, which writes `m1.3-store-listchats.log` and doesn't overwrite the committed `m1.3-store.log`.
3. **The gate:**
   - **`TestServiceListChatsMixedPage`:** a group (caller is owner, muted) plus two direct chats, one opened by the caller and one by the peer, against the live database. Expect the caller's own membership on every item, the correct `other_member` with display name on both direct items, `muted_until` on the muted group, and persisted `participant_ids`.
   - **`TestStoreGetManySkipsMissingChats`:** a present and an absent chat ID in one batch. Expect only the present one, and no error.
4. Tear down (`scripts/chat.sh destroy`), then confirm Terraform state is empty and no Firestore database exists.

### Measurement (planned)

- **Once the gate runs:**
  - the evidence log above;
  - the port JSON-shape test, which checks §4.2's fields under the API's conventions;
  - the drift-guard test, which fails if `Chat` gains a field `ChatListItem` neither carries nor explicitly leaves out;
  - the unit tests proving one batched chat read, no per-chat reads, and one batched user read.
- **Read cost:** 2M + D document reads in three round trips, where M is the number of chats and D the number of direct chats. That's a stated design cost, not yet measured live.
- **Not measured:** latency of `ListChats` at 500 chats; actual Firestore read counts (only inferable from the code); the payload size with both `chats` and `items`.

---

## Doubts across Module 1

- **From voice discussion, 2026-11-22: telemetry reaching Cloud Trace and Managed Prometheus is evidenced only in PR #28's prose. What would a re-checkable proof look like?**
  - The PR describes querying the Cloud Trace API (an `auth.verify_otp` span tree) and Managed Prometheus (`auth_otp_requests_total`, exact count), but **no query output was committed**.
  - **[Doubt — mine]** A candidate proof: a script that runs the flow, then queries both APIs for that run's trace ID and the counter's delta, and writes the responses (project redacted) into `docs/artifacts/evidence/` like every other gate. A gate would then assert "the delta equals the requests sent".
  - **New finding while writing this dossier (verified 2026-11-22):** Google's *Managed OpenTelemetry for GKE* pages ([concepts](https://cloud.google.com/kubernetes-engine/docs/concepts/managed-otel-gke), [how-to](https://cloud.google.com/kubernetes-engine/docs/how-to/managed-otel-gke), both last updated 2026-11-20) document **both** an HTTP endpoint on `:4318` **and a gRPC endpoint on `:4317`**. PR #28 and the comments in `internal/observability/{tracer,metrics}.go` say "only documents an OTLP/HTTP endpoint — no gRPC port is published". Whether the documentation changed after September is unknown. **As of today the code comment is inaccurate**, although the HTTP choice still works.
- **From voice discussion, 2026-11-22: what is the unit being measured?**
  - Today's counters count **events**: an OTP request, a token mint, a membership change.
  - ADR-012's SLOs are written per **request**, as latency histograms.
  - Gates count **operations under concurrency**.
  - None counts per **user** or per **chat**, and ADR-012 §1.3 forbids `user_id`/`chat_id` labels on metrics because of cardinality.
  - **[Doubt — mine]** For Module 1 the natural SLO unit is the *RPC request*, split by method and status. That's what `otelgrpc` would produce. Per-user or per-chat questions belong in traces and logs, which ADR-012 already says.

---

## Lessons

1. **We wired the pipe before deciding what to put in it.** PR #28 connected OpenTelemetry export to Cloud Trace and Managed Prometheus, and that was verified live. But nobody had decided what Module 1 *should* measure:
   - there is no latency histogram;
   - there is no per-RPC traffic or error signal;
   - the metric names don't fit ADR-012's plane prefixes.

   Export without a measurement design produces data that answers no question.
2. **Gates must name the mechanism, not just the outcome.** "Exactly one chat" passed while the sentinel the design relied on never fired (zero of 30). Recording *which* refusal happened turned a passing gate into an actual measurement (RTM-04 C7).
3. **Measure premises before building on them.** Twice ("a query matching nothing locks nothing", on registration and on direct chats) the reason in the ADR was measured false. In the direct-chat case it was caught *before* the code was written, because the negative control ran first.
4. **Some defects only exist in a live deploy:** the ungrantable `secrets.list` permission, the consumed-OTP reissue block, and the missing `false` booleans. Unit tests could not have found them.
5. **A claim without an artifact decays.** The M1.2 flow gate and the PR #28 telemetry check are asserted in prose, with no log. PRs #22 and #24–#29 landed without plan updates, so the plan was stale until v2.8.

---

## Open items carried forward

| Item | Owner | Notes |
|---|---|---|
| `ListChats` membership fields: live gate, then PR | M1.3 open item 1 | Branch `feat/m1.3-listchats-membership`; Firestore-only gate |
| `ListChats` `last_message`, `pending_ack_count` | waits on M2.2 / `delivery_state` | Need the Postgres tables |
| `GetMessages` | M5.2 | Served through Ingest `ReadMessages` (ADR-006 v1.2 §5) |
| REST Idempotency-Key | M1.4 | ADR-006 v1.1 §10.4–§10.5 |
| `ListChats` pagination ordered by `updated_at` | unassigned | Today: no pagination, document-ID order |
| REST JSON conventions vs ADR-006 (envelope, camelCase, enum names, `Timestamp` shape, omitted unset optionals) | unassigned | Affects every endpoint |
| Committed `m1.2-flow.log`; an M1.1 gate log | unassigned | Seed doubt 2 |
| Re-checkable telemetry proof; correct the gRPC-endpoint comment | unassigned | Seed doubt 3 |
| Golden signals: ADR-012 naming decision (plane for Chat Mgmt), then `otelgrpc` | unassigned | Seed doubt 1 |
| OTP reissue vs NIST "SHALL NOT reset the failed authentication count" | unassigned | [Doubt — mine], needs a test |

---

## References

**Repository (internal):** [ADR-002](../adr/ADR-002.md), [ADR-006](../adr/ADR-006.md), [ADR-012](../adr/ADR-012.md), [ADR-013](../adr/ADR-013.md), [ADR-015](../adr/ADR-015.md), [ADR-016](../adr/ADR-016.md), [ADR-021](../adr/ADR-021.md), [ADR-023](../adr/ADR-023.md), [Execution plan](../EXECUTION_PLAN.md), [evidence logs](../artifacts/evidence/README.md), [RTM-04](../artifacts/RTM-04.md).

**External (each fetched 2026-11-22; some cloud.google.com links redirect to docs.cloud.google.com):**
- Google Cloud — Firestore TTL: https://cloud.google.com/firestore/docs/ttl
- Google Cloud — Firestore best practices: https://cloud.google.com/firestore/docs/best-practices
- Google Cloud — Firestore quotas and limits: https://cloud.google.com/firestore/docs/quotas
- Google Cloud — Transaction serializability and isolation: https://cloud.google.com/firestore/docs/transaction-data-contention
- Google Cloud — Firestore REST `batchGet`: https://cloud.google.com/firestore/docs/reference/rest/v1/projects.databases.documents/batchGet
- Google Cloud — Managed OpenTelemetry for GKE (concepts): https://cloud.google.com/kubernetes-engine/docs/concepts/managed-otel-gke
- Google Cloud — Managed OpenTelemetry for GKE (how-to): https://cloud.google.com/kubernetes-engine/docs/how-to/managed-otel-gke
- Go client `Client.GetAll`: https://pkg.go.dev/cloud.google.com/go/firestore#Client.GetAll
- HashiCorp — `google_firestore_database` docs source: https://github.com/hashicorp/terraform-provider-google/blob/main/website/docs/r/firestore_database.html.markdown
- NIST SP 800-63B Rev. 4: https://pages.nist.gov/800-63-4/sp800-63b.html
- RFC 9700, OAuth 2.0 Security Best Current Practice: https://www.rfc-editor.org/rfc/rfc9700
- RFC 7519, JSON Web Token: https://www.rfc-editor.org/rfc/rfc7519
- RFC 8725, JWT Best Current Practices: https://www.rfc-editor.org/rfc/rfc8725
- Google SRE Book, ch. 6, Monitoring Distributed Systems: https://sre.google/sre-book/monitoring-distributed-systems/
- OpenTelemetry semantic conventions, RPC metrics (`rpc.server.call.duration`): https://opentelemetry.io/docs/specs/semconv/rpc/rpc-metrics/

## Changelog

| Version | Date | Change |
|---|---|---|
| v1.0 | 2026-11-22 | First Feature Dossier. M1.1–M1.3 written retrospectively; `ListChats` membership fields written forward-looking from its live gate. Records two findings made while writing: the OTP reissue resets the per-OTP attempt counter (compare NIST 800-63B), and Google now documents a gRPC endpoint (`:4317`) the code comments say does not exist. |
