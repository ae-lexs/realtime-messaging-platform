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
	DynamoDBTimeout     = 5 * time.Second  // Max time for DynamoDB operations
	KafkaProduceTimeout = 10 * time.Second // Max time for Kafka produce
	RedisTimeout        = 2 * time.Second  // Max time for Redis operations
	GRPCCallTimeout     = 10 * time.Second // Max time for inter-service gRPC calls

	// Graceful shutdown (ADR-014 §4.1)
	GracefulShutdownTimeout = 30 * time.Second // Max time to drain connections on shutdown

	// Rate limiting (ADR-013 §4.1)
	OTPRequestRateLimitPerPhone = 3                // Max OTP requests per phone per window
	OTPRequestRateLimitPerIP    = 10               // Max OTP requests per IP per window
	OTPRateLimitWindow          = 15 * time.Minute // Rate limit window for OTP requests
	OTPValidityDuration         = 5 * time.Minute  // How long an OTP remains valid
	MaxOTPVerifyAttempts        = 5                // Max verification attempts before lockout
	OTPLockoutDuration          = 15 * time.Minute // Lockout duration after max attempts

	// Token configuration (ADR-015)
	AccessTokenLifetime  = 1 * time.Hour       // JWT access token validity
	RefreshTokenLifetime = 30 * 24 * time.Hour // Refresh token validity (30 days)
	MaxSessionsPerUser   = 5                   // Max concurrent sessions per user

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
