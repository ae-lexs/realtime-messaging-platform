package domain

import "time"

// Normative limits from ADR-009 (Failure Handling).
// These are compiled defaults that can be overridden via configuration.
const (
	// Message limits
	MaxMessageSize           = 64 * 1024 // 64 KB max message body
	MaxClientMessageIDLength = 128       // Max length for client-provided message IDs

	// Chat limits
	MaxGroupSize          = 100 // Maximum members in a group chat
	MaxConcurrentChats    = 500 // Maximum chats a user can be a member of
	MaxDirectChatsPerPair = 1   // Exactly one direct chat per user pair

	// Connection limits (ADR-009 §3)
	MaxConnectionsPerUser     = 5  // Max concurrent WebSocket connections per user
	MaxConnectionsPerIP       = 20 // Max concurrent connections from a single IP
	ConnectionRateLimitWindow = 10 * time.Second
	ConnectionRateLimit       = 5 // Max new connections per user per window

	// Buffer limits (ADR-009 §2 Backpressure)
	OutboundBufferSize    = 256             // Messages buffered per connection before backpressure
	OutboundBufferTimeout = 5 * time.Second // Time to drain buffer before disconnect
	SlowConsumerThreshold = 100             // Buffer depth that triggers slow consumer warning

	// Heartbeat configuration (ADR-005 §6, ADR-009)
	HeartbeatInterval = 30 * time.Second // Server sends ping every 30s
	HeartbeatTimeout  = 30 * time.Second // Client must respond within 30s
	ConnectionTTL     = 60 * time.Second // Redis key TTL = 2x heartbeat interval

	// Timeout contracts (ADR-009 §1)
	KafkaProduceTimeout = 10 * time.Second // Max time for Kafka produce
	RedisTimeout        = 2 * time.Second  // Max time for Redis operations
	FirestoreTimeout    = 5 * time.Second  // Max time for a Firestore document read/write or query

	// FirestoreTxTimeout bounds a whole Firestore transaction, which is not
	// one operation and must not borrow FirestoreTimeout's budget.
	//
	// A read-write transaction that the store aborts is re-run by the client
	// library with exponential backoff, up to its own attempt limit, and
	// internal/firestore restarts it again if the store rejects one of those
	// retries (ADR-023 v1.4). The budget therefore has to cover several
	// attempts plus the backoff between them, where FirestoreTimeout covers
	// exactly one round trip.
	//
	// 5 s was measured to be too small: the M1.3 store gate hit
	// DeadlineExceeded inside a two-read transaction under ordinary
	// contention, on a path that had passed minutes earlier. 20 s is four
	// times the single-operation budget, leaving room for the retries while
	// staying well inside Firestore's own 270 s transaction limit.
	FirestoreTxTimeout   = 20 * time.Second
	SecretManagerTimeout = 10 * time.Second // Max time for a Secret Manager access; only on the startup and refresh paths, never per-request
	GRPCCallTimeout      = 10 * time.Second // Max time for inter-service gRPC calls

	// Graceful shutdown budget (ADR-014 §4.1)
	GracefulShutdownTimeout = 30 * time.Second // Total shutdown budget
	ShutdownDrainDelay      = 3 * time.Second  // Pause after failing health checks for LB propagation
	ShutdownHTTPTimeout     = 20 * time.Second // HTTP server drain for in-flight requests
	ShutdownOTELTimeout     = 5 * time.Second  // OTEL tracer + metrics flush

	// Rate limiting (ADR-013 §4.1)
	OTPRequestRateLimitPerPhone = 3                // Max OTP requests per phone per window
	OTPRequestRateLimitPerIP    = 10               // Max OTP requests per IP per window
	OTPRateLimitWindow          = 15 * time.Minute // Rate limit window for OTP requests
	OTPValidityDuration         = 5 * time.Minute  // How long an OTP remains valid
	MaxOTPVerifyAttempts        = 5                // Max verification attempts before lockout
	OTPLockoutDuration          = 15 * time.Minute // Lockout duration after max attempts

	// OTP record status (ADR-015 §1.3 state machine). A record stays
	// "verified" rather than being deleted so a replayed OTP is refused as
	// already-used instead of read as never-issued.
	OTPStatusPending  = "pending"
	OTPStatusVerified = "verified"

	// Token configuration (ADR-015)
	JWTKeyRefreshInterval = 5 * time.Minute     // How often the in-memory JWT key set is reloaded (ADR-015 §3.2)
	AccessTokenLifetime   = 1 * time.Hour       // JWT access token validity
	RefreshTokenLifetime  = 30 * 24 * time.Hour // Refresh token validity (30 days)
	MaxSessionsPerUser    = 5                   // Max concurrent sessions per user

	// Membership cache (ADR-003 §3.2)
	MembershipCacheTTL = 5 * time.Minute // Redis cache TTL for chat memberships

	// Pagination defaults
	DefaultPageSize = 50
	MaxPageSize     = 100
)

// ContentType represents supported message content types.
type ContentType string

const (
	ContentTypeText ContentType = "text"
)

// IsValidContentType checks if a content type is supported.
func IsValidContentType(ct ContentType) bool {
	return ct == ContentTypeText
}

// ChatType represents the type of chat.
type ChatType string

const (
	ChatTypeDirect ChatType = "direct"
	ChatTypeGroup  ChatType = "group"
)

// IsValidChatType checks if a chat type is valid.
func IsValidChatType(ct ChatType) bool {
	return ct == ChatTypeDirect || ct == ChatTypeGroup
}

// Role is a member's role in a chat (ADR-006 §8, ADR-016).
type Role string

const (
	// RoleOwner is the group creator. Exactly one per group, for the group's
	// whole life: ownership transfer is out of scope for the MVP, so the owner
	// can neither leave nor be removed nor have their role changed
	// (ADR-016 §4.3). Direct chats have no owner.
	RoleOwner Role = "owner"

	// RoleAdmin can add and remove members and rename the chat, but only
	// against members — an admin cannot act on another admin or the owner.
	RoleAdmin Role = "admin"

	// RoleMember can read, send and mute. Both participants of a direct chat
	// hold this role (ADR-006 §4.1).
	RoleMember Role = "member"
)

// IsValidRole reports whether r is a role this system assigns.
func IsValidRole(r Role) bool {
	return r == RoleOwner || r == RoleAdmin || r == RoleMember
}

// IsAssignableRole reports whether r may be granted through the API.
//
// RoleOwner is deliberately excluded: it is conferred once, by creating a
// group, and ADR-006 §4.7 forbids assigning it because ownership transfer has
// no implementation. A transfer would have to move the role off one member and
// onto another in a single transaction — two independent writes would leave a
// window with zero owners or two (ADR-016 §4.3).
func IsAssignableRole(r Role) bool {
	return r == RoleAdmin || r == RoleMember
}
