package adapter

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
)

// --- Stubs ---

// stubSMClient implements smClient for testing.
type stubSMClient struct {
	getSecretValueFn func(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

func (s *stubSMClient) GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return s.getSecretValueFn(ctx, params, optFns...)
}

// stubSSMClient implements ssmClient for testing.
type stubSSMClient struct {
	getParameterFn        func(ctx context.Context, params *awsssm.GetParameterInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error)
	getParametersByPathFn func(ctx context.Context, params *awsssm.GetParametersByPathInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error)

	// getParametersByPathCallCount tracks how many times GetParametersByPath was called.
	getParametersByPathCallCount int
}

func (s *stubSSMClient) GetParameter(ctx context.Context, params *awsssm.GetParameterInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
	return s.getParameterFn(ctx, params, optFns...)
}

func (s *stubSSMClient) GetParametersByPath(ctx context.Context, params *awsssm.GetParametersByPathInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
	s.getParametersByPathCallCount++
	return s.getParametersByPathFn(ctx, params, optFns...)
}

// --- Test Helpers ---

// testKeyPair generates an RSA key pair and returns PEM-encoded strings.
func testKeyPair(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	return privateKey, string(privPEM), string(pubPEM)
}

// newValidStubs creates SM and SSM stubs that return valid key data.
func newValidStubs(t *testing.T, keyID, privPEM, pubPEM string) (*stubSMClient, *stubSSMClient) {
	t.Helper()

	sm := &stubSMClient{
		getSecretValueFn: func(_ context.Context, params *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			expectedSecret := smSigningKeyPrefix + keyID
			if aws.ToString(params.SecretId) != expectedSecret {
				return nil, fmt.Errorf("unexpected secret ID: %s", aws.ToString(params.SecretId))
			}
			return &secretsmanager.GetSecretValueOutput{
				SecretString: aws.String(privPEM),
			}, nil
		},
	}

	ssm := &stubSSMClient{
		getParameterFn: func(_ context.Context, params *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
			if aws.ToString(params.Name) != ssmCurrentKeyIDPath {
				return nil, fmt.Errorf("unexpected parameter name: %s", aws.ToString(params.Name))
			}
			return &awsssm.GetParameterOutput{
				Parameter: &ssmtypes.Parameter{
					Name:  aws.String(ssmCurrentKeyIDPath),
					Value: aws.String(keyID),
				},
			}, nil
		},
		getParametersByPathFn: func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
			return &awsssm.GetParametersByPathOutput{
				Parameters: []ssmtypes.Parameter{
					{
						Name:  aws.String(ssmPublicKeysPathPrefix + keyID),
						Value: aws.String(pubPEM),
					},
				},
			}, nil
		},
	}

	return sm, ssm
}

// --- Tests ---

func TestNewAWSKeyStore(t *testing.T) {
	t.Run("success with valid keys", func(t *testing.T) {
		// Arrange
		expectedKey, privPEM, pubPEM := testKeyPair(t)
		keyID := "test-key-001"
		sm, ssm := newValidStubs(t, keyID, privPEM, pubPEM)
		clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))

		// Act
		ks, err := NewAWSKeyStore(context.Background(), sm, ssm, clock)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, ks)
		assert.Equal(t, keyID, ks.currentKeyID)
		assert.True(t, expectedKey.Equal(ks.privateKey))
		assert.Len(t, ks.publicKeys, 1)
		assert.Contains(t, ks.publicKeys, keyID)
	})

	t.Run("multiple public keys", func(t *testing.T) {
		// Arrange
		_, privPEM, pubPEM1 := testKeyPair(t)
		_, _, pubPEM2 := testKeyPair(t)
		keyID := "key-current"
		sm, ssmStub := newValidStubs(t, keyID, privPEM, pubPEM1)

		// Override GetParametersByPath to return two keys.
		ssmStub.getParametersByPathFn = func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
			return &awsssm.GetParametersByPathOutput{
				Parameters: []ssmtypes.Parameter{
					{
						Name:  aws.String(ssmPublicKeysPathPrefix + "key-current"),
						Value: aws.String(pubPEM1),
					},
					{
						Name:  aws.String(ssmPublicKeysPathPrefix + "key-old"),
						Value: aws.String(pubPEM2),
					},
				},
			}, nil
		}
		clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))

		// Act
		ks, err := NewAWSKeyStore(context.Background(), sm, ssmStub, clock)

		// Assert
		require.NoError(t, err)
		assert.Len(t, ks.publicKeys, 2)
		assert.Contains(t, ks.publicKeys, "key-current")
		assert.Contains(t, ks.publicKeys, "key-old")
	})
}

func TestNewAWSKeyStore_Errors(t *testing.T) {
	_, validPrivPEM, _ := testKeyPair(t)

	tests := []struct {
		name     string
		smSetup  func() *stubSMClient
		ssmSetup func() *stubSSMClient
		wantErr  string
	}{
		{
			name: "SSM GetParameter fails",
			smSetup: func() *stubSMClient {
				return &stubSMClient{
					getSecretValueFn: func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
						return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(validPrivPEM)}, nil
					},
				}
			},
			ssmSetup: func() *stubSSMClient {
				return &stubSSMClient{
					getParameterFn: func(_ context.Context, _ *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
						return nil, fmt.Errorf("ssm unavailable")
					},
					getParametersByPathFn: func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
						return &awsssm.GetParametersByPathOutput{}, nil
					},
				}
			},
			wantErr: "fetching current key ID from SSM",
		},
		{
			name: "SSM parameter has nil value",
			smSetup: func() *stubSMClient {
				return &stubSMClient{
					getSecretValueFn: func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
						return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(validPrivPEM)}, nil
					},
				}
			},
			ssmSetup: func() *stubSSMClient {
				return &stubSSMClient{
					getParameterFn: func(_ context.Context, _ *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
						return &awsssm.GetParameterOutput{
							Parameter: &ssmtypes.Parameter{
								Name:  aws.String(ssmCurrentKeyIDPath),
								Value: nil,
							},
						}, nil
					},
					getParametersByPathFn: func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
						return &awsssm.GetParametersByPathOutput{}, nil
					},
				}
			},
			wantErr: "has no value",
		},
		{
			name: "Secrets Manager unavailable",
			smSetup: func() *stubSMClient {
				return &stubSMClient{
					getSecretValueFn: func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
						return nil, fmt.Errorf("secrets manager unavailable")
					},
				}
			},
			ssmSetup: func() *stubSSMClient {
				return &stubSSMClient{
					getParameterFn: func(_ context.Context, _ *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
						return &awsssm.GetParameterOutput{
							Parameter: &ssmtypes.Parameter{
								Name:  aws.String(ssmCurrentKeyIDPath),
								Value: aws.String("key-1"),
							},
						}, nil
					},
					getParametersByPathFn: func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
						return &awsssm.GetParametersByPathOutput{}, nil
					},
				}
			},
			wantErr: "fetching signing key",
		},
		{
			name: "Secrets Manager returns nil SecretString",
			smSetup: func() *stubSMClient {
				return &stubSMClient{
					getSecretValueFn: func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
						return &secretsmanager.GetSecretValueOutput{SecretString: nil}, nil
					},
				}
			},
			ssmSetup: func() *stubSSMClient {
				return &stubSSMClient{
					getParameterFn: func(_ context.Context, _ *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
						return &awsssm.GetParameterOutput{
							Parameter: &ssmtypes.Parameter{
								Name:  aws.String(ssmCurrentKeyIDPath),
								Value: aws.String("key-1"),
							},
						}, nil
					},
					getParametersByPathFn: func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
						return &awsssm.GetParametersByPathOutput{}, nil
					},
				}
			},
			wantErr: "has no secret string",
		},
		{
			name: "invalid private key PEM",
			smSetup: func() *stubSMClient {
				return &stubSMClient{
					getSecretValueFn: func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
						return &secretsmanager.GetSecretValueOutput{
							SecretString: aws.String("not-a-pem"),
						}, nil
					},
				}
			},
			ssmSetup: func() *stubSSMClient {
				return &stubSSMClient{
					getParameterFn: func(_ context.Context, _ *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
						return &awsssm.GetParameterOutput{
							Parameter: &ssmtypes.Parameter{
								Name:  aws.String(ssmCurrentKeyIDPath),
								Value: aws.String("key-1"),
							},
						}, nil
					},
					getParametersByPathFn: func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
						return &awsssm.GetParametersByPathOutput{}, nil
					},
				}
			},
			wantErr: "parsing private key",
		},
		{
			name: "SSM GetParametersByPath fails (public keys)",
			smSetup: func() *stubSMClient {
				return &stubSMClient{
					getSecretValueFn: func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
						return &secretsmanager.GetSecretValueOutput{
							SecretString: aws.String(validPrivPEM),
						}, nil
					},
				}
			},
			ssmSetup: func() *stubSSMClient {
				return &stubSSMClient{
					getParameterFn: func(_ context.Context, _ *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
						return &awsssm.GetParameterOutput{
							Parameter: &ssmtypes.Parameter{
								Name:  aws.String(ssmCurrentKeyIDPath),
								Value: aws.String("key-1"),
							},
						}, nil
					},
					getParametersByPathFn: func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
						return nil, fmt.Errorf("ssm path unavailable")
					},
				}
			},
			wantErr: "loading public keys from SSM",
		},
		{
			name: "invalid public key PEM in SSM",
			smSetup: func() *stubSMClient {
				return &stubSMClient{
					getSecretValueFn: func(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
						return &secretsmanager.GetSecretValueOutput{
							SecretString: aws.String(validPrivPEM),
						}, nil
					},
				}
			},
			ssmSetup: func() *stubSSMClient {
				return &stubSSMClient{
					getParameterFn: func(_ context.Context, _ *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
						return &awsssm.GetParameterOutput{
							Parameter: &ssmtypes.Parameter{
								Name:  aws.String(ssmCurrentKeyIDPath),
								Value: aws.String("key-1"),
							},
						}, nil
					},
					getParametersByPathFn: func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
						return &awsssm.GetParametersByPathOutput{
							Parameters: []ssmtypes.Parameter{
								{
									Name:  aws.String(ssmPublicKeysPathPrefix + "bad-key"),
									Value: aws.String("not-a-pem"),
								},
							},
						}, nil
					},
				}
			},
			wantErr: "parsing public key for kid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))

			// Act
			ks, err := NewAWSKeyStore(context.Background(), tt.smSetup(), tt.ssmSetup(), clock)

			// Assert
			require.Error(t, err)
			assert.Nil(t, ks)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAWSKeyStore_SigningKey(t *testing.T) {
	t.Run("returns cached private key and key ID", func(t *testing.T) {
		// Arrange
		expectedKey, privPEM, pubPEM := testKeyPair(t)
		keyID := "signing-key-001"
		sm, ssmStub := newValidStubs(t, keyID, privPEM, pubPEM)
		clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
		ks, err := NewAWSKeyStore(context.Background(), sm, ssmStub, clock)
		require.NoError(t, err)

		// Act
		pk, kid, err := ks.SigningKey()

		// Assert
		require.NoError(t, err)
		assert.Equal(t, keyID, kid)
		assert.True(t, expectedKey.Equal(pk))
	})
}

func TestAWSKeyStore_PublicKey(t *testing.T) {
	t.Run("found in cache returns immediately", func(t *testing.T) {
		// Arrange
		expectedKey, privPEM, pubPEM := testKeyPair(t)
		keyID := "pub-key-001"
		sm, ssmStub := newValidStubs(t, keyID, privPEM, pubPEM)
		clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
		ks, err := NewAWSKeyStore(context.Background(), sm, ssmStub, clock)
		require.NoError(t, err)

		initialCallCount := ssmStub.getParametersByPathCallCount

		// Act
		pk, err := ks.PublicKey(keyID)

		// Assert
		require.NoError(t, err)
		assert.True(t, expectedKey.PublicKey.Equal(pk))
		// No additional SSM calls should have been made.
		assert.Equal(t, initialCallCount, ssmStub.getParametersByPathCallCount)
	})

	t.Run("unknown kid with cooldown expired triggers SSM refresh", func(t *testing.T) {
		// Arrange
		_, privPEM, pubPEM := testKeyPair(t)
		newKey, _, newPubPEM := testKeyPair(t)
		keyID := "key-original"
		newKeyID := "key-rotated"
		sm, ssmStub := newValidStubs(t, keyID, privPEM, pubPEM)
		clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
		ks, err := NewAWSKeyStore(context.Background(), sm, ssmStub, clock)
		require.NoError(t, err)

		// Advance clock past the kid cooldown (30s).
		clock.Advance(31 * time.Second)

		// Update stub to return the new key on next refresh.
		ssmStub.getParametersByPathFn = func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
			return &awsssm.GetParametersByPathOutput{
				Parameters: []ssmtypes.Parameter{
					{
						Name:  aws.String(ssmPublicKeysPathPrefix + keyID),
						Value: aws.String(pubPEM),
					},
					{
						Name:  aws.String(ssmPublicKeysPathPrefix + newKeyID),
						Value: aws.String(newPubPEM),
					},
				},
			}, nil
		}

		// Act
		pk, err := ks.PublicKey(newKeyID)

		// Assert
		require.NoError(t, err)
		assert.True(t, newKey.PublicKey.Equal(pk))
	})

	t.Run("unknown kid within cooldown returns error without SSM call", func(t *testing.T) {
		// Arrange
		_, privPEM, pubPEM := testKeyPair(t)
		keyID := "key-001"
		sm, ssmStub := newValidStubs(t, keyID, privPEM, pubPEM)
		clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
		ks, err := NewAWSKeyStore(context.Background(), sm, ssmStub, clock)
		require.NoError(t, err)

		// Advance past cooldown and trigger a refresh for an unknown kid to set the cooldown.
		clock.Advance(31 * time.Second)
		_, _ = ks.PublicKey("nonexistent-kid-1")

		callCountAfterFirstRefresh := ssmStub.getParametersByPathCallCount

		// Advance by less than cooldown (only 10s, cooldown is 30s).
		clock.Advance(10 * time.Second)

		// Act
		pk, err := ks.PublicKey("nonexistent-kid-2")

		// Assert
		require.Error(t, err)
		assert.Nil(t, pk)
		assert.Contains(t, err.Error(), "cooldown active")
		// No additional SSM calls should have been made.
		assert.Equal(t, callCountAfterFirstRefresh, ssmStub.getParametersByPathCallCount)
	})

	t.Run("cache TTL expired triggers refresh", func(t *testing.T) {
		// Arrange
		_, privPEM, pubPEM := testKeyPair(t)
		newKey, _, newPubPEM := testKeyPair(t)
		keyID := "key-001"
		newKeyID := "key-002"
		sm, ssmStub := newValidStubs(t, keyID, privPEM, pubPEM)
		clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
		ks, err := NewAWSKeyStore(context.Background(), sm, ssmStub, clock)
		require.NoError(t, err)

		initialCallCount := ssmStub.getParametersByPathCallCount

		// Advance clock past cache TTL (300s).
		clock.Advance(301 * time.Second)

		// Update stub to include a new key.
		ssmStub.getParametersByPathFn = func(_ context.Context, _ *awsssm.GetParametersByPathInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersByPathOutput, error) {
			return &awsssm.GetParametersByPathOutput{
				Parameters: []ssmtypes.Parameter{
					{
						Name:  aws.String(ssmPublicKeysPathPrefix + keyID),
						Value: aws.String(pubPEM),
					},
					{
						Name:  aws.String(ssmPublicKeysPathPrefix + newKeyID),
						Value: aws.String(newPubPEM),
					},
				},
			}, nil
		}

		// Act — request existing key, but cache is expired so it triggers a refresh.
		pk, err := ks.PublicKey(keyID)

		// Assert
		require.NoError(t, err)
		assert.NotNil(t, pk)
		// SSM was called again for refresh.
		assert.Greater(t, ssmStub.getParametersByPathCallCount, initialCallCount)

		// The new key should now be available after the refresh.
		pk2, err := ks.PublicKey(newKeyID)
		require.NoError(t, err)
		assert.True(t, newKey.PublicKey.Equal(pk2))
	})

	t.Run("unknown kid after refresh returns error", func(t *testing.T) {
		// Arrange
		_, privPEM, pubPEM := testKeyPair(t)
		keyID := "key-001"
		sm, ssmStub := newValidStubs(t, keyID, privPEM, pubPEM)
		clock := domaintest.NewFakeClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
		ks, err := NewAWSKeyStore(context.Background(), sm, ssmStub, clock)
		require.NoError(t, err)

		// Advance past cooldown.
		clock.Advance(31 * time.Second)

		// Act
		pk, err := ks.PublicKey("totally-nonexistent")

		// Assert
		require.Error(t, err)
		assert.Nil(t, pk)
		assert.Contains(t, err.Error(), `unknown key ID "totally-nonexistent"`)
	})
}

func TestParseRSAPrivateKey(t *testing.T) {
	t.Run("PKCS1 format", func(t *testing.T) {
		// Arrange
		expectedKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		privBytes := x509.MarshalPKCS1PrivateKey(expectedKey)
		pemStr := string(pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: privBytes,
		}))

		// Act
		key, err := parseRSAPrivateKey(pemStr)

		// Assert
		require.NoError(t, err)
		assert.True(t, expectedKey.Equal(key))
	})

	t.Run("PKCS8 format", func(t *testing.T) {
		// Arrange
		expectedKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		privBytes, err := x509.MarshalPKCS8PrivateKey(expectedKey)
		require.NoError(t, err)
		pemStr := string(pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privBytes,
		}))

		// Act
		key, err := parseRSAPrivateKey(pemStr)

		// Assert
		require.NoError(t, err)
		assert.True(t, expectedKey.Equal(key))
	})

	t.Run("no PEM block", func(t *testing.T) {
		// Act
		key, err := parseRSAPrivateKey("not-pem-data")

		// Assert
		require.Error(t, err)
		assert.Nil(t, key)
		assert.Contains(t, err.Error(), "no PEM block found")
	})

	t.Run("corrupted PEM data", func(t *testing.T) {
		// Arrange
		pemStr := string(pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: []byte("corrupted-data"),
		}))

		// Act
		key, err := parseRSAPrivateKey(pemStr)

		// Assert
		require.Error(t, err)
		assert.Nil(t, key)
		assert.Contains(t, err.Error(), "parsing PKCS#1 private key")
	})
}

func TestParseRSAPublicKey(t *testing.T) {
	t.Run("valid PKIX format", func(t *testing.T) {
		// Arrange
		privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
		require.NoError(t, err)
		pemStr := string(pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubBytes,
		}))

		// Act
		key, err := parseRSAPublicKey(pemStr)

		// Assert
		require.NoError(t, err)
		assert.True(t, privateKey.PublicKey.Equal(key))
	})

	t.Run("no PEM block", func(t *testing.T) {
		// Act
		key, err := parseRSAPublicKey("not-pem-data")

		// Assert
		require.Error(t, err)
		assert.Nil(t, key)
		assert.Contains(t, err.Error(), "no PEM block found")
	})

	t.Run("corrupted PEM data", func(t *testing.T) {
		// Arrange
		pemStr := string(pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: []byte("corrupted-data"),
		}))

		// Act
		key, err := parseRSAPublicKey(pemStr)

		// Assert
		require.Error(t, err)
		assert.Nil(t, key)
		assert.Contains(t, err.Error(), "parsing PKIX public key")
	})
}
