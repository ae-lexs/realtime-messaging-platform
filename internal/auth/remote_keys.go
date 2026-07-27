package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// Secret IDs (ADR-015 Appendix F). GCP Secret Manager's namespace is flat — an
// ID is a plain name, not a path — so the active key is named by
// jwt-current-key-id and its key pair is named after whatever that resolves to.
//
// These are secret *names*, never secret values: the material they address is
// fetched at runtime. gosec's G101 heuristic cannot tell the two apart, hence
// the suppressions.
const (
	// SecretCurrentKeyID names the secret holding the active signing key's ID.
	SecretCurrentKeyID = "jwt-current-key-id" //nolint:gosec // a secret's name, not its value

	// SecretSigningKeyPrefix prefixes jwt-signing-key-{KEY_ID} (PEM private key).
	SecretSigningKeyPrefix = "jwt-signing-key-" //nolint:gosec // a secret's name, not its value

	// SecretPublicKeyPrefix prefixes jwt-public-key-{KEY_ID} (PEM public key).
	// Addressed by name, never discovered: see RemoteKeyStore.
	SecretPublicKeyPrefix = "jwt-public-key-" //nolint:gosec // a secret's name, not its value

	// SecretOTPPepper names the HMAC pepper for OTP MACs (ADR-015 §1.2).
	SecretOTPPepper = "otp-pepper"
)

// SecretFetcher is the narrow view of a secret store RemoteKeyStore needs.
// internal/secrets.Client satisfies it; tests supply a map.
//
// It reads one secret by name and nothing else. That is not minimalism for its
// own sake — it is the shape the IAM model actually permits; see RemoteKeyStore.
type SecretFetcher interface {
	Latest(ctx context.Context, secretID string) ([]byte, error)
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
// It resolves keys by name — jwt-current-key-id names the active key, and the
// signing and public secrets are named after it — and never enumerates. The
// first deploy is what settled that: an earlier version discovered valid `kid`
// values by listing secrets with the jwt-public-key- prefix, and the pod
// crash-looped on PermissionDenied for secretmanager.secrets.list. The
// permission cannot be scoped to a secret, because listing is a query over the
// project's whole secret collection; making it work would have meant granting
// roles/secretmanager.viewer project-wide.
//
// The cost of resolving by name is that exactly one `kid` is accepted at a
// time, so a rotation invalidates access tokens minted under the previous key.
// They live at most an hour (ADR-015 §3.3), and rotation is unexercised until
// M7 — so the trade is an hour of forced re-auth during a deliberate rotation,
// against a permanent project-wide widening of this service's reach. ADR-015
// v1.1 had already deferred the rotation machinery; this keeps the deferral
// honest instead of paying for it in IAM.
//
// Scope note, unchanged: §3.2's unknown-`kid` immediate refresh with a
// 30-second cooldown is not implemented, and never has been — the v1 build
// shipped only the KeyStore interface and StaticKeyStore.
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

// Refresh reloads the active key ID and the key pair it names.
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

	publicKeys := make(map[string]*rsa.PublicKey, 1)

	// The published public key is preferred over deriving one from the private
	// key: if the two ever disagree, the tokens this service mints would not
	// validate anywhere else, and failing here says so.
	publicPEM, err := s.fetcher.Latest(ctx, SecretPublicKeyPrefix+keyID)
	switch {
	case err == nil:
		publicKey, parseErr := parseRSAPublicKey(publicPEM)
		if parseErr != nil {
			return fmt.Errorf("auth: parse public key %s: %w", keyID, parseErr)
		}
		publicKeys[keyID] = publicKey
	case errors.Is(err, domain.ErrNotFound):
		// No published public key. Derive it, so the service can still
		// validate the tokens it mints, and say so — this is a provisioning
		// gap, and other services will not be able to verify anything.
		publicKeys[keyID] = &privateKey.PublicKey
		s.logger.WarnContext(ctx, "auth.public_key_not_published",
			slog.String("kid", keyID),
			slog.String("secret", SecretPublicKeyPrefix+keyID),
		)
	default:
		return fmt.Errorf("auth: load public key %s: %w", keyID, err)
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
