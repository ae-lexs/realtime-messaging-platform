// Package main is the entrypoint for the Fanout service.
// Fanout consumes from Kafka and delivers messages to connected clients via Gateway.
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
		Name:           "fanout",
		PortFromConfig: func(cfg *config.Config) int { return cfg.Fanout.HTTPPort },
	}, nil)
}
