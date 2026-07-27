// Package main is the entrypoint for the Chat Management service.
//
// M1.2 restores the composition root M0.1 retired: the auth logic salvaged
// from the AWS build is wired to Firestore (identity), Memorystore (abuse
// control and revocation) and Secret Manager (signing key, OTP pepper), and
// served over both gRPC and REST.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/adapter"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/port"
	"github.com/aelexs/realtime-messaging-platform/internal/config"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
	redisclient "github.com/aelexs/realtime-messaging-platform/internal/redis"
	"github.com/aelexs/realtime-messaging-platform/internal/secrets"
	"github.com/aelexs/realtime-messaging-platform/internal/server"
)

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	return server.Run(ctx, server.Params{
		Name:               "chatmgmt",
		PortFromConfig:     func(cfg *config.Config) int { return cfg.ChatMgmt.HTTPPort },
		GRPCPortFromConfig: func(cfg *config.Config) int { return cfg.ChatMgmt.GRPCPort },
		Setup:              setup,
	}, server.Listeners{})
}

// setup builds the auth service and registers it on both transports.
//
// Everything here fails loudly. ADR-015's signing_key_required_for_startup
// invariant says Chat Mgmt must not serve traffic without a signing key and a
// pepper, and this is where that is enforced: a missing dependency aborts the
// rollout, where a readiness probe can act on it, rather than becoming a 500
// on every login.
func setup(ctx context.Context, deps server.SetupDeps) (func(context.Context) error, error) {
	cfg := deps.Config
	clock := domain.RealClock{}

	// closers accumulates what has been opened, so a failure part-way through
	// construction does not leak the clients opened before it.
	var closers []func() error
	abort := func(err error) (func(context.Context) error, error) {
		return nil, errors.Join(append([]error{err}, closeAll(closers)...)...)
	}

	fsClient, err := firestore.NewClient(ctx, firestore.Config{
		ProjectID:  cfg.Firestore.ProjectID,
		DatabaseID: cfg.Firestore.Database,
		Timeout:    cfg.Firestore.Timeout,
	})
	if err != nil {
		return abort(err)
	}
	closers = append(closers, fsClient.Close)

	secretsClient, err := secrets.NewClient(ctx, secrets.Config{
		ProjectID: cfg.Secrets.ProjectID,
		Timeout:   cfg.Secrets.Timeout,
	})
	if err != nil {
		return abort(err)
	}
	closers = append(closers, secretsClient.Close)

	redisConn := redisclient.NewClient(redisclient.Config{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		ReadTimeout:  cfg.Redis.Timeout,
		WriteTimeout: cfg.Redis.Timeout,
	})
	closers = append(closers, redisConn.Close)

	keyStore, err := auth.NewRemoteKeyStore(ctx, auth.RemoteKeyStoreConfig{
		Fetcher:         secretsClient,
		RefreshInterval: cfg.Auth.KeyRefresh,
		Logger:          deps.Logger,
	})
	if err != nil {
		return abort(err)
	}

	pepper, err := secretsClient.Latest(ctx, auth.SecretOTPPepper)
	if err != nil {
		return abort(fmt.Errorf("load OTP pepper: %w", err))
	}
	if len(pepper) == 0 {
		// An empty pepper fails nothing visibly — it quietly reduces every OTP
		// MAC to an unkeyed hash over a six-digit value, which is trivially
		// brute-forced from a leaked record (ADR-015 §1.1).
		return abort(fmt.Errorf("%s is empty: %w", auth.SecretOTPPepper, domain.ErrConfigRequired))
	}

	// The key refresher is a goroutine this function owns: started here,
	// stopped and joined in cleanup, so shutdown leaves nothing outstanding.
	refreshCtx, stopRefresh := context.WithCancel(context.Background())
	var refresherWG sync.WaitGroup
	refresherWG.Add(1)
	go func() {
		defer refresherWG.Done()
		keyStore.Run(refreshCtx)
	}()
	stopRefresher := func() {
		stopRefresh()
		refresherWG.Wait()
	}

	svc := app.NewAuthService(app.AuthServiceConfig{
		OTPStore:     adapter.NewOTPStore(firestore.NewOTPRequests(fsClient), clock),
		UserStore:    adapter.NewUserStore(firestore.NewUsers(fsClient)),
		SessionStore: adapter.NewSessionStore(firestore.NewSessions(fsClient)),
		Transactor:   adapter.NewAuthTransactor(firestore.NewAuthTx(fsClient)),

		// Both fail closed on a Redis error (ADR-013 §3.4), which is what makes
		// Memorystore a hard dependency of this service rather than a cache.
		RateLimiter:     adapter.NewRateLimiter(redisConn.RDB),
		RevocationStore: adapter.NewRevocationStore(redisConn.RDB),

		SMSProvider: adapter.NewLogSMSProvider(deps.Logger),

		Minter: auth.NewMinter(auth.MinterConfig{
			KeyStore:  keyStore,
			AccessTTL: domain.AccessTokenLifetime,
			Issuer:    cfg.Auth.Issuer,
			Audience:  cfg.Auth.Audience,
			Clock:     clock,
		}),
		Validator: auth.NewValidator(auth.ValidatorConfig{
			KeyStore: keyStore,
			Issuer:   cfg.Auth.Issuer,
			Audience: cfg.Auth.Audience,
			Clock:    clock,
		}),

		Clock:  clock,
		Pepper: pepper,
		Logger: deps.Logger,
	})

	handler := port.NewAuthHandler(svc)
	messagingv1.RegisterAuthServiceServer(deps.GRPCServer, handler)

	// The REST bridge calls the handler in-process rather than dialling this
	// service's own gRPC port: same behaviour, one fewer connection and one
	// fewer thing to fail. ServeMuxOptions is not optional — it carries the
	// header plumbing and the marshaling corrections, both of which fail
	// silently by default (see internal/chatmgmt/port/rest_options.go).
	gwMux := runtime.NewServeMux(port.ServeMuxOptions()...)
	if regErr := messagingv1.RegisterAuthServiceHandlerServer(ctx, gwMux, handler); regErr != nil {
		stopRefresher()
		return abort(fmt.Errorf("register REST handlers: %w", regErr))
	}
	deps.HTTPMux.Handle("/v1/", gwMux)

	deps.Logger.Info("auth service registered",
		"grpc_port", cfg.ChatMgmt.GRPCPort,
		"rest_prefix", "/v1/auth",
		"firestore_database", cfg.Firestore.Database,
	)

	cleanup := func(_ context.Context) error {
		stopRefresher()

		// The service owns a background goroutine per in-flight SMS send;
		// waiting here is the other half of that ownership contract.
		svc.Wait()

		return errors.Join(closeAll(closers)...)
	}

	return cleanup, nil
}

// closeAll closes in reverse order of opening and collects every error instead
// of stopping at the first, so one stuck client cannot leave the others open.
func closeAll(closers []func() error) []error {
	errs := make([]error, 0, len(closers))
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
