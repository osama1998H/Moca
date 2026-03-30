package observe

import (
	"context"
	"log/slog"
	"os"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

const loggerKey contextKey = 0

// NewLogger creates a new JSON structured logger writing to stdout.
// Use slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, or slog.LevelError.
func NewLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}

// WithSite returns a logger with the "site" attribute attached.
// Used to add tenant context to all log entries within a site's request scope.
func WithSite(logger *slog.Logger, site string) *slog.Logger {
	return logger.With(slog.String("site", site))
}

// WithRequest returns a logger with "request_id" and "user" attributes attached.
// Call this at the start of each HTTP request handler.
func WithRequest(logger *slog.Logger, requestID, user string) *slog.Logger {
	return logger.With(
		slog.String("request_id", requestID),
		slog.String("user", user),
	)
}

// ContextWithLogger stores the logger in ctx and returns the updated context.
// Retrieve it later with LoggerFromContext.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFromContext retrieves the logger stored by ContextWithLogger.
// If no logger is found, it returns the default slog logger (Info level, JSON to stdout).
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return NewLogger(slog.LevelInfo)
}
