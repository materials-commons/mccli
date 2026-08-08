package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

const (
	// DefaultLevelName is the logging level used when the user does not provide
	// an explicit level.
	DefaultLevelName = "warn"
)

type contextKey struct{}

// Config describes user-configurable logging behavior.
type Config struct {
	// Level is the minimum log level to emit. Supported values are:
	// debug, info, warn, and error.
	Level string

	// File is the optional path to a log file. If empty, logs are written to
	// stderr.
	File string
}

// Logger returns the logger stored in ctx.
//
// If ctx does not contain an mc2 logger, Logger returns slog.Default(). This
// fallback keeps package code safe even in tests or future callers that have
// not configured logging yet.
func Logger(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(contextKey{}).(*slog.Logger)
	if !ok || logger == nil {
		return slog.Default()
	}

	return logger
}

// WithLogger returns a child context containing logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		logger = slog.Default()
	}

	return context.WithValue(ctx, contextKey{}, logger)
}

// Configure creates a logger from cfg and returns a context containing it.
//
// If cfg.File is empty, logs are written to stderr. If cfg.File is set, the file
// is opened in append mode and the caller is responsible for closing the
// returned cleanup function.
func Configure(ctx context.Context, cfg Config) (context.Context, func() error, error) {
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		return ctx, nil, err
	}

	var output io.Writer = os.Stderr
	cleanup := func() error { return nil }

	if cfg.File != "" {
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return ctx, nil, fmt.Errorf("open log file %q: %w", cfg.File, err)
		}

		output = file
		cleanup = file.Close
	}

	logger := NewLogger(output, level)
	slog.SetDefault(logger)

	return WithLogger(ctx, logger), cleanup, nil
}

// NewLogger constructs an mc2 logger that writes structured text logs.
func NewLogger(output io.Writer, level slog.Leveler) *slog.Logger {
	if output == nil {
		output = io.Discard
	}

	handler := slog.NewTextHandler(output, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler)
}

// ParseLevel converts a user-provided logging level into a slog.Level.
func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "":
		return slog.LevelWarn, nil
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelWarn, fmt.Errorf("invalid log level %q: expected debug, info, warn, or error", level)
	}
}
