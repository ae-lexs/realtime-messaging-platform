# MVP Definition — Realtime Messaging Platform

## Project Intent

This project is **not** a production chat application.

It is a **Distributed Systems Lab** designed to demonstrate senior/staff-level decision making in the design and implementation of a real-time messaging system. The primary goal is to explore **correctness, ordering, delivery semantics, scalability, and failure modes** under realistic constraints, while keeping the scope intentionally narrow.

The system is developed **incrementally in stages**, where each stage introduces a bounded set of capabilities and is accompanied by explicit **Architecture Decision Records (ADRs)**.  
Every non-trivial design choice is documented, justified, and treated as part of the system's contract.

---

## MVP Scope

### In Scope
- User authentication via phone number (OTP-based identity bootstrap)
- 1:1 and group (1:N) chat conversations
- Sending and receiving text messages (UTF-8, emojis supported)
- Offline message delivery via store-and-forward (catch-up on reconnect)
- Real-time delivery over WebSockets
- Persistent message history

### Out of Scope (for MVP)
- Push notifications
- Media messages (images, files, audio)
- Message deletion or editing
- Blocking users
- Mobile applications (web client or test clients only)
- Read receipts beyond basic delivery acknowledgment

---

## Architectural Philosophy

The MVP is explicitly designed around **distributed systems fundamentals**, not UI completeness:

- **Correctness before features**
- **Explicit invariants over implicit behavior**
- **Failure-aware design**
- **Idempotency, ordering, and replayability**
- **Clear separation of concerns (planes)**

The system favors **Availability and Partition Tolerance** for real-time communication paths, while enforcing **strong correctness guarantees** in the persistence layer.

---

## Consistency Model

Clients can expect the following consistency guarantees:

- **Read-your-own-writes**: A sender always sees their own message immediately after a successful send acknowledgment from the durability plane.
- **Per-chat total order**: All participants observe messages within a chat in the same order (by `sequence`).
- **Eventual visibility**: Other participants see messages with eventual consistency — delivery depends on connectivity and fanout latency, but ordering is preserved.
- **No cross-chat ordering**: Messages across different chats have no ordering relationship.

---

## MVP Architectural Decisions (Authoritative)

The following **ten decisions define the MVP contract**.  
They are non-negotiable for Stage 1 and are documented via ADRs.

---

### 1. Ordering & Delivery Semantics

**Decision**
- Messages are totally ordered **per chat** using a server-assigned monotonic `sequence`.
- Transport is **at-least-once**, persistence is **effectively-once** via idempotent writes (duplicate writes with the same `client_message_id` are collapsed, ensuring effectively-once semantics from the client’s perspective).
- Clients must provide a `client_message_id` for retry safety.
- Reconnect uses `last_acked_sequence` for deterministic catch-up.

**Rationale**
Global ordering is unnecessary and expensive. Per-chat ordering is sufficient, scalable, and testable.

---

### 2. Plane Separation

**Decision**
The system is split into three planes:
- **Connection Plane**: WebSocket gateway (low latency, AP-oriented)
- **Durability Plane**: Message ingest + persistence (correctness-first)
- **Fanout Plane**: Delivery to online users + offline sync trigger

**Rationale**
Prevents accidental coupling of ephemeral state with durable state.

---

### 3. Source of Truth

**Decision**
- **Database** is the system of record for messages, memberships, and delivery state.
- **Kafka** is the durable event log for fanout and integration.
- **Redis** is ephemeral only (presence, connection routing, short-lived metadata).

**Rationale**
Ephemeral systems must never be authoritative.

---

### 4. Message Sequencing Strategy

**Decision**
- Message `sequence` is allocated by the **Durability Plane** at ingest time.
- Sequencing is per-chat and persisted atomically with the message write.
- Implementation uses **DynamoDB atomic counters** to allocate sequences with low-latency conditional writes.

**Rationale**
Provides low-latency send acknowledgments and clear ordering semantics. DynamoDB is chosen to gain hands-on experience with its consistency model and atomic operations in a distributed context.

Hot-partition mitigation for large or highly active chats is deferred to later stages and explicitly out of scope for MVP.

---

### 5. Identity, Sessions, and Devices

**Decision**
- Phone number + OTP bootstraps identity.
- Server issues JWT access tokens.
- Clients generate a stable `device_id`.
- WebSocket connections are authenticated and bound to identity + device.

**Rationale**
Separates identity from connections and prepares for multi-device support.

---

### 6. Delivery Acknowledgments

**Decision**
- "Delivered" indicates **receipt by the client application, not user read acknowledgment**.
- The system stores `(user_id, chat_id, last_acked_sequence)`.
- Per-message per-recipient delivery state is explicitly **out of scope** for MVP.

**Rationale**
Avoids unbounded state growth in group chats.

---

### 7. Offline Message Handling

**Decision**
- All messages are persisted regardless of recipient connectivity.
- Offline users receive messages via **sync on reconnect**, not background pushes.
- No retry loops to dead sockets.

**Rationale**
Offline delivery is a data problem, not a socket problem.

---

### 8. API & Protocol Split

**Decision**
- **REST APIs** for chat management and message history.
- **WebSocket protocol** for real-time messaging, acknowledgments, and sync.
- Core message types (illustrative, full protocol defined in ADR): `send_message`, `message`, `ack`, `sync_request`.

**Rationale**
Separates consistency paths from latency paths.

---

### 9. Data Model & Indexing

**Decision**
- No embedded collections for participants or messages.
- Core tables:
  - `chats`
  - `chat_memberships`
  - `messages` (partitioned by `chat_id`, ordered by `sequence`)
  - `delivery_state`
  - `idempotency_keys`

**Rationale**
Scales linearly and supports deterministic replay.

---

### 10. Failure Handling & Backpressure

**Decision**
- WebSocket gateways enforce per-connection outbound limits.
- Slow or unresponsive clients are disconnected.
- Fanout failures rely on reconnect sync, not infinite retries.

**Rationale**
Protects system health under load and partial failures.

---

## Failure Scenarios (Acceptance Criteria)

The following failure scenarios are explicitly in scope for the MVP. The system must handle these correctly, and they serve as acceptance criteria for validating distributed systems behavior:

| Scenario | Expected Behavior |
|----------|-------------------|
| **Client reconnects with stale sequence** | Client sends `sync_request` with `last_acked_sequence`; server replays all messages with `sequence > last_acked_sequence` in order. |
| **Gateway crashes mid-fanout** | Messages already persisted remain durable. On reconnect, client syncs missed messages. No data loss. |
| **Duplicate message submission (retry)** | Server detects duplicate via `client_message_id`, returns success with original `sequence`. No duplicate message stored. |
| **Client disconnects before receiving ACK** | Client retries on reconnect. Idempotency ensures no duplicate persistence. Client receives correct `sequence` on retry. |
| **Slow client triggers backpressure** | Gateway disconnects client after outbound buffer threshold exceeded. Client reconnects and syncs missed messages. |

These scenarios validate correctness under retries, reconnects, and partial failures — the core goals of this distributed systems lab.

---

## Incremental Development Model

The project evolves in **stages**:

- Each stage introduces a bounded capability.
- Each stage includes one or more **Architecture Decision Records (ADRs)**.
- ADRs are treated as **authoritative design contracts**.
- Later stages may extend, but never silently violate, earlier decisions.

---

## What This Project Demonstrates

- Senior-level distributed systems thinking
- Explicit trade-offs and invariants
- Correctness under retries, reconnects, and partitions
- Realistic architectural patterns used in production messaging systems
- Engineering judgment over framework usage

This repository is intended to be read, reviewed, and reasoned about — not just run.
