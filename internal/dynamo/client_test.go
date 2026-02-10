package dynamo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

func TestNewClientWithEndpoint(t *testing.T) {
	ctx := context.Background()

	client, err := dynamo.NewClient(ctx, dynamo.Config{
		Endpoint: "http://localhost:4566",
		Region:   "us-east-2",
		Timeout:  5 * time.Second,
	})

	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, client.DB)
}

func TestNewClientWithDefaultEndpoint(t *testing.T) {
	ctx := context.Background()

	client, err := dynamo.NewClient(ctx, dynamo.Config{
		Region:  "us-east-2",
		Timeout: 5 * time.Second,
	})

	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, client.DB)
}
