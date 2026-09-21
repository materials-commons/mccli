package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/reconcile"
	"github.com/materials-commons/mccli/internal/services"
	"github.com/urfave/cli/v3"
)

func rmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Usage:     "Remove files and directories locally and remotely",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "recursive",
				Aliases: []string{"r"},
				Usage:   "Remove remote directories recursively",
			},
			&cli.BoolFlag{
				Name:  "remote-only",
				Usage: "Remove files only on the Materials Commons server",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Force removal of files and directories",
			},
			&cli.BoolFlag{
				Name:  "local-only",
				Usage: "Remove files only locally",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Show what would be removed without actually removing anything",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			workingDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}
			opts := rmOpts{
				WorkingDir: workingDir,
				Recursive:  cmd.Bool("recursive"),
				RemoteOnly: cmd.Bool("remote-only"),
				LocalOnly:  cmd.Bool("local-only"),
				Force:      cmd.Bool("force"),
				DryRun:     cmd.Bool("dry-run"),
				Out:        os.Stdout,
			}
			return runRmCmd(ctx, opts, cmd.Args().Slice())
		},
	}
}

type rmOpts struct {
	WorkingDir string
	Recursive  bool
	RemoteOnly bool
	LocalOnly  bool
	Force      bool
	DryRun     bool
	Out        io.Writer
}

type rmRunner struct {
	deps di.Dependencies
}

func runRmCmd(ctx context.Context, opts rmOpts, args []string) error {
	return rmRunner{deps: di.Production()}.run(ctx, opts, args)
}

func (r rmRunner) run(ctx context.Context, opts rmOpts, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(args) == 0 {
		return fmt.Errorf("at least one path argument is required")
	}

	opts = r.normalizeOptions(opts)

	if opts.RemoteOnly && opts.LocalOnly {
		return fmt.Errorf("cannot specify both --remote-only and --local-only")
	}

	remover, err := newRemover(r.deps, ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to create remover: %w", err)
	}

	for _, arg := range args {
		remotePath, err := remover.normalizeRemotePath(arg)
		if err != nil {
			// TODO: should we return an error here or log and continue?
			return err
		}

		if err := remover.removePath(ctx, remotePath); err != nil {
			// TODO: should we return an error here or log and continue?
			return err
		}
	}

	return nil
}

func (r rmRunner) normalizeOptions(opts rmOpts) rmOpts {
	if opts.WorkingDir == "" {
		opts.WorkingDir = "."
	}

	if opts.Out == nil {
		opts.Out = os.Stdout
	}

	return opts
}

type remover struct {
	opts              rmOpts
	project           config.Project
	store             di.Store
	remoteGetter      mc.FileDirectoryGetter
	remoteFileDeleter mc.FileDeleter
	translator        mc.ProjectPathTranslator
	reconciler        *reconcile.Reconciler
}

func newRemover(deps di.Dependencies, ctx context.Context, opts rmOpts) (*remover, error) {
	var (
		r   remover
		err error
	)

	r.opts = opts

	// The mode for the reconciler is set to download. This ensures that any file we are looking to remove
	// has already been uploaded. That way we aren't accidentally deleting files that the user may not realize
	// haven't been uploaded. That is we want to safely delete files, not delete files the user created but
	// hasn't done anything with.
	r.reconciler = reconcile.New(reconcile.ModeDownload)

	container := services.NewContainer(deps)
	defer func() {
		_ = container.Close(context.Background())
	}()

	cmdCtx, err := container.LoadCommandContext(ctx, opts.WorkingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load command context: %w", err)
	}
	r.project = cmdCtx.Project

	if r.store, err = container.Store(ctx); err != nil {
		return nil, fmt.Errorf("failed to load store: %w", err)
	}

	if r.remoteGetter, r.remoteFileDeleter, err = getRemoverRemotes(container); err != nil {
		return nil, fmt.Errorf("failed to load remote removers: %w", err)
	}

	if r.translator, err = container.Translator(); err != nil {
		return nil, fmt.Errorf("failed to load translator: %w", err)
	}

	return &r, nil
}

// normalizeRemotePath takes a path string and normalizes it to a remote path. It performs other
// cleanups such as conversion of "\" to "/", and taking care of relative paths in the path string.
// Finally, it returns a remote project path.
func (r *remover) normalizeRemotePath(path string) (string, error) {
	if path == "" {
		path = "/"
	}

	// Convert windows paths to unix paths, clean up multiple slashes in a row.
	inputPath := filepath.ToSlash(path)
	if !strings.HasPrefix(inputPath, "/") {
		inputPath = "/" + inputPath
	}

	// Take care of relative paths in the path string.
	cleanedPath := filepath.Clean(inputPath)
	if cleanedPath == "." {
		cleanedPath = "/"
	}

	return mc.NormalizeRemoteProjectPath(cleanedPath)
}

func (r *remover) removePath(ctx context.Context, remotePath string) error {
	remoteFile, err := r.remoteGetter.GetFileByPath(r.project.ProjectID, remotePath)
	if err != nil {
		if reconcile.IsRemoteNotFound(err) {
			return r.possiblyRemoveLocalOnlyPath(ctx, remotePath)
		}
		return fmt.Errorf("get remote file by path %q: %w", remotePath, err)
	}

	// Continue here
	_ = remoteFile

	return nil
}

func (r *remover) possiblyRemoveLocalOnlyPath(ctx context.Context, remotePath string) error {
	localPath, err := r.translator.RemoteToLocal(remotePath)
	if err != nil {
		return fmt.Errorf("translate remote path %q: %w", remotePath, err)
	}

	localEntry, err := reconcile.ObserveLocal(ctx, r.translator, localPath, time.Now())
	switch {
	case err != nil:
		return fmt.Errorf("observe local path %q: %w", localPath, err)

	case localEntry == nil:
		_, _ = fmt.Fprintf(r.opts.Out, "%s: No such file or directory\n", remotePath)
		return nil
	}

	return nil
}

func getRemoverRemotes(container *services.Container) (mc.FileDirectoryGetter, mc.FileDeleter, error) {
	remoteAny, err := container.Remote()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load remote: %w", err)
	}

	remoteGetter, ok := remoteAny.(mc.FileDirectoryGetter)
	if !ok {
		return nil, nil, fmt.Errorf("remote does not implement FileDirectoryGetter")
	}

	remoteRemover, ok := remoteAny.(mc.FileDeleter)
	if !ok {
		return nil, nil, fmt.Errorf("remote does not implement FileDeleter")
	}

	return remoteGetter, remoteRemover, nil
}
