package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// MintResult holds the result of minting an access token.
type MintResult struct {
	Token     string
	JTI       string
	ExpiresAt time.Time
}

// Minter creates signed JWT access tokens per ADR-015 §3.3.
type Minter struct {
	keyStore  KeyStore
	accessTTL time.Duration
	issuer    string
	audience  string
	clock     domain.Clock
}

// MinterConfig holds configuration for creating a Minter.
type MinterConfig struct {
	KeyStore  KeyStore
	AccessTTL time.Duration
	Issuer    string
	Audience  string
	Clock     domain.Clock
}

// NewMinter creates a new JWT minter.
func NewMinter(cfg MinterConfig) *Minter {
	return &Minter{
		keyStore:  cfg.KeyStore,
		accessTTL: cfg.AccessTTL,
		issuer:    cfg.Issuer,
		audience:  cfg.Audience,
		clock:     cfg.Clock,
	}
}

// MintAccessToken creates a signed RS256 JWT access token for the given
// user and session. Returns the signed token string, JTI, and expiration.
func (m *Minter) MintAccessToken(userID, sessionID string) (MintResult, error) {
	privateKey, keyID, err := m.keyStore.SigningKey()
	if err != nil {
		return MintResult{}, fmt.Errorf("get signing key: %w", err)
	}

	now := m.clock.Now().UTC()
	jti := uuid.NewString()
	expiresAt := now.Add(m.accessTTL)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    m.issuer,
			Audience:  jwt.ClaimStrings{m.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        jti,
		},
		SessionID: sessionID,
		Scope:     "messaging",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &claims)
	token.Header["kid"] = keyID

	signed, err := token.SignedString(privateKey)
	if err != nil {
		return MintResult{}, fmt.Errorf("sign access token: %w", err)
	}

	return MintResult{
		Token:     signed,
		JTI:       jti,
		ExpiresAt: expiresAt,
	}, nil
}
