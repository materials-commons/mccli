// Package up implements the mc2 up command.
package up

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/materials-commons/mccli/pkg/di"
)

// Options contains user-facing mc2 up command options.
type Options struct {
	// WorkingDir is the directory used to resolve relative path arguments and to
	// discover the current Materials Commons project.
	WorkingDir string

	// Paths are file or directory paths to upload.
	Paths []string

	// Recursive controls whether directory contents are uploaded recursively.
	Recursive bool

	// WebSocketURL is the Materials Commons websocket endpoint.
	WebSocketURL string

	// Out receives command output. If nil, os.Stdout is used.
	Out io.Writer
}

// Runner executes up with injected dependencies.
type Runner struct {
	Deps di.Dependencies
}

// Run executes mc2 up using production dependencies.
func Run(ctx context.Context, opts Options) error {
	return Runner{Deps: di.Production()}.Run(ctx, opts)
}

// Run executes the up command.
func (r Runner) Run(ctx context.Context, opts Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	opts = normalizeOptions(opts)
	if len(opts.Paths) == 0 {
		return fmt.Errorf("at least one path is required")
	}

	uploader, err := newUploader(ctx, r.Deps, opts)
	if err != nil {
		return err
	}

	return uploader.upload(ctx)
}

func normalizeOptions(opts Options) Options {
	if opts.WorkingDir == "" {
		opts.WorkingDir = "."
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	return opts
}

func resolveInputPath(workingDir, inputPath string) (string, error) {
	if inputPath == "" {
		inputPath = "."
	}

	if filepath.IsAbs(inputPath) {
		return filepath.Clean(inputPath), nil
	}

	return filepath.Abs(filepath.Join(workingDir, inputPath))
}
