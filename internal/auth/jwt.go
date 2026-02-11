package auth

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// ErrTokenExpired is returned when a validly signed token has expired.
// Callers can use errors.Is to check for this condition without importing
// the JWT library directly.
var ErrTokenExpired = jwt.ErrTokenExpired

// Validator validates JWT access tokens per ADR-015 §3.
type Validator struct {
	keyStore KeyStore
	issuer   string
	audience string
	clock    domain.Clock
}

// ValidatorConfig holds configuration for creating a Validator.
type ValidatorConfig struct {
	KeyStore KeyStore
	Issuer   string
	Audience string
	Clock    domain.Clock
}

// NewValidator creates a new JWT validator.
func NewValidator(cfg ValidatorConfig) *Validator {
	return &Validator{
		keyStore: cfg.KeyStore,
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		clock:    cfg.Clock,
	}
}

// ValidateAccessToken parses and fully validates a JWT access token.
func (v *Validator) ValidateAccessToken(tokenString string) (*Claims, error) {
	return v.parseToken(tokenString, false)
}

// ValidateIgnoreExpiry parses and validates a JWT, waiving only the exp check.
// Used for the refresh flow where the access token may be expired but must
// still have a valid signature, issuer, audience, and kid (ADR-015 §4.2).
func (v *Validator) ValidateIgnoreExpiry(tokenString string) (*Claims, error) {
	return v.parseToken(tokenString, true)
}

func (v *Validator) parseToken(tokenString string, ignoreExpiry bool) (*Claims, error) {
	var claims Claims

	opts := []jwt.ParserOption{
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithTimeFunc(v.clock.Now),
		jwt.WithExpirationRequired(),
	}

	token, err := jwt.ParseWithClaims(tokenString, &claims, v.keyFunc, opts...)
	if err != nil {
		if ignoreExpiry && onlyExpiredError(err) && token != nil {
			// Token is expired but otherwise valid — acceptable for refresh.
		} else {
			return nil, fmt.Errorf("invalid access token: %w", err)
		}
	}

	if claims.SessionID == "" {
		return nil, fmt.Errorf("missing sid claim: %w", domain.ErrUnauthorized)
	}

	return &claims, nil
}

func (v *Validator) keyFunc(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}

	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, fmt.Errorf("missing or invalid kid in token header")
	}

	return v.keyStore.PublicKey(kid)
}

// onlyExpiredError returns true if err contains ErrTokenExpired
// and no other JWT validation errors.
func onlyExpiredError(err error) bool {
	if !errors.Is(err, jwt.ErrTokenExpired) {
		return false
	}
	return !errors.Is(err, jwt.ErrTokenMalformed) &&
		!errors.Is(err, jwt.ErrTokenUnverifiable) &&
		!errors.Is(err, jwt.ErrTokenSignatureInvalid) &&
		!errors.Is(err, jwt.ErrTokenNotValidYet) &&
		!errors.Is(err, jwt.ErrTokenInvalidAudience) &&
		!errors.Is(err, jwt.ErrTokenInvalidIssuer) &&
		!errors.Is(err, jwt.ErrTokenRequiredClaimMissing) &&
		!errors.Is(err, jwt.ErrTokenUsedBeforeIssued)
}
