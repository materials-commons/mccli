package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/materials-commons/mccli/internal/mc/file"
)

func TestRmRunnerRunRejectsNoArgsBeforeCreatingRemover(t *testing.T) {
	err := rmRunner{}.run(context.Background(), file.RemoverOpts{}, nil)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "at least one path argument") {
		t.Fatalf("run() error = %v, want missing path error", err)
	}
}

func TestRmRunnerRunRejectsRemoteOnlyAndLocalOnly(t *testing.T) {
	err := rmRunner{}.run(context.Background(), file.RemoverOpts{
		RemoteOnly: true,
		LocalOnly:  true,
	}, []string{"file.txt"})

	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "cannot specify both --remote-only and --local-only") {
		t.Fatalf("run() error = %v, want conflicting flags error", err)
	}
}

func TestRmRunnerRunReturnsCancelledContextBeforeValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rmRunner{}.run(ctx, file.RemoverOpts{}, []string{"file.txt"})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run() error = %v, want context.Canceled", err)
	}
}

func TestRmRunnerNormalizeOptionsDefaults(t *testing.T) {
	got := rmRunner{}.normalizeOptions(file.RemoverOpts{})

	if got.WorkingDir != "." {
		t.Fatalf("WorkingDir = %q, want .", got.WorkingDir)
	}
	if got.Out == nil {
		t.Fatal("Out = nil, want default writer")
	}
}
