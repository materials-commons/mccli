package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      slog.Level
		wantError bool
	}{
		{name: "empty defaults to warn", input: "", want: slog.LevelWarn},
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "warning alias", input: "warning", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "case insensitive", input: "DEBUG", want: slog.LevelDebug},
		{name: "invalid", input: "verbose", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLevel(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatal("ParseLevel() error = nil, want error")
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseLevel() error = %v, want nil", err)
			}

			if got != tt.want {
				t.Fatalf("ParseLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithLoggerAndLogger(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelDebug)

	ctx := WithLogger(context.Background(), logger)

	Logger(ctx).Info("hello", "component", "test")

	got := output.String()
	if !strings.Contains(got, "hello") {
		t.Fatalf("log output missing message: %q", got)
	}
	if !strings.Contains(got, "component=test") {
		t.Fatalf("log output missing attribute: %q", got)
	}
}

func TestNewLoggerFiltersByLevel(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelWarn)

	logger.Info("hidden")
	logger.Warn("visible")

	got := output.String()
	if strings.Contains(got, "hidden") {
		t.Fatalf("info log should have been filtered out: %q", got)
	}
	if !strings.Contains(got, "visible") {
		t.Fatalf("warn log should have been written: %q", got)
	}
}

func TestConfigureWritesToFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "mc2.log")

	ctx, cleanup, err := Configure(context.Background(), Config{
		Level: "info",
		File:  logPath,
	})
	if err != nil {
		t.Fatalf("Configure() error = %v, want nil", err)
	}

	Logger(ctx).Info("file log test")

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v, want nil", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v, want nil", err)
	}

	if !strings.Contains(string(data), "file log test") {
		t.Fatalf("log file missing message: %q", string(data))
	}
}
