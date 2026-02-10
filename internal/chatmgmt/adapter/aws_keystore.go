package adapter

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// smClient is the narrow consumer-defined interface for Secrets Manager operations.
type smClient interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// ssmClient is the narrow consumer-defined interface for SSM Parameter Store operations.
type ssmClient interface {
	GetParameter(ctx context.Context, params *awsssm.GetParameterInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error)
	GetParametersByPath(ctx context.Context, params *awsssm.GetParametersByPathInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error)
}

// Compile-time check: AWSKeyStore implements auth.KeyStore.
var _ auth.KeyStore = (*AWSKeyStore)(nil)

// AWSKeyStore implements auth.KeyStore by loading keys from AWS Secrets Manager
// (private signing key) and SSM Parameter Store (public verification keys).
//
// The signing key is eagerly loaded at construction time per ADR-015: the service
// MUST NOT start without a signing key. Public keys are cached with a configurable
// TTL (default 300s per TBD-PR1-1) and refreshed lazily on read.
type AWSKeyStore struct {
	sm    smClient
	ssm   ssmClient
	clock domain.Clock

	mu                    sync.RWMutex
	privateKey            *rsa.PrivateKey
	currentKeyID          string
	publicKeys            map[string]*rsa.PublicKey
	publicKeysLoadedAt    time.Time
	lastUnknownKidRefresh time.Time
	cacheTTL              time.Duration
	kidCooldown           time.Duration
}

const (
	// ssmCurrentKeyIDPath is the SSM parameter that stores the active signing key ID.
	ssmCurrentKeyIDPath = "/messaging/jwt/current-key-id"

	// ssmPublicKeysPathPrefix is the SSM parameter path prefix for public keys.
	// Each key is stored at /messaging/jwt/public-keys/{KEY_ID}.
	ssmPublicKeysPathPrefix = "/messaging/jwt/public-keys/"

	// smSigningKeyPrefix is the Secrets Manager secret name prefix for private keys.
	smSigningKeyPrefix = "jwt/signing-key/"

	// defaultCacheTTL is the public key cache TTL per TBD-PR1-1 (300s / 5 minutes).
	defaultCacheTTL = 300 * time.Second

	// defaultKidCooldown is the cooldown between unknown kid SSM refreshes per TBD-PR1-1 (30s).
	defaultKidCooldown = 30 * time.Second
)

// NewAWSKeyStore creates an AWSKeyStore and eagerly loads all keys from AWS.
// This is synchronous per 05_CONCURRENCY: no goroutines in constructors.
//
// The constructor:
//  1. Fetches the current key ID from SSM
//  2. Fetches the private signing key from Secrets Manager
//  3. Parses the PEM-encoded private key
//  4. Loads all public keys from SSM
//  5. Parses each PEM-encoded public key
//
// Returns an error if any step fails. Per ADR-015, the service MUST NOT start
// without a valid signing key.
func NewAWSKeyStore(ctx context.Context, sm smClient, ssm ssmClient, clock domain.Clock) (*AWSKeyStore, error) {
	// Step 1: Fetch current key ID from SSM.
	keyIDOutput, err := ssm.GetParameter(ctx, &awsssm.GetParameterInput{
		Name: aws.String(ssmCurrentKeyIDPath),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching current key ID from SSM: %w", err)
	}
	if keyIDOutput.Parameter == nil || keyIDOutput.Parameter.Value == nil {
		return nil, fmt.Errorf("SSM parameter %s has no value", ssmCurrentKeyIDPath)
	}
	currentKeyID := *keyIDOutput.Parameter.Value

	// Step 2: Fetch private signing key from Secrets Manager.
	secretName := smSigningKeyPrefix + currentKeyID
	secretOutput, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching signing key %q from Secrets Manager: %w", secretName, err)
	}
	if secretOutput.SecretString == nil {
		return nil, fmt.Errorf("signing key %q has no secret string", secretName)
	}

	// Step 3: Parse PEM-encoded private key.
	privateKey, err := parseRSAPrivateKey(*secretOutput.SecretString)
	if err != nil {
		return nil, fmt.Errorf("parsing private key for key ID %q: %w", currentKeyID, err)
	}

	// Step 4: Load all public keys from SSM.
	publicKeys, err := loadPublicKeysFromSSM(ctx, ssm)
	if err != nil {
		return nil, fmt.Errorf("loading public keys from SSM: %w", err)
	}

	return &AWSKeyStore{
		sm:                 sm,
		ssm:                ssm,
		clock:              clock,
		privateKey:         privateKey,
		currentKeyID:       currentKeyID,
		publicKeys:         publicKeys,
		publicKeysLoadedAt: clock.Now(),
		cacheTTL:           defaultCacheTTL,
		kidCooldown:        defaultKidCooldown,
	}, nil
}

// SigningKey returns the current private signing key and its key ID.
// Thread-safe via RLock.
func (ks *AWSKeyStore) SigningKey() (*rsa.PrivateKey, string, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	if ks.privateKey == nil {
		return nil, "", fmt.Errorf("no signing key available")
	}
	return ks.privateKey, ks.currentKeyID, nil
}

// PublicKey returns the public key for the given key ID.
//
// Cache strategy (per TBD-PR1-1):
//   - If kid is found and cache is fresh, return immediately.
//   - If cache is expired (age > cacheTTL), refresh all public keys inline.
//   - If kid is not found and cooldown is expired, do a single SSM refresh.
//   - If kid is still not found after refresh, return an error.
//
// NOTE: This method uses context.Background() for SSM refresh calls because the
// auth.KeyStore interface does not accept context. This is the documented exception
// per 04_CONTEXT: "adapters that perform cache refresh on read path may use
// context.Background() when the triggering interface doesn't accept context."
func (ks *AWSKeyStore) PublicKey(kid string) (*rsa.PublicKey, error) {
	// Fast path: RLock check.
	ks.mu.RLock()
	now := ks.clock.Now()
	cacheExpired := now.Sub(ks.publicKeysLoadedAt) > ks.cacheTTL

	if !cacheExpired {
		if pk, ok := ks.publicKeys[kid]; ok {
			ks.mu.RUnlock()
			return pk, nil
		}
	}
	ks.mu.RUnlock()

	// Slow path: cache expired or kid not found — need refresh.
	if cacheExpired {
		if err := ks.refreshPublicKeys(context.Background()); err != nil {
			return nil, fmt.Errorf("refreshing public keys (cache expired): %w", err)
		}

		ks.mu.RLock()
		pk, ok := ks.publicKeys[kid]
		ks.mu.RUnlock()
		if ok {
			return pk, nil
		}
	}

	// Kid not found after cache-expiry refresh (or cache was fresh but kid missing).
	// Check cooldown before doing an unknown-kid refresh.
	ks.mu.RLock()
	cooldownActive := now.Sub(ks.lastUnknownKidRefresh) <= ks.kidCooldown
	ks.mu.RUnlock()

	if cooldownActive {
		return nil, fmt.Errorf("unknown key ID %q (cooldown active)", kid)
	}

	// Single SSM refresh for unknown kid with cooldown update.
	if err := ks.refreshPublicKeysWithCooldown(context.Background()); err != nil {
		return nil, fmt.Errorf("refreshing public keys (unknown kid %q): %w", kid, err)
	}

	ks.mu.RLock()
	pk, ok := ks.publicKeys[kid]
	ks.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown key ID %q", kid)
	}
	return pk, nil
}

// refreshPublicKeys fetches all public keys from SSM and updates the cache.
// Acquires write Lock.
func (ks *AWSKeyStore) refreshPublicKeys(ctx context.Context) error {
	publicKeys, err := loadPublicKeysFromSSM(ctx, ks.ssm)
	if err != nil {
		return fmt.Errorf("loading public keys from SSM: %w", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.publicKeys = publicKeys
	ks.publicKeysLoadedAt = ks.clock.Now()
	return nil
}

// refreshPublicKeysWithCooldown refreshes public keys and updates the unknown kid cooldown.
// Acquires write Lock.
func (ks *AWSKeyStore) refreshPublicKeysWithCooldown(ctx context.Context) error {
	publicKeys, err := loadPublicKeysFromSSM(ctx, ks.ssm)
	if err != nil {
		return fmt.Errorf("loading public keys from SSM: %w", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.publicKeys = publicKeys
	ks.publicKeysLoadedAt = ks.clock.Now()
	ks.lastUnknownKidRefresh = ks.clock.Now()
	return nil
}

// loadPublicKeysFromSSM fetches all public key parameters under the SSM path prefix
// and parses each into an *rsa.PublicKey. The key ID is derived from the parameter
// name by trimming the path prefix.
func loadPublicKeysFromSSM(ctx context.Context, client ssmClient) (map[string]*rsa.PublicKey, error) {
	output, err := client.GetParametersByPath(ctx, &awsssm.GetParametersByPathInput{
		Path:      aws.String(ssmPublicKeysPathPrefix),
		Recursive: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("GetParametersByPath %q: %w", ssmPublicKeysPathPrefix, err)
	}

	publicKeys := make(map[string]*rsa.PublicKey, len(output.Parameters))
	for _, param := range output.Parameters {
		if param.Name == nil || param.Value == nil {
			continue
		}
		kid := strings.TrimPrefix(*param.Name, ssmPublicKeysPathPrefix)
		pk, err := parseRSAPublicKey(*param.Value)
		if err != nil {
			return nil, fmt.Errorf("parsing public key for kid %q: %w", kid, err)
		}
		publicKeys[kid] = pk
	}

	return publicKeys, nil
}

// parseRSAPrivateKey parses a PEM-encoded RSA private key. It supports both
// PKCS#1 (RSA PRIVATE KEY) and PKCS#8 (PRIVATE KEY) formats.
func parseRSAPrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key data")
	}

	// Try PKCS#1 first (RSA PRIVATE KEY).
	if block.Type == "RSA PRIVATE KEY" {
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing PKCS#1 private key: %w", err)
		}
		return key, nil
	}

	// Try PKCS#8 (PRIVATE KEY).
	keyIface, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing PKCS#8 private key: %w", err)
	}

	rsaKey, ok := keyIface.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("PKCS#8 key is not RSA (got %T)", keyIface)
	}
	return rsaKey, nil
}

// parseRSAPublicKey parses a PEM-encoded RSA public key in PKIX format.
func parseRSAPublicKey(pemData string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in public key data")
	}

	keyIface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing PKIX public key: %w", err)
	}

	rsaKey, ok := keyIface.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("PKIX key is not RSA (got %T)", keyIface)
	}
	return rsaKey, nil
}
