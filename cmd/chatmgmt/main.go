// Package main is the entrypoint for the Chat Management service.
// ChatMgmt handles chat lifecycle, membership, and auth endpoints.
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
		Name:               "chatmgmt",
		PortFromConfig:     func(cfg *config.Config) int { return cfg.ChatMgmt.HTTPPort },
		GRPCPortFromConfig: func(cfg *config.Config) int { return cfg.ChatMgmt.GRPCPort },
		Setup:              setup,
	}, server.Listeners{})
}
