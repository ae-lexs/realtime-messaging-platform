package redis

import (
	"time"

	"github.com/redis/go-redis/v9"
)

// Cmdable is a type alias for redis.Cmdable. Adapters accept this interface
// instead of importing go-redis directly, keeping the library confined to
// internal/redis/ per depguard rules.
type Cmdable = redis.Cmdable

// Config holds the parameters needed to connect to a Redis instance.
type Config struct {
	Addr         string
	Password     string
	DB           int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Client wraps a go-redis client. The RDB field satisfies the Cmdable
// interface and is the handle adapters use for Redis operations.
type Client struct {
	RDB *redis.Client
}

// NewClient creates a new Redis client configured from cfg.
func NewClient(cfg Config) *Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	return &Client{RDB: rdb}
}

// Close releases the underlying Redis connection.
func (c *Client) Close() error {
	return c.RDB.Close()
}
