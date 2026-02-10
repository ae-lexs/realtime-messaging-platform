package redis_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	iredis "github.com/aelexs/realtime-messaging-platform/internal/redis"
)

func TestNewClient(t *testing.T) {
	mr := miniredis.RunT(t)

	cfg := iredis.Config{
		Addr:         mr.Addr(),
		Password:     "",
		DB:           0,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	client := iredis.NewClient(cfg)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	require.NotNil(t, client, "NewClient must return a non-nil client")
	require.NotNil(t, client.RDB, "client.RDB must be non-nil")

	// Verify that RDB satisfies the Cmdable interface.
	var _ iredis.Cmdable = client.RDB
}
