// Package main is the entrypoint for the Chat Management service.
//
// M0.1 (toolchain reset) reduces ChatMgmt to a health-only skeleton: the
// substrate-neutral auth logic (internal/auth, internal/chatmgmt/app|port) is
// retained and unit-tested but no longer wired to a running server. The
// composition root returns with the Firestore auth re-home in M1.2, which
// re-registers the AuthService gRPC + grpc-gateway handlers.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aelexs/realtime-messaging-platform/internal/config"
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
		Name:           "chatmgmt",
		PortFromConfig: func(cfg *config.Config) int { return cfg.ChatMgmt.HTTPPort },
	}, server.Listeners{})
}
