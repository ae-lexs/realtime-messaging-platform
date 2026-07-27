package auth_test

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
)

// fakeFetcher is an in-memory SecretFetcher. Its payloads are mutable so a
// test can rotate keys underneath a live store.
type fakeFetcher struct {
	mu      sync.Mutex
	secrets map[string][]byte
	err     error
}

func newFakeFetcher(secrets map[string][]byte) *fakeFetcher {
	return &fakeFetcher{secrets: secrets}
}

func (f *fakeFetcher) Latest(_ context.Context, secretID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return nil, f.err
	}
	payload, ok := f.secrets[secretID]
	if !ok {
		return nil, errors.New("no such secret: " + secretID)
	}
	return payload, nil
}

func (f *fakeFetcher) ListIDsWithPrefix(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return nil, f.err
	}
	var ids []string
	for id := range f.secrets {
		if strings.HasPrefix(id, prefix) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (f *fakeFetcher) set(secretID string, payload []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets[secretID] = payload
}

func (f *fakeFetcher) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func privatePEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func publicPEM(t *testing.T, key *rsa.PublicKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestKeyStore(t *testing.T, fetcher auth.SecretFetcher) *auth.RemoteKeyStore {
	t.Helper()

	store, err := auth.NewRemoteKeyStore(context.Background(), auth.RemoteKeyStoreConfig{
		Fetcher:         fetcher,
		RefreshInterval: time.Minute,
		Logger:          discardLogger(),
	})
	require.NoError(t, err)
	return store
}

func TestRemoteKeyStoreLoadsTheKeySet(t *testing.T) {
	// Arrange — one active key plus a second whose public half is still
	// published, the shape a half-finished rotation leaves behind.
	active := generateTestKey(t)
	retiring := generateTestKey(t)

	fetcher := newFakeFetcher(map[string][]byte{
		auth.SecretCurrentKeyID:                    []byte("key-active"),
		auth.SecretSigningKeyPrefix + "key-active": privatePEM(t, active),
		auth.SecretPublicKeyPrefix + "key-active":  publicPEM(t, &active.PublicKey),
		auth.SecretPublicKeyPrefix + "key-old":     publicPEM(t, &retiring.PublicKey),
	})

	// Act
	store := newTestKeyStore(t, fetcher)

	// Assert
	signing, kid, err := store.SigningKey()
	require.NoError(t, err)
	assert.Equal(t, "key-active", kid)
	assert.Equal(t, active, signing)

	activePub, err := store.PublicKey("key-active")
	require.NoError(t, err)
	assert.Equal(t, &active.PublicKey, activePub)

	// The retiring key still verifies tokens minted before the switch —
	// dropping it would invalidate every access token in flight.
	oldPub, err := store.PublicKey("key-old")
	require.NoError(t, err)
	assert.Equal(t, &retiring.PublicKey, oldPub)

	_, err = store.PublicKey("key-never-issued")
	assert.Error(t, err)
}

// TestRemoteKeyStoreRefusesToStartWithoutKeys pins ADR-015's
// signing_key_required_for_startup invariant. Starting without a signing key
// would put a Chat Mgmt into the load balancer that fails every login at
// request time, instead of failing the rollout where a probe can act on it.
func TestRemoteKeyStoreRefusesToStartWithoutKeys(t *testing.T) {
	key := generateTestKey(t)

	tests := []struct {
		name    string
		secrets map[string][]byte
		wantErr string
	}{
		{
			name:    "no current key ID",
			secrets: map[string][]byte{},
			wantErr: "load current key ID",
		},
		{
			name:    "current key ID is blank",
			secrets: map[string][]byte{auth.SecretCurrentKeyID: []byte("  \n")},
			wantErr: "is empty",
		},
		{
			// Terraform created the secret container but scripts/auth-keys.sh
			// never ran — the likeliest first-deploy failure.
			name:    "no signing key for the named ID",
			secrets: map[string][]byte{auth.SecretCurrentKeyID: []byte("key-active")},
			wantErr: "load signing key",
		},
		{
			name: "signing key is not PEM",
			secrets: map[string][]byte{
				auth.SecretCurrentKeyID:                    []byte("key-active"),
				auth.SecretSigningKeyPrefix + "key-active": []byte("not a key"),
			},
			wantErr: "parse signing key",
		},
		{
			name: "public key is not PEM",
			secrets: map[string][]byte{
				auth.SecretCurrentKeyID:                    []byte("key-active"),
				auth.SecretSigningKeyPrefix + "key-active": privatePEM(t, key),
				auth.SecretPublicKeyPrefix + "key-old":     []byte("not a key"),
			},
			wantErr: "parse public key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.NewRemoteKeyStore(context.Background(), auth.RemoteKeyStoreConfig{
				Fetcher:         newFakeFetcher(tt.secrets),
				RefreshInterval: time.Minute,
				Logger:          discardLogger(),
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestRemoteKeyStoreDerivesTheSigningPublicKey covers the case where
// jwt-public-key-{KEY_ID} was never published: the service must still be able
// to validate the tokens it mints itself, on the REST path.
func TestRemoteKeyStoreDerivesTheSigningPublicKey(t *testing.T) {
	// Arrange — a signing key with no matching public-key secret.
	key := generateTestKey(t)
	fetcher := newFakeFetcher(map[string][]byte{
		auth.SecretCurrentKeyID:                    []byte("key-active"),
		auth.SecretSigningKeyPrefix + "key-active": privatePEM(t, key),
	})

	// Act
	store := newTestKeyStore(t, fetcher)

	// Assert
	pub, err := store.PublicKey("key-active")
	require.NoError(t, err)
	assert.Equal(t, &key.PublicKey, pub)
}

func TestRemoteKeyStoreRefreshPicksUpARotation(t *testing.T) {
	// Arrange
	first := generateTestKey(t)
	second := generateTestKey(t)

	fetcher := newFakeFetcher(map[string][]byte{
		auth.SecretCurrentKeyID:               []byte("key-1"),
		auth.SecretSigningKeyPrefix + "key-1": privatePEM(t, first),
		auth.SecretPublicKeyPrefix + "key-1":  publicPEM(t, &first.PublicKey),
	})
	store := newTestKeyStore(t, fetcher)

	// Act — rotate: publish the new pair and repoint the current key ID.
	fetcher.set(auth.SecretSigningKeyPrefix+"key-2", privatePEM(t, second))
	fetcher.set(auth.SecretPublicKeyPrefix+"key-2", publicPEM(t, &second.PublicKey))
	fetcher.set(auth.SecretCurrentKeyID, []byte("key-2"))
	require.NoError(t, store.Refresh(context.Background()))

	// Assert
	signing, kid, err := store.SigningKey()
	require.NoError(t, err)
	assert.Equal(t, "key-2", kid)
	assert.Equal(t, second, signing)

	// The superseded key keeps verifying until its tokens expire.
	oldPub, err := store.PublicKey("key-1")
	require.NoError(t, err)
	assert.Equal(t, &first.PublicKey, oldPub)
}

// TestRemoteKeyStoreKeepsKeysWhenRefreshFails is the behaviour that keeps a
// Secret Manager blip from becoming a total auth outage: a failed refresh must
// leave the loaded key set serving, not clear it. A half-populated map would
// reject tokens the store could validate a moment earlier.
func TestRemoteKeyStoreKeepsKeysWhenRefreshFails(t *testing.T) {
	// Arrange
	key := generateTestKey(t)
	fetcher := newFakeFetcher(map[string][]byte{
		auth.SecretCurrentKeyID:                    []byte("key-active"),
		auth.SecretSigningKeyPrefix + "key-active": privatePEM(t, key),
		auth.SecretPublicKeyPrefix + "key-active":  publicPEM(t, &key.PublicKey),
	})
	store := newTestKeyStore(t, fetcher)

	// Act
	fetcher.fail(errors.New("secret manager unavailable"))
	err := store.Refresh(context.Background())

	// Assert
	require.Error(t, err)

	signing, kid, signErr := store.SigningKey()
	require.NoError(t, signErr)
	assert.Equal(t, "key-active", kid)
	assert.Equal(t, key, signing)

	pub, pubErr := store.PublicKey("key-active")
	require.NoError(t, pubErr)
	assert.Equal(t, &key.PublicKey, pub)
}

// TestRemoteKeyStoreRunStopsOnContextCancel pins the goroutine-ownership
// contract: Run belongs to its caller and must return when told, so the
// composition root can join it during shutdown. The package's goleak check
// would catch a leak, but only after the fact and without saying why.
func TestRemoteKeyStoreRunStopsOnContextCancel(t *testing.T) {
	// Arrange
	key := generateTestKey(t)
	fetcher := newFakeFetcher(map[string][]byte{
		auth.SecretCurrentKeyID:                    []byte("key-active"),
		auth.SecretSigningKeyPrefix + "key-active": privatePEM(t, key),
		auth.SecretPublicKeyPrefix + "key-active":  publicPEM(t, &key.PublicKey),
	})
	store := newTestKeyStore(t, fetcher)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	// Act
	go func() {
		defer close(done)
		store.Run(ctx)
	}()
	cancel()

	// Assert
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}
