# PR-1 TBD Decisions: Key Rotation & Auth TTL Values

- **Status**: Approved
- **Date**: 2026-02-10
- **Related ADRs**: ADR-013 (Security & Abuse Controls), ADR-015 (Authentication & OTP Implementation)
- **Execution Plan Reference**: TBD-PR1-1, TBD-PR1-2

---

## Purpose

This document resolves the two TBD notes identified in the Execution Plan for PR-1 (Authentication — OTP, Tokens & Sessions). Each decision pins concrete values for parameters that ADR-013 and ADR-015 left as ranges or recommendations. These are not ADR-level architectural decisions — they are implementation specifications that select specific values within the bounds already established by the ADRs.

> **Normative Policy Layer**: This document defines **mandatory implementation values**. All auth-related code in this repository must use these exact values. Deviations require an ADR amendment or explicit justification in the PR description with reviewer approval.

---

## TBD-PR1-1: Key Rotation Workflow & Dual-Key Validation

### Problem Statement

ADR-013 §2.2 specifies RS256 with 2048-bit keys and 90-day rotation with a 7-day overlap. ADR-015 §3.2 specifies a 5-minute validator cache TTL and unknown `kid` handling with a 30-second cooldown. ADR-015 §7.2 details a 4-phase rotation procedure.

The Execution Plan's TBD-PR1-1 asks to pin: (1) the rotation window duration, (2) the public key cache TTL in validators, and (3) the operational procedure for key rotation. The note recommended `rotation_window = 2× max token lifetime = 2 hours`, but ADR-015 already specifies a 7-day overlap — this document validates which value is correct and why.

### Decision

#### Signing Algorithm & Key Parameters

| Parameter | Value | Source |
|-----------|-------|--------|
| Algorithm | RS256 (RSASSA-PKCS1-v1_5 + SHA-256) | ADR-013 §2.2 |
| Key size | RSA 2048-bit | ADR-013 §2.2; NIST SP 800-131A Rev 2 (approved through 2030) |
| Key ID format | UUIDv4 | ADR-015 §7.1 |

**Validation**: RSA-2048 with RS256 is the most widely deployed JWT algorithm. NIST SP 800-131A Rev 2 (March 2024) classifies RSA-2048 as "acceptable" through 2030. Auth0, Okta, and Google Identity Platform all default to RS256. Migration to RS384/RS512 or ES256 is deferred — the provider interface in `internal/auth/` supports algorithm changes without schema migration since the algorithm is encoded in the JWT header, not persisted.

#### Rotation Timeline

| Phase | Timing | Action | State |
|-------|--------|--------|-------|
| Phase 1: Generate | Day 0 | Generate Key B; store private key in Secrets Manager, public key in SSM; update `current-key-id` to Key B | Both Key A and Key B public keys in SSM; only Key B private key is active for signing |
| Phase 2: Overlap | Day 0–7 | Chat Mgmt signs new tokens with Key B; all validators accept both Key A and Key B | Dual-key validation active |
| Phase 3: Deactivate | Day 7 | Remove Key A private key from Secrets Manager | Key A can no longer sign; existing Key A tokens still validate |
| Phase 4: Remove | Day 90 | Remove Key A public key from SSM | Key A tokens rejected (all expired long ago) |

**Why 7-day overlap, not 2 hours**: The Execution Plan note recommended `2× max access token TTL = 2 hours` for the rotation window. This is sufficient for **token expiry** (all Key A tokens expire within 60 minutes), but insufficient for **operational safety**:

- A 2-hour window requires completing the rotation procedure within 2 hours or risk service disruption. If a deployment fails during Phase 1, rollback must happen before the window closes.
- A 7-day window provides a comfortable buffer for: failed deployments, service cache refresh cycles (5 minutes each), ECS task restarts, and human operator availability.
- The overlap window's security cost is negligible — both keys are equally strong RSA-2048 keys. The only risk is if Key A's private key is compromised during the overlap window, but Key A's private key is removed on Day 7 regardless.

The ratio `overlap_window / max_access_token_TTL = 168:1` (7 days / 60 minutes) provides a large safety margin. This follows the pattern used by Cloudflare Access (7-day rotation overlap) and Auth0 (configurable overlap, default 7 days).

**Source**: ADR-013 §2.2, ADR-015 §7.2, Cloudflare Access rotation documentation, Auth0 signing key rotation guide.

#### Validator Key Cache

| Parameter | Value | Source |
|-----------|-------|--------|
| Public key cache TTL | 300 seconds (5 minutes) | ADR-015 §3.2 |
| Cache refresh mechanism | Background goroutine with ticker | ADR-015 §3.2 |
| Unknown `kid` handling | Single immediate SSM refresh + 30-second cooldown, then reject | ADR-015 §3.2 |
| SSM parameter path (current key ID) | `/messaging/jwt/current-key-id` | ADR-015 §3.2 |
| SSM parameter path (public keys) | `/messaging/jwt/public-keys/{KEY_ID}` | ADR-015 §3.2 |
| Secrets Manager path (private key) | `jwt/signing-key/{KEY_ID}` | ADR-015 §3.2 |

**Why 5 minutes, not shorter**: A 5-minute cache TTL means that after Phase 1 of rotation, all validators will pick up the new public key within 5 minutes. With the unknown `kid` fallback (immediate SSM refresh), this drops to near-zero for tokens signed with the new key — the first JWT with the new `kid` triggers an immediate refresh. The 5-minute TTL is a background refresh that keeps the cache warm, not the primary mechanism for new key discovery.

**Why 30-second cooldown on unknown `kid` refresh**: Without a cooldown, an attacker sending JWTs with random fabricated `kid` values could cause an SSM call storm. The 30-second cooldown limits SSM calls to 2/minute per service instance regardless of attack volume. This follows the Connect2ID Nimbus JOSE+JWT library pattern.

**Source**: ADR-015 §3.2, Nimbus JOSE+JWT `RemoteJWKSet` default refresh rate limiter (5 minutes), Connect2ID documentation on unknown key handling.

#### Operational Rotation Checklist

This checklist is normative for all key rotation operations:

```
PRE-ROTATION CHECKLIST:
  [ ] Verify current key ID via SSM: /messaging/jwt/current-key-id
  [ ] Verify all services healthy (ECS task health checks passing)
  [ ] Confirm no ongoing deployments or incidents

PHASE 1 — GENERATE (Day 0):
  [ ] Generate RSA-2048 key pair locally
  [ ] Store private key in Secrets Manager: jwt/signing-key/{NEW_KEY_ID}
  [ ] Store public key in SSM: /messaging/jwt/public-keys/{NEW_KEY_ID}
  [ ] Update current key ID in SSM: /messaging/jwt/current-key-id → NEW_KEY_ID
  [ ] Shred local key files
  [ ] WAIT 5 minutes (validator cache refresh cycle)
  [ ] Verify: request new access token → JWT header kid = NEW_KEY_ID
  [ ] Verify: old tokens still validate (dual-key active)

PHASE 2 — OVERLAP (Day 0–7):
  [ ] Monitor: auth_token_validation_errors metric (should be zero)
  [ ] Monitor: auth_key_cache_refresh_total metric (normal cadence)

PHASE 3 — DEACTIVATE (Day 7):
  [ ] Delete old private key from Secrets Manager: jwt/signing-key/{OLD_KEY_ID}
  [ ] WAIT 60 minutes (max access token TTL)
  [ ] Verify: no tokens signed with OLD_KEY_ID are in active use
      (auth_token_validated_by_kid metric for OLD_KEY_ID should be zero)

PHASE 4 — REMOVE (Day 90):
  [ ] Delete old public key from SSM: /messaging/jwt/public-keys/{OLD_KEY_ID}
  [ ] Verify: SSM contains exactly one public key under /messaging/jwt/public-keys/

ROLLBACK (if Phase 1 fails):
  [ ] Restore /messaging/jwt/current-key-id to OLD_KEY_ID
  [ ] Delete new key from Secrets Manager and SSM
  [ ] Verify: services still signing with old key
```

---

## TBD-PR1-2: TTL Values for Auth Tables

### Problem Statement

The Execution Plan's TBD-PR1-2 asks to pin explicit TTL values for: `otp_requests` DynamoDB TTL, `sessions` DynamoDB TTL, and revoked JTI Redis TTL. It recommends `otp_requests TTL = 10 minutes (2× OTP validity)`, `sessions TTL = 30 days`, and `revoked JTI TTL = 1 hour`. ADR-015 already specifies most of these values — this document validates them against industry standards and adds normative implementation rules.

### Decision

#### Access Token Lifetime

| Parameter | Value | Source |
|-----------|-------|--------|
| Access token lifetime | **60 minutes** | ADR-015 §3.3 (`m.accessTTL` default); ADR-013 §2.2 (range: 15–60 min) |

**Why 60 minutes (top of ADR-013's range)**: A shorter TTL (15 minutes) would reduce the revocation gap but increase refresh frequency by 4×, amplifying DynamoDB read/write volume on the `sessions` table and increasing client complexity (more frequent background refreshes). For a messaging application where connections persist for hours:

- NIST SP 800-63B §7.2 recommends re-authentication timeouts proportional to the assurance level. At AAL1 (single-factor OTP), 60 minutes is within norms.
- Auth0's default access token TTL is 86400 seconds (24 hours); our 60 minutes is significantly more conservative.
- The revocation check on every request (`GET revoked_jti:{jti}`) provides near-real-time revocation regardless of token TTL.

60 minutes balances security (bounded exposure) against operational cost (refresh frequency) and user experience (fewer re-authentication prompts during active use).

**Source**: NIST SP 800-63B §7.2, Auth0 token best practices, ADR-015 §3.3.

#### OTP Lifecycle TTLs

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| OTP validity window | **5 minutes** | ADR-013 §2.2; NIST SP 800-63B §5.1.4.1 (max 10 minutes for OTP) |
| OTP record DynamoDB TTL | **`created_at + 1 hour`** | ADR-015 §1.1 (`ttl` attribute) |
| OTP max attempts | **5** | ADR-013 §2.2 |

**Why OTP record TTL = 1 hour, not 10 minutes (2× validity)**:

The Execution Plan recommended `10 minutes (2× OTP validity)`. ADR-015 §1.1 specifies `expires_at + 1 hour cleanup buffer`. The 1-hour value is correct for two reasons:

1. **DynamoDB TTL is not real-time**: DynamoDB's TTL process deletes expired items within 48 hours, not immediately. Setting a TTL of 10 minutes would leave the record visible for an indeterminate period anyway. The application must always filter by `expires_at` — the DynamoDB TTL is a garbage collection mechanism, not a security control.

2. **Audit trail**: The 1-hour buffer preserves OTP records briefly after expiry for debugging and forensics. If a user reports "I never received my OTP" within the first hour, the record is still queryable to verify whether it was created and what status it reached.

**Normative rule — DynamoDB TTL filtering**: Application code **must** check `expires_at > now` on every read from `otp_requests`. Never rely solely on DynamoDB TTL for security-sensitive expiry. This is defense-in-depth: DynamoDB TTL is eventual garbage collection, not a security boundary.

```go
// CORRECT — application-level expiry check
record, err := db.GetItem(ctx, "otp_requests", phoneHash)
if err != nil { return err }
if record.ExpiresAt.Before(clock.Now()) {
    return domain.ErrInvalidOTP // Treat as expired even if TTL hasn't deleted it
}

// WRONG — relying on DynamoDB TTL for security
record, err := db.GetItem(ctx, "otp_requests", phoneHash)
if err != nil { return err }
// If record exists, it must be valid... WRONG: TTL deletion is eventual!
```

**Source**: NIST SP 800-63B §5.1.4.1, AWS DynamoDB TTL documentation ("typically deletes within 48 hours"), ADR-015 §1.1.

#### Session Lifecycle TTLs

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Session absolute lifetime | **30 days** | ADR-015 §5; ADR-013 §3.2 ("Forced re-auth: After 30 days regardless of activity") |
| Session DynamoDB TTL | **`expires_at + 24 hours`** | Audit buffer; DynamoDB TTL eventual deletion lag |
| Refresh token lifetime | **30 days** (= session lifetime) | ADR-015 §4.1; refresh token is tied to session |
| Session idle timeout | **None (MVP)** | See rationale below |
| `prev_token_hash` retention | **= session lifetime (30 days)** | Naturally cleaned by session DynamoDB TTL |
| Max concurrent sessions | **5 per user** | ADR-013 §3.2; ADR-015 §6.1 |

**Why 30 days for session lifetime**: NIST SP 800-63B §7.2 permits session lifetimes up to 30 days at AAL1 (single-factor authentication). For a messaging application:

- WhatsApp sessions persist indefinitely (until manual logout).
- Signal sessions persist for 30 days without re-authentication.
- Telegram sessions persist for 6 months.
- Our 30-day lifetime is conservative relative to industry practice and matches NIST AAL1 maximums.

**Why no idle timeout for MVP**: NIST SP 800-63B §7.2 requires idle timeouts at AAL2+ but not at AAL1. For a messaging application, idle timeouts cause poor UX — a user who hasn't opened the app for a few hours would need to re-authenticate via OTP. Since the application's primary use case is persistent background connectivity (WebSocket), an idle timeout would trigger constantly. This can be revisited post-MVP if the security profile changes.

**Why `expires_at + 24 hours` for session DynamoDB TTL**: The 24-hour buffer serves two purposes:

1. **Refresh token reuse detection**: If a session expires and its DynamoDB record is immediately deleted, the reuse detection mechanism (checking `prev_token_hash`) loses its data. The 24-hour buffer ensures that replay attempts against a recently-expired session still trigger the reuse detection path and log the security event, rather than returning a generic "session not found" error.

2. **Audit trail**: Session records are queryable for 24 hours after expiry for forensics and debugging.

**Why `prev_token_hash` retention = session lifetime**: The `prev_token_hash` attribute is part of the session record (ADR-015 Appendix C). It persists as long as the session record exists and is naturally cleaned when the session's DynamoDB TTL fires. No separate TTL management is needed. This follows RFC 9700 §4.14 (OAuth 2.0 Security Best Current Practice) which recommends maintaining refresh token chain state for the lifetime of the token family.

**Source**: NIST SP 800-63B §7.2, RFC 9700 §4.14, ADR-015 §5, ADR-013 §3.2.

#### Revocation TTLs

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Revoked JTI Redis TTL | **3600 seconds (fixed)** | ADR-015 §6.2; = max access token lifetime |
| Redis key pattern | `revoked_jti:{jti}` | ADR-015 §6.2 (single mechanism) |
| Redis eviction policy (revocation keyspace) | **`noeviction`** | See rationale below |

**Why fixed 3600s, not dynamic `exp - now()`**: The Execution Plan recommended TTL = access token max lifetime (1 hour), which matches ADR-015 §6.2. The question is whether to use a fixed TTL or compute `token.exp - now()` per revocation:

- **Fixed 3600s** (chosen): Simpler implementation; every revocation entry lives for exactly 1 hour. This is slightly wasteful for tokens that expire soon, but the memory cost is negligible (~50 bytes per key × max concurrent revocations).
- **Dynamic `exp - now()`**: Computes exact remaining lifetime per token. Saves a few bytes of Redis memory but adds clock-dependent logic and a branch for admin-initiated revocations where the access token may not be available (e.g., "revoke all sessions for user" — the admin doesn't have the user's access token `exp` claim).

Fixed TTL is chosen because it is simpler, handles all revocation paths uniformly, and the memory savings of dynamic TTL are immaterial.

**Redis eviction policy — `noeviction` for revocation keyspace**: If Redis reaches its memory limit and uses an eviction policy like `allkeys-lru`, revocation entries could be evicted before their TTL expires. Eviction of a revoked JTI entry is a **security vulnerability** — the revoked token would pass validation.

**Normative rule**: The Redis instance (or logical database) used for JTI revocation **must** be configured with `maxmemory-policy noeviction`. If memory is exhausted, new `SET` commands will return an error, which the application handles as a fail-closed condition (deny the revocation request, alert operators). This is preferable to silently evicting existing revocation entries.

For MVP with a single Redis instance, this means the entire instance uses `noeviction`. If rate limit keys and revocation keys share the same instance, memory sizing must account for the sum of both workloads. A future enhancement could separate these into distinct Redis logical databases or clusters.

**Source**: ADR-015 §6.2, Redis `maxmemory-policy` documentation, OWASP JWT Security Cheat Sheet (revocation list integrity).

#### Refresh Token Rotation Grace Period

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Refresh token rotation grace period | **0 seconds (strict rotation)** | Simpler security model |

**Why strict rotation (no grace period)**: Some implementations (e.g., Auth0) offer a configurable grace period during which the old refresh token remains valid after rotation. This accommodates clients that may retry a refresh request due to network issues and accidentally use the old token.

For MVP, strict rotation is chosen:

- **Simpler security model**: The reuse detection mechanism (ADR-015 §4.2) triggers session revocation on replay of `prev_token_hash`. A grace period would require distinguishing "legitimate retry within grace" from "malicious replay" — adding time-based logic to a security-critical path.
- **Client responsibility**: The client must serialize refresh calls. If a refresh response is lost (network failure after server processes rotation), the client's old refresh token hits `prev_token_hash` and the session is revoked. The client re-authenticates via OTP. This is an acceptable UX trade-off for MVP — the scenario is rare (network failure at the exact moment of refresh response), and OTP re-authentication is fast.
- **Auth0 defaults to 0s grace period** in strict rotation mode, validating this as an industry-accepted default.

**Source**: Auth0 refresh token rotation documentation, RFC 9700 §4.14.

---

## Summary of Decisions

| TBD | Parameter | Value | Key Rationale |
|-----|-----------|-------|---------------|
| **TBD-PR1-1** | Algorithm | RS256 / RSA-2048 | NIST-approved through 2030; industry default |
| **TBD-PR1-1** | Rotation frequency | 90 days | ADR-013 §2.2 |
| **TBD-PR1-1** | Signing overlap window | 7 days | Operational safety margin (168:1 ratio to token TTL) |
| **TBD-PR1-1** | Public key retention (after private key deactivation) | Until Day 90 | Conservative; covers any straggler tokens |
| **TBD-PR1-1** | Validator cache TTL | 300 seconds (5 minutes) | Balance between freshness and SSM call volume |
| **TBD-PR1-1** | Unknown `kid` handling | Single SSM refresh + 30s cooldown | Handles rotation edge case; cooldown prevents abuse |
| **TBD-PR1-1** | **Normative**: 4-phase rotation checklist | See §TBD-PR1-1 | Operational procedure with explicit timing gates |
| **TBD-PR1-2** | Access token lifetime | 60 minutes | Top of ADR-013 range; balanced for messaging UX |
| **TBD-PR1-2** | OTP validity | 5 minutes | ADR-013 §2.2; NIST max 10 min |
| **TBD-PR1-2** | OTP DynamoDB TTL | `created_at + 1 hour` | GC buffer; app must filter by `expires_at` |
| **TBD-PR1-2** | Session absolute lifetime | 30 days | NIST AAL1 max; industry norm for messaging |
| **TBD-PR1-2** | Session DynamoDB TTL | `expires_at + 24 hours` | Audit buffer + reuse detection window |
| **TBD-PR1-2** | Refresh token lifetime | 30 days (= session) | Tied to session lifecycle |
| **TBD-PR1-2** | Session idle timeout | None (MVP) | AAL1 doesn't require; messaging UX |
| **TBD-PR1-2** | Revoked JTI Redis TTL | 3600 seconds (fixed) | = max access token lifetime; uniform for all paths |
| **TBD-PR1-2** | `prev_token_hash` retention | = session lifetime | Naturally cleaned by session DynamoDB TTL |
| **TBD-PR1-2** | Refresh rotation grace period | 0 seconds (strict) | Simpler security model; client serializes refreshes |
| **TBD-PR1-2** | **Normative**: DynamoDB TTL filtering | Always filter `expires_at > now` | DynamoDB TTL is GC, not a security boundary |
| **TBD-PR1-2** | **Normative**: Redis eviction policy | `noeviction` for revocation keyspace | Eviction of revoked JTI = security vulnerability |
| **TBD-PR1-2** | **Normative**: Revoked JTI TTL fixed | 3600s, not dynamic `exp - now()` | Uniform handling; admin revocations lack token `exp` |

---

## Validation Checklist

### Key Rotation (TBD-PR1-1)
- [ ] JWT signed with RS256 / RSA-2048
- [ ] `kid` header present in all minted JWTs
- [ ] Validator cache refreshes public keys every 300 seconds
- [ ] Unknown `kid` triggers single SSM refresh with 30-second cooldown
- [ ] Tokens signed with old key validate during 7-day overlap window
- [ ] Tokens signed with old key rejected after public key removed from SSM
- [ ] Rotation checklist executed successfully in local environment

### Auth TTLs (TBD-PR1-2)
- [ ] Access token `exp` claim set to `iat + 3600` (60 minutes)
- [ ] OTP record `expires_at` set to `created_at + 5 minutes`
- [ ] OTP record `ttl` (DynamoDB) set to `created_at + 1 hour` (Unix epoch)
- [ ] Application code filters `expires_at > now` on OTP reads (not relying on DynamoDB TTL)
- [ ] Session `expires_at` set to `created_at + 30 days`
- [ ] Session `ttl` (DynamoDB) set to `expires_at + 24 hours` (Unix epoch)
- [ ] Refresh token bound to session lifetime (30 days)
- [ ] No idle timeout enforced on sessions
- [ ] `revoked_jti:{jti}` Redis key set with `EX 3600` (fixed TTL)
- [ ] Redis `maxmemory-policy` set to `noeviction`
- [ ] Refresh token rotation is strict (no grace period)
- [ ] Replay of `prev_token_hash` triggers session revocation + security event log

---

## References

### Standards & RFCs
- [NIST SP 800-131A Rev 2 — Transitioning the Use of Cryptographic Algorithms and Key Lengths](https://csrc.nist.gov/publications/detail/sp/800-131a/rev-2/final) — RSA-2048 approved through 2030
- [NIST SP 800-63B — Digital Identity Guidelines: Authentication and Lifecycle Management](https://pages.nist.gov/800-63-4/sp800-63b.html) — OTP validity (§5.1.4.1), session lifetime (§7.2), idle timeout (§7.2)
- [RFC 9700 — OAuth 2.0 Security Best Current Practice](https://datatracker.ietf.org/doc/html/rfc9700) — Refresh token rotation (§4.14), token chain state retention
- [OWASP JWT Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/JSON_Web_Token_for_Java_Cheat_Sheet.html) — Revocation list integrity, algorithm selection

### Provider Documentation
- [Auth0 — Signing Key Rotation](https://auth0.com/docs/get-started/tenant-settings/signing-keys/rotate-signing-keys) — 7-day overlap pattern, grace period defaults
- [Auth0 — Refresh Token Rotation](https://auth0.com/docs/secure/tokens/refresh-tokens/refresh-token-rotation) — Strict rotation, reuse detection
- [Cloudflare Access — Key Rotation](https://developers.cloudflare.com/cloudflare-one/identity/authorization-cookie/) — 7-day overlap pattern

### AWS Documentation
- [DynamoDB TTL — How It Works](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/TTL.html) — Eventual deletion within 48 hours
- [Redis maxmemory-policy](https://redis.io/docs/latest/develop/reference/eviction/) — `noeviction` policy behavior

### Library Defaults
- [Nimbus JOSE+JWT — RemoteJWKSet](https://connect2id.com/products/nimbus-jose-jwt) — 5-minute default cache TTL, rate-limited refresh on unknown keys

### Project ADRs
- ADR-013 §2.2 — OTP security controls, JWT requirements, key rotation frequency
- ADR-013 §3.2 — Session security, forced re-auth, concurrent session limit
- ADR-015 §1.1 — `otp_requests` table schema, TTL attribute
- ADR-015 §3.2 — Key access model, cache TTL, unknown `kid` handling
- ADR-015 §3.3 — Access token minting, `accessTTL` default
- ADR-015 §4.2 — Refresh token rotation with reuse detection
- ADR-015 §5 — Session absolute lifetime
- ADR-015 §6.2 — Revoked JTI Redis key pattern, TTL
- ADR-015 §7.2 — 4-phase rotation procedure
