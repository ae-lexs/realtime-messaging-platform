package app_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

var testPepper = []byte("test-pepper-32-bytes-long-ok!!")

var testStart = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

// stubOTPStore implements app.OTPStore with function fields.
type stubOTPStore struct {
	createOTPFn         func(ctx context.Context, record app.OTPRecord) error
	getOTPFn            func(ctx context.Context, phoneHash string) (*app.OTPRecord, error)
	incrementAttemptsFn func(ctx context.Context, phoneHash string) error
}

func (s *stubOTPStore) CreateOTP(ctx context.Context, record app.OTPRecord) error {
	if s.createOTPFn != nil {
		return s.createOTPFn(ctx, record)
	}
	return nil
}

func (s *stubOTPStore) GetOTP(ctx context.Context, phoneHash string) (*app.OTPRecord, error) {
	if s.getOTPFn != nil {
		return s.getOTPFn(ctx, phoneHash)
	}
	return nil, domain.ErrNotFound
}

func (s *stubOTPStore) IncrementAttempts(ctx context.Context, phoneHash string) error {
	if s.incrementAttemptsFn != nil {
		return s.incrementAttemptsFn(ctx, phoneHash)
	}
	return nil
}

// stubUserStore implements app.UserStore with function fields.
type stubUserStore struct {
	getByIDFn     func(ctx context.Context, userID string) (*app.UserRecord, error)
	findByPhoneFn func(ctx context.Context, phone string) (*app.UserRecord, error)
}

func (s *stubUserStore) GetByID(ctx context.Context, userID string) (*app.UserRecord, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(ctx, userID)
	}
	return nil, domain.ErrNotFound
}

func (s *stubUserStore) FindByPhone(ctx context.Context, phone string) (*app.UserRecord, error) {
	if s.findByPhoneFn != nil {
		return s.findByPhoneFn(ctx, phone)
	}
	return nil, domain.ErrNotFound
}

// stubSessionStore implements app.SessionStore with function fields.
type stubSessionStore struct {
	createFn     func(ctx context.Context, session app.SessionRecord) error
	getByIDFn    func(ctx context.Context, sessionID string) (*app.SessionRecord, error)
	listByUserFn func(ctx context.Context, userID string) ([]app.SessionRecord, error)
	updateFn     func(ctx context.Context, sessionID string, update app.SessionUpdate) error
	deleteFn     func(ctx context.Context, sessionID string) error
}

func (s *stubSessionStore) Create(ctx context.Context, session app.SessionRecord) error {
	if s.createFn != nil {
		return s.createFn(ctx, session)
	}
	return nil
}

func (s *stubSessionStore) GetByID(ctx context.Context, sessionID string) (*app.SessionRecord, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(ctx, sessionID)
	}
	return nil, domain.ErrNotFound
}

func (s *stubSessionStore) ListByUser(ctx context.Context, userID string) ([]app.SessionRecord, error) {
	if s.listByUserFn != nil {
		return s.listByUserFn(ctx, userID)
	}
	return nil, nil
}

func (s *stubSessionStore) Update(ctx context.Context, sessionID string, update app.SessionUpdate) error {
	if s.updateFn != nil {
		return s.updateFn(ctx, sessionID, update)
	}
	return nil
}

func (s *stubSessionStore) Delete(ctx context.Context, sessionID string) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, sessionID)
	}
	return nil
}

// stubTransactor implements app.AuthTransactor with function fields.
type stubTransactor struct {
	verifyOTPAndCreateUserFn    func(ctx context.Context, params app.RegistrationParams) error
	verifyOTPAndCreateSessionFn func(ctx context.Context, params app.LoginParams) error
}

func (s *stubTransactor) VerifyOTPAndCreateUser(ctx context.Context, params app.RegistrationParams) error {
	if s.verifyOTPAndCreateUserFn != nil {
		return s.verifyOTPAndCreateUserFn(ctx, params)
	}
	return nil
}

func (s *stubTransactor) VerifyOTPAndCreateSession(ctx context.Context, params app.LoginParams) error {
	if s.verifyOTPAndCreateSessionFn != nil {
		return s.verifyOTPAndCreateSessionFn(ctx, params)
	}
	return nil
}

// stubRateLimiter implements app.RateLimiter with function fields.
type stubRateLimiter struct {
	checkAndIncrementFn func(ctx context.Context, key string, limit, windowSeconds int) (bool, error)
	checkLockoutFn      func(ctx context.Context, key string) (bool, error)
	setLockoutFn        func(ctx context.Context, key string, ttlSeconds int) error
}

func (s *stubRateLimiter) CheckAndIncrement(ctx context.Context, key string, limit, windowSeconds int) (bool, error) {
	if s.checkAndIncrementFn != nil {
		return s.checkAndIncrementFn(ctx, key, limit, windowSeconds)
	}
	return true, nil
}

func (s *stubRateLimiter) CheckLockout(ctx context.Context, key string) (bool, error) {
	if s.checkLockoutFn != nil {
		return s.checkLockoutFn(ctx, key)
	}
	return false, nil
}

func (s *stubRateLimiter) SetLockout(ctx context.Context, key string, ttlSeconds int) error {
	if s.setLockoutFn != nil {
		return s.setLockoutFn(ctx, key, ttlSeconds)
	}
	return nil
}

// stubRevocationStore implements app.RevocationStore with function fields.
type stubRevocationStore struct {
	revokeFn    func(ctx context.Context, jti string) error
	isRevokedFn func(ctx context.Context, jti string) (bool, error)
}

func (s *stubRevocationStore) Revoke(ctx context.Context, jti string) error {
	if s.revokeFn != nil {
		return s.revokeFn(ctx, jti)
	}
	return nil
}

func (s *stubRevocationStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if s.isRevokedFn != nil {
		return s.isRevokedFn(ctx, jti)
	}
	return false, nil
}

// stubSMSProvider implements auth.SMSProvider with a function field.
type stubSMSProvider struct {
	sendOTPFn func(ctx context.Context, phone, otp string) error
}

func (s *stubSMSProvider) SendOTP(ctx context.Context, phone, otp string) error {
	if s.sendOTPFn != nil {
		return s.sendOTPFn(ctx, phone, otp)
	}
	return nil
}

// testHarness holds all stubs and the constructed AuthService for a test.
type testHarness struct {
	svc             *app.AuthService
	clock           *domaintest.FakeClock
	otpStore        *stubOTPStore
	userStore       *stubUserStore
	sessionStore    *stubSessionStore
	transactor      *stubTransactor
	rateLimiter     *stubRateLimiter
	revocationStore *stubRevocationStore
	smsProvider     *stubSMSProvider
	minter          *auth.Minter
	validator       *auth.Validator
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyStore := auth.NewStaticKeyStore(key, "test-key-001")
	clock := domaintest.NewFakeClock(testStart)

	minter := auth.NewMinter(auth.MinterConfig{
		KeyStore:  keyStore,
		AccessTTL: domain.AccessTokenLifetime,
		Issuer:    "messaging-platform",
		Audience:  "messaging-api",
		Clock:     clock,
	})
	validator := auth.NewValidator(auth.ValidatorConfig{
		KeyStore: keyStore,
		Issuer:   "messaging-platform",
		Audience: "messaging-api",
		Clock:    clock,
	})

	h := &testHarness{
		clock:           clock,
		otpStore:        &stubOTPStore{},
		userStore:       &stubUserStore{},
		sessionStore:    &stubSessionStore{},
		transactor:      &stubTransactor{},
		rateLimiter:     &stubRateLimiter{},
		revocationStore: &stubRevocationStore{},
		smsProvider:     &stubSMSProvider{},
		minter:          minter,
		validator:       validator,
	}

	h.svc = app.NewAuthService(app.AuthServiceConfig{
		OTPStore:        h.otpStore,
		UserStore:       h.userStore,
		SessionStore:    h.sessionStore,
		Transactor:      h.transactor,
		RateLimiter:     h.rateLimiter,
		RevocationStore: h.revocationStore,
		SMSProvider:     h.smsProvider,
		Minter:          minter,
		Validator:       validator,
		Clock:           clock,
		Pepper:          testPepper,
		Logger:          slog.Default(),
	})

	return h
}

// sampleOTPRecord returns a valid pending OTP record for testing.
func sampleOTPRecord(phoneHash string, clock *domaintest.FakeClock) *app.OTPRecord {
	now := clock.Now().UTC()
	expiresAt := now.Add(domain.OTPValidityDuration)
	otp := "123456"
	mac := auth.ComputeOTPMAC(testPepper, otp, phoneHash, expiresAt.Format(time.RFC3339))
	return &app.OTPRecord{
		PhoneHash: phoneHash,
		OTPMAC:    mac,
		Status:    "pending",
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: expiresAt.Format(time.RFC3339),
		TTL:       expiresAt.Unix(),
	}
}

// sampleUserRecord returns a valid user record for testing.
func sampleUserRecord() *app.UserRecord {
	return &app.UserRecord{
		UserID:      "user-existing-001",
		PhoneNumber: "+15551234567",
		DisplayName: "",
		CreatedAt:   testStart.Add(-24 * time.Hour).Format(time.RFC3339),
		UpdatedAt:   testStart.Add(-24 * time.Hour).Format(time.RFC3339),
	}
}

// sampleSessionRecord returns a valid session record for testing.
func sampleSessionRecord(userID, sessionID, deviceID, refreshHash string, clock *domaintest.FakeClock) *app.SessionRecord {
	now := clock.Now().UTC()
	expiry := now.Add(domain.RefreshTokenLifetime)
	return &app.SessionRecord{
		SessionID:        sessionID,
		UserID:           userID,
		DeviceID:         deviceID,
		RefreshTokenHash: refreshHash,
		TokenGeneration:  1,
		CreatedAt:        now.Format(time.RFC3339),
		ExpiresAt:        expiry.Format(time.RFC3339),
		TTL:              expiry.Unix(),
	}
}
