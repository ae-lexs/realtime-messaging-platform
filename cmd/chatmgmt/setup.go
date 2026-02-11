package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"log/slog"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/adapter"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/port"
	"github.com/aelexs/realtime-messaging-platform/internal/config"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
	"github.com/aelexs/realtime-messaging-platform/internal/redis"
	"github.com/aelexs/realtime-messaging-platform/internal/server"
)

// Table names match the LocalStack init script (scripts/localstack-init.sh).
const (
	otpRequestsTable = "otp_requests"
	usersTable       = "users"
	sessionsTable    = "sessions"
)

// JWT issuer/audience match the domain convention.
const (
	jwtIssuer   = "messaging-platform"
	jwtAudience = "messaging-api"
)

// devPepper is the HMAC pepper used in local development.
// Production uses a value from AWS Secrets Manager (TF-1 follow-up).
var devPepper = []byte("local-dev-pepper-32-bytes-ok!!")

// setup is the chatmgmt service composition root. It creates infrastructure
// clients, adapters, the auth service, and registers gRPC + grpc-gateway handlers.
func setup(ctx context.Context, deps server.SetupDeps) (func(context.Context) error, error) {
	cfg := deps.Config
	logger := deps.Logger

	// 1. Infrastructure clients.
	dynamoClient, err := dynamo.NewClient(ctx, dynamo.Config{
		Endpoint: cfg.DynamoDB.Endpoint,
		Region:   cfg.AWS.Region,
		Timeout:  cfg.DynamoDB.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("chatmgmt setup: create dynamo client: %w", err)
	}

	redisClient := redis.NewClient(redis.Config{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		ReadTimeout:  cfg.Redis.Timeout,
		WriteTimeout: cfg.Redis.Timeout,
	})

	// 2. Adapters.
	clock := domain.RealClock{}
	otpStore := adapter.NewOTPStore(dynamoClient.DB, otpRequestsTable, clock)
	userStore := adapter.NewUserStore(dynamoClient.DB, usersTable)
	sessionStore := adapter.NewSessionStore(dynamoClient.DB, sessionsTable, clock)
	transactor := adapter.NewTransactor(dynamoClient.DB, otpRequestsTable, usersTable, sessionsTable)
	rateLimiter := adapter.NewRateLimiter(redisClient.RDB)
	revocationStore := adapter.NewRevocationStore(redisClient.RDB)

	// 3. Key store + SMS provider (environment-dependent).
	keyStore, err := createKeyStore(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("chatmgmt setup: create key store: %w", err)
	}

	smsProvider := createSMSProvider(cfg, logger)

	// 4. Auth core.
	minter := auth.NewMinter(auth.MinterConfig{
		KeyStore:  keyStore,
		AccessTTL: domain.AccessTokenLifetime,
		Issuer:    jwtIssuer,
		Audience:  jwtAudience,
		Clock:     clock,
	})
	validator := auth.NewValidator(auth.ValidatorConfig{
		KeyStore: keyStore,
		Issuer:   jwtIssuer,
		Audience: jwtAudience,
		Clock:    clock,
	})

	// 5. Auth service.
	authSvc := app.NewAuthService(app.AuthServiceConfig{
		OTPStore:        otpStore,
		UserStore:       userStore,
		SessionStore:    sessionStore,
		Transactor:      transactor,
		RateLimiter:     rateLimiter,
		RevocationStore: revocationStore,
		SMSProvider:     smsProvider,
		Minter:          minter,
		Validator:       validator,
		Clock:           clock,
		Pepper:          devPepper,
		Logger:          logger,
	})

	// 6. Register gRPC + grpc-gateway.
	handler := port.NewAuthHandler(authSvc)
	messagingv1.RegisterAuthServiceServer(deps.GRPCServer, handler)

	gwMux := runtime.NewServeMux()
	if err := messagingv1.RegisterAuthServiceHandlerServer(ctx, gwMux, handler); err != nil {
		return nil, fmt.Errorf("chatmgmt setup: register grpc-gateway: %w", err)
	}
	deps.HTTPMux.Handle("/", gwMux)

	logger.InfoContext(ctx, "chatmgmt auth service initialized")

	cleanup := func(_ context.Context) error {
		authSvc.Wait()
		return redisClient.Close()
	}

	return cleanup, nil
}

// createKeyStore returns the appropriate key store for the environment.
// Local: generates an ephemeral RSA key pair (no AWS dependency).
// Production: loads from AWS Secrets Manager + SSM (not yet wired — TF-1 follow-up).
func createKeyStore(cfg *config.Config, logger *slog.Logger) (auth.KeyStore, error) {
	if cfg.IsLocal() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("generate dev RSA key: %w", err)
		}
		logger.Info("using ephemeral RSA key for local development", slog.String("key_id", "dev-key-001"))
		return auth.NewStaticKeyStore(key, "dev-key-001"), nil
	}

	// TODO(TF-1): Wire AWSKeyStore for production.
	return nil, fmt.Errorf("production key store not yet implemented (TF-1)")
}

// createSMSProvider returns the appropriate SMS provider for the environment.
// Local: logs OTPs instead of sending real SMS.
// Production: uses Amazon SNS (not yet wired — TF-1 follow-up).
func createSMSProvider(cfg *config.Config, logger *slog.Logger) auth.SMSProvider {
	if cfg.IsLocal() {
		logger.Info("using log-only SMS provider for local development")
		return adapter.NewLogSMSProvider(logger)
	}

	// TODO(TF-1): Wire SNSSMSProvider for production.
	logger.Warn("production SMS provider not yet implemented, using log-only provider")
	return adapter.NewLogSMSProvider(logger)
}
