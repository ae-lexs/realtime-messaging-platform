package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

var tracer = otel.Tracer("chatmgmt/app")

var (
	otpRequestsTotal        metric.Int64Counter
	tokenMintedTotal        metric.Int64Counter
	sessionCreatedTotal     metric.Int64Counter
	authFailuresTotal       metric.Int64Counter
	rateLimitsTotal         metric.Int64Counter
	sessionRevocationsTotal metric.Int64Counter
)

func init() {
	m := otel.Meter("chatmgmt/app")

	otpRequestsTotal, _ = m.Int64Counter("auth_otp_requests_total",
		metric.WithDescription("Total OTP requests"))
	tokenMintedTotal, _ = m.Int64Counter("auth_token_minted_total",
		metric.WithDescription("Total tokens minted"))
	sessionCreatedTotal, _ = m.Int64Counter("auth_session_created_total",
		metric.WithDescription("Total sessions created"))
	authFailuresTotal, _ = m.Int64Counter("security_auth_failures_total",
		metric.WithDescription("Total authentication failures"))
	rateLimitsTotal, _ = m.Int64Counter("security_rate_limits_total",
		metric.WithDescription("Total rate limit hits"))
	sessionRevocationsTotal, _ = m.Int64Counter("security_session_revocations_total",
		metric.WithDescription("Total session revocations"))
}

// The records below are the service's own vocabulary; adapters map them onto
// whatever the store holds. Two properties are worth stating because earlier
// revisions had neither (ADR-023):
//
//   - Timestamps are time.Time, not formatted strings, so no flow re-parses one
//     or carries an unreachable parse-error branch. The conversion to whatever
//     the store wants happens once, in the adapter.
//   - Expiry is a single field. A Firestore TTL policy reads the timestamp
//     directly, so nothing carries a second copy of the same instant in another
//     encoding, and no two fields can disagree about when something expires.

// OTPRecord represents an OTP request stored in the OTP collection.
// Structurally mirrors the adapter record; the wiring layer converts between them.
type OTPRecord struct {
	PhoneHash    string
	OTPMAC       string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Status       string
	AttemptCount int
}

// IsExpired reports whether the OTP is past its expiry at now.
//
// Existence is not validity: the store's TTL collects an expired OTP only
// within ~24 hours, so a lapsed record stays readable for most of a day
// (ADR-023 v1.2). This is the gate.
func (r OTPRecord) IsExpired(now time.Time) bool {
	return !now.Before(r.ExpiresAt)
}

// UserRecord represents a user stored in the users collection.
type UserRecord struct {
	UserID      string
	PhoneNumber string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SessionRecord represents an active session stored in the sessions collection.
type SessionRecord struct {
	SessionID        string
	UserID           string
	DeviceID         string
	RefreshTokenHash string
	PrevTokenHash    string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	TokenGeneration  int64
}

// IsExpired reports whether the session is past its expiry at now — the
// ADR-023 application-enforced invariant. TTL is garbage collection; this is
// the correctness gate, and it must be consulted on every session read.
func (r SessionRecord) IsExpired(now time.Time) bool {
	return !now.Before(r.ExpiresAt)
}

// SessionUpdate holds the mutable fields for a session rotation.
type SessionUpdate struct {
	RefreshTokenHash string
	PrevTokenHash    string
	ExpiresAt        time.Time
	TokenGeneration  int64
}

// RegistrationParams holds the inputs for a transactional new-user registration.
type RegistrationParams struct {
	PhoneHash    string
	OTPExpiresAt time.Time
	OTPMAC       string

	UserID      string
	PhoneNumber string
	Now         time.Time

	SessionID        string
	DeviceID         string
	RefreshTokenHash string
	SessionExpiresAt time.Time
}

// LoginParams holds the inputs for a transactional existing-user login.
type LoginParams struct {
	PhoneHash    string
	OTPExpiresAt time.Time
	OTPMAC       string

	SessionID        string
	UserID           string
	DeviceID         string
	RefreshTokenHash string
	CreatedAt        time.Time
	SessionExpiresAt time.Time
}

// OTPStore persists and retrieves OTP requests.
type OTPStore interface {
	CreateOTP(ctx context.Context, record OTPRecord) error
	GetOTP(ctx context.Context, phoneHash string) (*OTPRecord, error)
	IncrementAttempts(ctx context.Context, phoneHash string) error
}

// UserStore persists and retrieves user records.
type UserStore interface {
	GetByID(ctx context.Context, userID string) (*UserRecord, error)
	FindByPhone(ctx context.Context, phone string) (*UserRecord, error)
}

// SessionStore persists and retrieves session records.
type SessionStore interface {
	Create(ctx context.Context, session SessionRecord) error
	GetByID(ctx context.Context, sessionID string) (*SessionRecord, error)
	ListByUser(ctx context.Context, userID string) ([]SessionRecord, error)
	Update(ctx context.Context, sessionID string, update SessionUpdate) error
	Delete(ctx context.Context, sessionID string) error
}

// AuthTransactor executes the multi-document transactions the auth flows
// require to be atomic (ADR-015 §5).
type AuthTransactor interface {
	VerifyOTPAndCreateUser(ctx context.Context, params RegistrationParams) error
	VerifyOTPAndCreateSession(ctx context.Context, params LoginParams) error
}

// RateLimiter checks and enforces rate limits.
type RateLimiter interface {
	CheckAndIncrement(ctx context.Context, key string, limit, windowSeconds int) (bool, error)
	CheckLockout(ctx context.Context, key string) (bool, error)
	SetLockout(ctx context.Context, key string, ttlSeconds int) error
}

// RevocationStore tracks revoked JTIs for token invalidation.
type RevocationStore interface {
	Revoke(ctx context.Context, jti string) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

// RequestOTPResult is returned by RequestOTP on success.
type RequestOTPResult struct {
	ExpiresAt         time.Time
	RetryAfterSeconds int
}

// VerifyOTPResult is returned by VerifyOTP on success.
type VerifyOTPResult struct {
	User              UserRecord
	SessionID         string
	AccessToken       string
	RefreshToken      string
	IsNewUser         bool
	AccessTokenExpiry time.Time
}

// RefreshResult is returned by RefreshTokens on success.
type RefreshResult struct {
	AccessToken       string
	RefreshToken      string
	AccessTokenExpiry time.Time
}

// AuthServiceConfig holds the dependencies for AuthService.
type AuthServiceConfig struct {
	OTPStore        OTPStore
	UserStore       UserStore
	SessionStore    SessionStore
	Transactor      AuthTransactor
	RateLimiter     RateLimiter
	RevocationStore RevocationStore
	SMSProvider     auth.SMSProvider
	Minter          *auth.Minter
	Validator       *auth.Validator
	Clock           domain.Clock
	Pepper          []byte
	Logger          *slog.Logger
}

// AuthService orchestrates the four auth flows: Request OTP, Verify OTP,
// Refresh Tokens, and Logout (ADR-015).
type AuthService struct {
	otpStore        OTPStore
	userStore       UserStore
	sessionStore    SessionStore
	transactor      AuthTransactor
	rateLimiter     RateLimiter
	revocationStore RevocationStore
	smsProvider     auth.SMSProvider
	minter          *auth.Minter
	validator       *auth.Validator
	clock           domain.Clock
	pepper          []byte
	logger          *slog.Logger
	bgWG            sync.WaitGroup // owns background goroutines (SMS sends)
}

// NewAuthService creates a new AuthService with the given dependencies.
func NewAuthService(cfg AuthServiceConfig) *AuthService {
	return &AuthService{
		otpStore:        cfg.OTPStore,
		userStore:       cfg.UserStore,
		sessionStore:    cfg.SessionStore,
		transactor:      cfg.Transactor,
		rateLimiter:     cfg.RateLimiter,
		revocationStore: cfg.RevocationStore,
		smsProvider:     cfg.SMSProvider,
		minter:          cfg.Minter,
		validator:       cfg.Validator,
		clock:           cfg.Clock,
		pepper:          cfg.Pepper,
		logger:          cfg.Logger,
	}
}

// Wait blocks until all background goroutines owned by this service complete.
// The caller (wiring layer) must invoke this during graceful shutdown to
// satisfy the goroutine ownership contract.
func (s *AuthService) Wait() {
	s.bgWG.Wait()
}
