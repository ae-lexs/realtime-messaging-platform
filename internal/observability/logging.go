package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// LogConfig holds configuration for the structured logger.
type LogConfig struct {
	Level       string // "debug", "info", "warn", "error"
	Format      string // "json" or "text"
	ServiceName string
	Environment string
}

// sensitivePatterns contains field name patterns that should be redacted.
// These patterns are matched case-insensitively against attribute keys.
var sensitivePatterns = []string{
	"_key",
	"_secret",
	"_token",
	"_password",
	"_pepper",
	"_credential",
	"authorization",
	"bearer",
	"api_key",
	"apikey",
	"secret",
	"password",
	"private",
}

// InitLogger creates a new structured logger with secret redaction.
// The returned logger is also set as the default via slog.SetDefault.
func InitLogger(cfg LogConfig) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactSecrets,
	}

	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// Add service context to all log entries
	logger := slog.New(handler).With(
		slog.String("service", cfg.ServiceName),
		slog.String("environment", cfg.Environment),
	)

	slog.SetDefault(logger)
	return logger
}

// NewRedactingHandler creates a slog handler that redacts sensitive fields.
// This is an alternative to InitLogger for custom handler composition.
func NewRedactingHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}

	originalReplace := opts.ReplaceAttr
	opts.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
		// Apply original replacer first if present
		if originalReplace != nil {
			a = originalReplace(groups, a)
		}
		return redactSecrets(groups, a)
	}

	return slog.NewJSONHandler(w, opts)
}

// redactSecrets is a ReplaceAttr function that redacts sensitive fields.
func redactSecrets(groups []string, a slog.Attr) slog.Attr {
	keyLower := strings.ToLower(a.Key)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(keyLower, pattern) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}
	return a
}

// LoggerFromContext extracts a logger from context, or returns the default logger.
// If a trace ID is present in the context, it's added to the logger.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	logger := slog.Default()

	// Add trace ID if present
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		logger = logger.With(slog.String("trace_id", traceID))
	}

	return logger
}

// WithTraceID returns a new logger with the trace ID from context.
func WithTraceID(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		return logger.With(slog.String("trace_id", traceID))
	}
	return logger
}
