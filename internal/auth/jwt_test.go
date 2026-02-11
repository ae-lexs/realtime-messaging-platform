package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
)

func newTestMinterAndValidator(t *testing.T) (*auth.Minter, *auth.Validator, *auth.StaticKeyStore, *domaintest.FakeClock) {
	t.Helper()
	key := generateTestKey(t)
	keyID := "test-key-001"
	start := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	clock := domaintest.NewFakeClock(start)
	keyStore := auth.NewStaticKeyStore(key, keyID)

	minter := auth.NewMinter(auth.MinterConfig{
		KeyStore:  keyStore,
		AccessTTL: 60 * time.Minute,
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

	return minter, validator, keyStore, clock
}

func TestValidateAccessToken(t *testing.T) {
	minter, validator, keyStore, clock := newTestMinterAndValidator(t)
	start := clock.Now()

	t.Run("valid token succeeds", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		claims, err := validator.ValidateAccessToken(result.Token)
		require.NoError(t, err)
		assert.Equal(t, "user_123", claims.Subject)
		assert.Equal(t, "sess_456", claims.SessionID)
		assert.Equal(t, "messaging", claims.Scope)
		assert.Equal(t, result.JTI, claims.ID)
	})

	t.Run("expired token fails", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		clock.Advance(2 * time.Hour)
		_, err = validator.ValidateAccessToken(result.Token)
		require.Error(t, err)
		assert.True(t, errors.Is(err, auth.ErrTokenExpired))
		clock.Set(start)
	})

	t.Run("token valid at TTL minus one second", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		clock.Advance(60*time.Minute - time.Second)
		claims, err := validator.ValidateAccessToken(result.Token)
		require.NoError(t, err)
		assert.Equal(t, "user_123", claims.Subject)
		clock.Set(start)
	})

	t.Run("token expired at TTL plus one second", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		clock.Advance(60*time.Minute + time.Second)
		_, err = validator.ValidateAccessToken(result.Token)
		require.Error(t, err)
		assert.True(t, errors.Is(err, auth.ErrTokenExpired))
		clock.Set(start)
	})

	t.Run("wrong issuer fails", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		wrongIssuer := auth.NewValidator(auth.ValidatorConfig{
			KeyStore: keyStore,
			Issuer:   "wrong-issuer",
			Audience: "messaging-api",
			Clock:    clock,
		})

		_, err = wrongIssuer.ValidateAccessToken(result.Token)
		assert.Error(t, err)
	})

	t.Run("wrong audience fails", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		wrongAud := auth.NewValidator(auth.ValidatorConfig{
			KeyStore: keyStore,
			Issuer:   "messaging-platform",
			Audience: "wrong-audience",
			Clock:    clock,
		})

		_, err = wrongAud.ValidateAccessToken(result.Token)
		assert.Error(t, err)
	})

	t.Run("unknown kid fails", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		otherKey := generateTestKey(t)
		otherStore := auth.NewStaticKeyStore(otherKey, "other-key")
		wrongKidValidator := auth.NewValidator(auth.ValidatorConfig{
			KeyStore: otherStore,
			Issuer:   "messaging-platform",
			Audience: "messaging-api",
			Clock:    clock,
		})

		_, err = wrongKidValidator.ValidateAccessToken(result.Token)
		assert.Error(t, err)
	})

	t.Run("tampered token fails", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		tampered := result.Token[:len(result.Token)-5] + "XXXXX"
		_, err = validator.ValidateAccessToken(tampered)
		assert.Error(t, err)
	})

	t.Run("token missing sid claim is rejected", func(t *testing.T) {
		clock.Set(start)
		key := generateTestKey(t)
		kidVal := "no-sid-key"
		ks := auth.NewStaticKeyStore(key, kidVal)
		now := clock.Now()

		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"sub":   "user_123",
			"iss":   "messaging-platform",
			"aud":   "messaging-api",
			"iat":   now.Unix(),
			"exp":   now.Add(time.Hour).Unix(),
			"jti":   "test-jti",
			"scope": "messaging",
			// no "sid"
		})
		token.Header["kid"] = kidVal
		signed, err := token.SignedString(key)
		require.NoError(t, err)

		v := auth.NewValidator(auth.ValidatorConfig{
			KeyStore: ks,
			Issuer:   "messaging-platform",
			Audience: "messaging-api",
			Clock:    clock,
		})
		_, err = v.ValidateAccessToken(signed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sid")
	})

	t.Run("non-RSA signing method is rejected", func(t *testing.T) {
		clock.Set(start)
		hmacToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user_123",
			"iss": "messaging-platform",
			"aud": "messaging-api",
			"iat": clock.Now().Unix(),
			"exp": clock.Now().Add(time.Hour).Unix(),
			"jti": "test-jti",
			"sid": "sess_456",
		})
		hmacToken.Header["kid"] = "test-key-001"
		signed, err := hmacToken.SignedString([]byte("hmac-secret"))
		require.NoError(t, err)

		_, err = validator.ValidateAccessToken(signed)
		assert.Error(t, err)
	})
}

func TestValidateIgnoreExpiry(t *testing.T) {
	minter, validator, keyStore, clock := newTestMinterAndValidator(t)
	start := clock.Now()

	t.Run("expired token succeeds with ignore-expiry", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		clock.Advance(2 * time.Hour)

		// Normal validation fails
		_, err = validator.ValidateAccessToken(result.Token)
		require.Error(t, err)

		// Ignore-expiry succeeds
		claims, err := validator.ValidateIgnoreExpiry(result.Token)
		require.NoError(t, err)
		assert.Equal(t, "user_123", claims.Subject)
		assert.Equal(t, "sess_456", claims.SessionID)
		clock.Set(start)
	})

	t.Run("tampered expired token still fails", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		clock.Advance(2 * time.Hour)
		tampered := result.Token[:len(result.Token)-5] + "XXXXX"
		_, err = validator.ValidateIgnoreExpiry(tampered)
		assert.Error(t, err)
		clock.Set(start)
	})

	t.Run("wrong issuer fails even when ignoring expiry", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		clock.Advance(2 * time.Hour)
		wrongIssuer := auth.NewValidator(auth.ValidatorConfig{
			KeyStore: keyStore,
			Issuer:   "wrong-issuer",
			Audience: "messaging-api",
			Clock:    clock,
		})

		_, err = wrongIssuer.ValidateIgnoreExpiry(result.Token)
		assert.Error(t, err)
		clock.Set(start)
	})

	t.Run("wrong audience fails even when ignoring expiry", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		clock.Advance(2 * time.Hour)
		wrongAud := auth.NewValidator(auth.ValidatorConfig{
			KeyStore: keyStore,
			Issuer:   "messaging-platform",
			Audience: "wrong-audience",
			Clock:    clock,
		})

		_, err = wrongAud.ValidateIgnoreExpiry(result.Token)
		assert.Error(t, err)
		clock.Set(start)
	})

	t.Run("non-expired token also succeeds", func(t *testing.T) {
		clock.Set(start)
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		claims, err := validator.ValidateIgnoreExpiry(result.Token)
		require.NoError(t, err)
		assert.Equal(t, "user_123", claims.Subject)
	})
}
