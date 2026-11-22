# Chats and members: direct chats, groups, roles, mute

Part of [Module 1](README.md). Explanation: sections 1–3. Reference: sections 4–5. How-to: section 6.

## 1. What it does

A signed-in person can open a **direct chat** with one other person, or create a **group** of up to 100 people. Two people always share **one** direct chat: opening it again, from either side, returns the same conversation. A group has one **owner** (its creator), any number of **admins**, and **members**. The owner and admins manage who is in the group and what it is called, and the owner decides who is an admin. Anyone in a chat can **mute** it for a while or indefinitely, which affects only them. Every action is checked against the person's **stored** membership, never against what the request claims.

## 2. How it flows

```mermaid
sequenceDiagram
    actor U as Person
    participant C as App
    participant M as Chat Mgmt
    participant R as Redis
    participant F as Firestore
    U->>C: open a chat with B
    C->>M: POST /v1/chats (direct, B)<br/>Bearer access token
    M->>M: verify the token
    M->>R: is this token revoked? (fails closed)
    M->>F: transaction: get direct_chats/{min}__{max}
    alt the pair already has a chat
        M-->>C: 200 OK, the existing chat
    else first time
        M->>F: create chat + direct_chats + 2 memberships, one commit
        M-->>C: 201 Created
    end
    U->>C: mute the group
    C->>M: POST /v1/chats/{id}/mute
    M->>F: get memberships/{chat_id}__{user_id}<br/>(strongly consistent point read)
    M->>F: update muted_until
    M-->>C: muted until …
```

## 3. The rules

| Rule | How it's enforced |
|---|---|
| Only signed-in, non-revoked callers reach any chat action | Every RPC passes `AuthenticatedChatServer`: token check, then the Redis revocation list, failing closed |
| One direct chat per pair, whichever side opens it | `direct_chats/{min(a,b)}__{max(a,b)}` is read and created inside the creating transaction; a repeat returns the existing chat (200, not 409) |
| You can't open a direct chat with yourself | Refused before any write |
| A direct chat's two members never change | Add, remove and leave are refused on direct chats |
| A group holds at most 100 members | The count on the chat document is read and updated inside the same transaction as the add |
| Only members can see or act on a chat | Each action does a strongly consistent point read of `memberships/{chat_id}__{user_id}`; a non-member gets "not a member" |
| Owner and admins rename a group; the owner and admins add people | An admin can add only plain members; only the owner can add an admin |
| An admin can remove only plain members | The target's stored role is read before the removal |
| Only the owner changes roles, and nobody can be made owner | `owner` is not an assignable role |
| The owner can't leave or be removed | Refused in the removal transaction; owner transfer does not exist yet |
| Removing yourself is "leave", not "remove" | Self-removal through the remove endpoint is refused |
| Mute lasts 1–8,760 hours, or indefinitely | Out-of-range durations are refused; indefinite is stored as `9999-01-01`, because `null` means "not muted" |

## 4. Where the data lives

```mermaid
erDiagram
    CHATS ||--|{ MEMBERSHIPS : "has members"
    USERS ||--o{ MEMBERSHIPS : "belongs to"
    DIRECT_CHATS |o--|| CHATS : "points to (direct only)"
    CHATS {
        string id PK "UUID"
        string chat_type "direct or group"
        string name "empty for direct"
        string created_by FK
        int member_count "the group cap reads this"
        array participant_ids "direct only: the sorted pair"
    }
    MEMBERSHIPS {
        string id PK "chat_id + __ + user_id, 74 bytes"
        string chat_id FK
        string user_id FK
        string role "owner, admin or member"
        timestamp joined_at
        timestamp muted_until "null when not muted"
    }
    DIRECT_CHATS {
        string id PK "min(user) + __ + max(user)"
        string chat_id FK
    }
```

One membership document answers three questions: "is X in Y?" is a point read on its ID, and "who is in Y?" and "what is X in?" are equality queries on `chat_id` and `user_id`. Inspect them with [`scripts/inspect.sh`](../../scripts/inspect.sh); today's output is from 2026-11-22, using the ListChats gate's users (A `8bb12951…`, B `49d25070…`, C `0d102d9c…`):

```bash
scripts/inspect.sh pair 49d25070-2951-46ec-be78-669159815ac9 8bb12951-a86b-4a6f-9a04-e30e654fd906   # order doesn't matter
scripts/inspect.sh doc chats/290614a1-6932-4c3c-8e47-9fbe85c671f8
scripts/inspect.sh where memberships chat_id 6cd6c32d-810b-4246-bd83-cae37679110f
```
```
{"id":"direct_chats/49d25070-…__8bb12951-…","updateTime":"…15:59:06.448209Z","fields":{"chat_id":"290614a1-6932-4c3c-8e47-9fbe85c671f8","created_at":"…15:59:06.382593Z"}}
{"id":"chats/290614a1-…","updateTime":"…15:59:06.448209Z","fields":{"created_by":"8bb12951-…","member_count":2,"name":"","participant_ids":["49d25070-…","8bb12951-…"],"chat_type":"direct",…}}
{"id":"memberships/6cd6c32d-…__0d102d9c-…","updateTime":"…15:59:05.878702Z","fields":{"role":"member","joined_at":"…15:59:05.808460Z","muted_until":null,…}}
{"id":"memberships/6cd6c32d-…__49d25070-…","updateTime":"…15:59:05.878702Z","fields":{"role":"member","joined_at":"…15:59:05.808460Z","muted_until":null,…}}
{"id":"memberships/6cd6c32d-…__8bb12951-…","updateTime":"…15:59:06.211316Z","fields":{"role":"owner","joined_at":"…15:59:05.808460Z","muted_until":"2026-11-23T15:59:06.186728Z",…}}
```

The direct chat and its pair document were written in one commit (same `updateTime`); so were the group and its members. The owner's membership is newer because muting rewrote it. The derived membership ID is 74 bytes: two 36-byte UUIDs and `__` ([`m1.1-store.log`](../artifacts/evidence/m1.1-store.log)).

## 5. How we know it works

| Signal | Query | Today |
|---|---|---|
| Chats created | `sum by (chat_type) (chat_created_total)` | `direct` 2, `group` 1 |
| Direct-chat dedup | `sum by (result) (direct_chat_dedup_total)` | `created` 2; no `existing` (summed without the label it misreads as "2 dedups") |
| Membership changes | `sum by (change_type) (membership_mutation_total)` | `muted` 1 |
| Refusals | `sum by (operation, reason) (chat_authz_denials_total)` | not queried on 2026-11-22 |
| Trace | `root:chat.create` | `2dc313a5…`, 192.7 ms: the transaction 146.2 ms (begin 73.5, read 28.7, commit 43.7), then `list_by_chat` 24.5 ms, then **two `firestore.user.get` in sequence** (7 ms, 14.9 ms) |
| Trace | `span:firestore.membership.get` | `a564c0b5…`: `chat.mute` 45.9 ms, a 17.7 ms point read, then a 28.1 ms update |
| Log | `jsonPayload.msg="chat.create"` | 3 lines (`direct` ×2, `group`), all `"existing":false`, each with the `trace_id` of its trace |

**Evidence logs:**
- [`m1.3-store.log`](../artifacts/evidence/m1.3-store.log): the chat-store gate. Concurrent direct creation yields exactly one chat, and the log records which mechanism refused each loser.
- [`m1.3-store-membership.log`](../artifacts/evidence/m1.3-store-membership.log): the group cap with the count still exact, direct-chat immutability, and the indefinite-mute sentinel (`9999-01-01`) surviving a live round trip.
- [`m1.3-direct-pair-pessimistic.log`](../artifacts/evidence/m1.3-direct-pair-pessimistic.log) and [`-optimistic.log`](../artifacts/evidence/m1.3-direct-pair-optimistic.log): the race experiment. The sentinel refused **zero** racers in 30, so it is kept as a lookup rather than as the lock.
- [`m1.3-flow.log`](../artifacts/evidence/m1.3-flow.log): 22 steps against the deployed pod (2026-09-20), including "member left; the owner was refused".

**Not measured:** per-RPC latency, rate and errors (the gRPC server is not instrumented); load behaviour.

## 6. How to operate and debug it

**A direct chat seems duplicated.**
1. `scripts/inspect.sh pair <user-a> <user-b>` gives the one chat the pair maps to.
2. `scripts/inspect.sh where memberships user_id <user-a>` lists every chat A is in. Read each candidate with `doc chats/<id>`; a true duplicate is a second `direct` chat whose `participant_ids` is the same pair. None has ever been observed.
3. `sum by (result) (direct_chat_dedup_total)`: `existing` counts the creates that correctly returned the old chat.

**A user gets "forbidden" or "not a member".**
1. `scripts/inspect.sh doc memberships/<chat-id>__<user-id>`. A 404 means they are not a member; otherwise `role` decides (see the rules).
2. `sum by (operation, reason) (chat_authz_denials_total)` shows which action is being refused, and how often.

**A member can't be added.** Read the chat: `member_count` 100 means the group is full. A direct chat never accepts members.

**Mute seems not to apply.** Read the membership: `muted_until` must be in the future. `9999-01-01` means "indefinite"; `null` means not muted.

**Find the log line for a slow create.** Take the `trace_id` from the trace and run `scripts/inspect.sh logs "$NS jsonPayload.trace_id=\"<id>\""`.

**Code:** [`internal/chatmgmt/app/chat_service.go`](../../internal/chatmgmt/app/chat_service.go) (every action and its authorization), [`internal/firestore/chat_tx.go`](../../internal/firestore/chat_tx.go) (`CreateDirect`, `CreateGroup`, the in-transaction cap, removal), [`internal/chatmgmt/port/authenticated_chat.go`](../../internal/chatmgmt/port/authenticated_chat.go) (token and revocation check).

## Open items

| Item | Notes |
|---|---|
| **`CreateChat` reads members one at a time** | The response is built with one sequential `firestore.user.get` per member (trace `2dc313a5…`); `GetChat` has the same cost. A batched read, as the chat list uses, would make it one round trip |
| Owner transfer | Out of scope, which is why the owner can't leave |
| Sentinel negative control | Parked: the sentinel never fires (0 of 30), so a control that removes it would show nothing |
| Idempotency-Key for REST creates | Planned as M1.4 (ADR-006 v1.1 §10.4–§10.5) |

## Decisions and sources

- **Governing ADRs:** [ADR-016](../adr/ADR-016.md) §8, the authorization matrix, where "All checks are performed by the Chat Mgmt Service with `ConsistentRead=true` on the caller's membership record"; [ADR-023](../adr/ADR-023.md) v1.4–v1.5, the Firestore model (`direct_chats` is "a lookup, not only a sentinel"; the measured result that it never fires is recorded there); [ADR-006](../adr/ADR-006.md) §4.1, where an existing direct chat returns 200. Plan: [`EXECUTION_PLAN.md`](../EXECUTION_PLAN.md), M1.1 and M1.3.
- **[Firestore transaction serializability and isolation](https://cloud.google.com/firestore/docs/transaction-data-contention):** agree; the group cap relies on the documented pessimistic lock on the chat document it reads. **Differ, knowingly:** we measured that a query matching nothing was also protected, which the page neither promises nor denies, so the design does not rely on it.
- **[Firestore best practices](https://cloud.google.com/firestore/docs/best-practices):** agree; IDs are random UUIDs, not monotonic, so the composite IDs start with a random prefix.
- **[Firestore quotas and limits](https://cloud.google.com/firestore/docs/quotas):** agree; 74 bytes is far below the 1,500-byte ID limit, and `__` is used only as an inner separator, never in the reserved `__.*__` form.
- [Inference] The pair document follows the "uniqueness claim" pattern (a key whose existence is the lock); no single primary source names it, so none is cited.

**History:** Firestore tier PR #14 (2026-07-26); chats and membership PRs #22, #24–#29 (2026-08-12 to 2026-09-20), with #23 the ADR-023 v1.4 amendment; live logs 2026-08-10 to 2026-09-20 and the PR #32 live session 2026-11-22. Previously part of the Module 1 Feature Dossier v1.0–v1.4.
