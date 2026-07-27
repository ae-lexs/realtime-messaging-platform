package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Secret ID conventions (ADR-015 Appendix F). AWS held these across Secrets
// Manager and SSM under a path hierarchy; Secret Manager IDs are flat, so the
// path segments become name components.
// These are secret *names*, never secret values — the material they address
// lives in Secret Manager and is fetched at runtime. gosec's G101 heuristic
// cannot tell the two apart, hence the suppressions.
const (
	// SecretCurrentKeyID names the secret holding the active signing key's ID.
	SecretCurrentKeyID = "jwt-current-key-id" //nolint:gosec // a secret's name, not its value

	// SecretSigningKeyPrefix prefixes jwt-signing-key-{KEY_ID} (PEM private key).
	SecretSigningKeyPrefix = "jwt-signing-key-" //nolint:gosec // a secret's name, not its value

	// SecretPublicKeyPrefix prefixes jwt-public-key-{KEY_ID} (PEM public key).
	// Listing by this prefix is how the set of acceptable `kid` values is
	// discovered, so a key added during rotation becomes valid for
	// verification without a deploy.
	SecretPublicKeyPrefix = "jwt-public-key-" //nolint:gosec // a secret's name, not its value

	// SecretOTPPepper names the HMAC pepper for OTP MACs (ADR-015 §1.2).
	SecretOTPPepper = "otp-pepper"
)

// SecretFetcher is the narrow view of a secret store RemoteKeyStore needs.
// internal/secrets.Client satisfies it; tests supply a map.
type SecretFetcher interface {
	Latest(ctx context.Context, secretID string) ([]byte, error)
	ListIDsWithPrefix(ctx context.Context, prefix string) ([]string, error)
}

// Compile-time check: RemoteKeyStore satisfies KeyStore.
var _ KeyStore = (*RemoteKeyStore)(nil)

// RemoteKeyStoreConfig holds the dependencies for RemoteKeyStore.
type RemoteKeyStoreConfig struct {
	Fetcher SecretFetcher

	// RefreshInterval bounds how stale the key set may be. ADR-015 §3.2
	// specifies 5 minutes.
	RefreshInterval time.Duration

	Logger *slog.Logger
}

// RemoteKeyStore is a KeyStore backed by a secret store, holding the key
// material in memory between refreshes (ADR-015 §3.2).
//
// Scope note: §3.2 also specifies that a token bearing an unknown `kid`
// triggers a single out-of-cache refresh with a 30-second cooldown. That is
// deliberately NOT implemented here, and the omission is recorded in ADR-015
// v1.1 rather than left to be discovered: the AWS build shipped only the
// KeyStore interface and StaticKeyStore, so building it during a re-home would
// be new scope, and it is reachable only mid-rotation — which nothing exercises
// before the M7 verification module. Until then the 5-minute refresh bounds how
// long a freshly added key stays unrecognised.
type RemoteKeyStore struct {
	fetcher  SecretFetcher
	interval time.Duration
	logger   *slog.Logger

	mu         sync.RWMutex
	privateKey *rsa.PrivateKey
	keyID      string
	publicKeys map[string]*rsa.PublicKey
}

// NewRemoteKeyStore loads the key set once and fails if it cannot.
//
// The failure is intentional and is ADR-015's signing_key_required_for_startup
// invariant: a Chat Mgmt that starts without a signing key would accept auth
// traffic it cannot possibly serve, failing every login at request time instead
// of failing the rollout, where a readiness probe can act on it.
func NewRemoteKeyStore(ctx context.Context, cfg RemoteKeyStoreConfig) (*RemoteKeyStore, error) {
	if cfg.Fetcher == nil {
		return nil, fmt.Errorf("auth: remote key store requires a fetcher")
	}
	if cfg.RefreshInterval <= 0 {
		return nil, fmt.Errorf("auth: remote key store refresh interval must be positive")
	}
	if cfg.Logger == nil {
		return nil, fmt.Errorf("auth: remote key store requires a logger")
	}

	store := &RemoteKeyStore{
		fetcher:    cfg.Fetcher,
		interval:   cfg.RefreshInterval,
		logger:     cfg.Logger,
		publicKeys: map[string]*rsa.PublicKey{},
	}

	if err := store.Refresh(ctx); err != nil {
		return nil, err
	}

	return store, nil
}

// SigningKey returns the current private signing key and its key ID.
func (s *RemoteKeyStore) SigningKey() (*rsa.PrivateKey, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.privateKey == nil {
		return nil, "", fmt.Errorf("auth: no signing key available")
	}
	return s.privateKey, s.keyID, nil
}

// PublicKey returns the public key for the given key ID.
func (s *RemoteKeyStore) PublicKey(kid string) (*rsa.PublicKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.publicKeys[kid]
	if !ok {
		return nil, fmt.Errorf("auth: unknown key ID %q", kid)
	}
	return key, nil
}

// Refresh reloads the current key ID, its private key, and every public key.
//
// It builds the whole set before taking the write lock, so a failure part-way
// through leaves the previously loaded keys serving traffic rather than a
// half-populated map — a partial refresh would reject tokens signed by keys the
// store held a moment earlier.
func (s *RemoteKeyStore) Refresh(ctx context.Context) error {
	currentKeyID, err := s.fetcher.Latest(ctx, SecretCurrentKeyID)
	if err != nil {
		return fmt.Errorf("auth: load current key ID: %w", err)
	}
	keyID := strings.TrimSpace(string(currentKeyID))
	if keyID == "" {
		return fmt.Errorf("auth: %s is empty", SecretCurrentKeyID)
	}

	signingPEM, err := s.fetcher.Latest(ctx, SecretSigningKeyPrefix+keyID)
	if err != nil {
		return fmt.Errorf("auth: load signing key %s: %w", keyID, err)
	}
	privateKey, err := parseRSAPrivateKey(signingPEM)
	if err != nil {
		return fmt.Errorf("auth: parse signing key %s: %w", keyID, err)
	}

	publicIDs, err := s.fetcher.ListIDsWithPrefix(ctx, SecretPublicKeyPrefix)
	if err != nil {
		return fmt.Errorf("auth: list public keys: %w", err)
	}

	publicKeys := make(map[string]*rsa.PublicKey, len(publicIDs))
	for _, secretID := range publicIDs {
		kid := strings.TrimPrefix(secretID, SecretPublicKeyPrefix)

		publicPEM, fetchErr := s.fetcher.Latest(ctx, secretID)
		if fetchErr != nil {
			return fmt.Errorf("auth: load public key %s: %w", kid, fetchErr)
		}
		publicKey, parseErr := parseRSAPublicKey(publicPEM)
		if parseErr != nil {
			return fmt.Errorf("auth: parse public key %s: %w", kid, parseErr)
		}
		publicKeys[kid] = publicKey
	}

	// The signing key's own public half must be in the verify set, or this
	// service would mint tokens it cannot itself validate on the REST path.
	if _, ok := publicKeys[keyID]; !ok {
		publicKeys[keyID] = &privateKey.PublicKey
		s.logger.WarnContext(ctx, "auth.public_key_missing_for_signing_key",
			slog.String("kid", keyID),
			slog.String("secret", SecretPublicKeyPrefix+keyID),
		)
	}

	s.mu.Lock()
	s.privateKey = privateKey
	s.keyID = keyID
	s.publicKeys = publicKeys
	s.mu.Unlock()

	s.logger.DebugContext(ctx, "auth.keys_refreshed",
		slog.String("kid", keyID),
		slog.Int("public_keys", len(publicKeys)),
	)

	return nil
}

// Run refreshes the key set on RefreshInterval until ctx is cancelled.
//
// The caller owns this goroutine — the composition root starts it and joins it
// during shutdown — so nothing here spawns work the constructor's caller cannot
// see or wait on.
//
// A failed refresh is logged and the previous key set is kept. Tearing down a
// working key set because Secret Manager was briefly unreachable would convert
// a transient dependency failure into a total auth outage; ADR-013's
// fail-closed rule is about the revocation and rate-limit checks, not about
// discarding material already held.
func (s *RemoteKeyStore) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Refresh(ctx); err != nil {
				s.logger.ErrorContext(ctx, "auth.key_refresh_failed",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// parseRSAPrivateKey accepts both PEM encodings openssl produces: PKCS#8
// ("PRIVATE KEY", what `openssl genpkey` writes and what scripts/auth-keys.sh
// stores) and PKCS#1 ("RSA PRIVATE KEY", what older openssl invocations write).
// Accepting both costs a few lines and avoids a confusing "failed to parse"
// against a key that is perfectly valid.
func parseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKCS#1 or PKCS#8 key: %w", err)
	}

	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is %T, not RSA", parsed)
	}
	return key, nil
}

// parseRSAPublicKey accepts PKIX ("PUBLIC KEY") and PKCS#1 ("RSA PUBLIC KEY").
func parseRSAPublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}

	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKIX or PKCS#1 public key: %w", err)
	}

	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is %T, not RSA", parsed)
	}
	return key, nil
}
