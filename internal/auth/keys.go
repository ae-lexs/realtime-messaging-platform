package auth

import (
	"crypto/rsa"
	"fmt"
	"sync"
)

// KeyStore provides access to JWT signing and verification keys.
// Implementations load keys from Secrets Manager/SSM (production)
// or hold them in memory (testing).
type KeyStore interface {
	// SigningKey returns the current private signing key and its key ID.
	SigningKey() (*rsa.PrivateKey, string, error)

	// PublicKey returns the public key for the given key ID.
	PublicKey(kid string) (*rsa.PublicKey, error)
}

// StaticKeyStore is a KeyStore backed by in-memory keys. Use for testing only.
type StaticKeyStore struct {
	mu         sync.RWMutex
	privateKey *rsa.PrivateKey
	keyID      string
	publicKeys map[string]*rsa.PublicKey
}

// NewStaticKeyStore creates a StaticKeyStore with a single key pair.
func NewStaticKeyStore(privateKey *rsa.PrivateKey, keyID string) *StaticKeyStore {
	return &StaticKeyStore{
		privateKey: privateKey,
		keyID:      keyID,
		publicKeys: map[string]*rsa.PublicKey{
			keyID: &privateKey.PublicKey,
		},
	}
}

// SigningKey returns the private signing key and its key ID.
func (s *StaticKeyStore) SigningKey() (*rsa.PrivateKey, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.privateKey == nil {
		return nil, "", fmt.Errorf("no signing key available")
	}
	return s.privateKey, s.keyID, nil
}

// PublicKey returns the public key for the given key ID.
func (s *StaticKeyStore) PublicKey(kid string) (*rsa.PublicKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pk, ok := s.publicKeys[kid]
	if !ok {
		return nil, fmt.Errorf("unknown key ID %q", kid)
	}
	return pk, nil
}

// AddPublicKey adds a public key for testing key rotation scenarios.
func (s *StaticKeyStore) AddPublicKey(kid string, key *rsa.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publicKeys[kid] = key
}
