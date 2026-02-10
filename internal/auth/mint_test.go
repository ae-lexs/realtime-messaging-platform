package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
)

// FakeClock is a deterministic, advanceable clock for tests.
// Follows the pattern from DD_TIME_AND_CLOCKS.md: "Time is a dependency;
// inject it like any other." Use Advance/Set to control time progression
// instead of creating new clock instances.
type FakeClock struct {
	mu      sync.Mutex
	current time.Time
}

func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{current: t}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = t
}

func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func TestMintAccessToken(t *testing.T) {
	key := generateTestKey(t)
	keyID := "test-key-001"
	start := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	minter := auth.NewMinter(auth.MinterConfig{
		KeyStore:  auth.NewStaticKeyStore(key, keyID),
		AccessTTL: 60 * time.Minute,
		Issuer:    "messaging-platform",
		Audience:  "messaging-api",
		Clock:     clock,
	})

	t.Run("produces valid signed JWT with expected claims", func(t *testing.T) {
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)
		assert.NotEmpty(t, result.Token)
		assert.NotEmpty(t, result.JTI)
		assert.Equal(t, start.Add(60*time.Minute), result.ExpiresAt)

		// Parse and verify
		var claims auth.Claims
		token, err := jwt.ParseWithClaims(result.Token, &claims, func(token *jwt.Token) (any, error) {
			return &key.PublicKey, nil
		}, jwt.WithTimeFunc(clock.Now))
		require.NoError(t, err)
		assert.True(t, token.Valid)

		assert.Equal(t, "user_123", claims.Subject)
		assert.Equal(t, "messaging-platform", claims.Issuer)
		assert.Equal(t, jwt.ClaimStrings{"messaging-api"}, claims.Audience)
		assert.Equal(t, "sess_456", claims.SessionID)
		assert.Equal(t, "messaging", claims.Scope)
		assert.Equal(t, result.JTI, claims.ID)
		assert.Equal(t, start.Unix(), claims.IssuedAt.Unix())
		assert.Equal(t, start.Add(60*time.Minute).Unix(), claims.ExpiresAt.Unix())

		// Check header
		assert.Equal(t, keyID, token.Header["kid"])
		assert.Equal(t, "RS256", token.Header["alg"])
	})

	t.Run("each token has unique JTI", func(t *testing.T) {
		r1, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)
		r2, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)
		assert.NotEqual(t, r1.JTI, r2.JTI)
	})

	t.Run("advancing clock changes iat and exp", func(t *testing.T) {
		clock.Set(start)
		r1, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		clock.Advance(10 * time.Minute)
		r2, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		assert.Equal(t, start.Add(60*time.Minute), r1.ExpiresAt)
		assert.Equal(t, start.Add(70*time.Minute), r2.ExpiresAt)

		// Reset for other tests
		clock.Set(start)
	})

	t.Run("token rejected with wrong key", func(t *testing.T) {
		result, err := minter.MintAccessToken("user_123", "sess_456")
		require.NoError(t, err)

		otherKey := generateTestKey(t)
		_, err = jwt.Parse(result.Token, func(token *jwt.Token) (any, error) {
			return &otherKey.PublicKey, nil
		}, jwt.WithTimeFunc(clock.Now))
		assert.Error(t, err)
	})
}
