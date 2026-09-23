package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/filedb"
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
		// The remote called returned an error, lets check if its not found.
		if reconcile.IsRemoteNotFound(err) {
			// The file doesn't exist on the remote. We might remove it depending on the
			// flags the user specified.
			return r.possiblyRemoveLocalOnlyPath(ctx, remotePath)
		}

		// The error wasn't not found. Since we don't know the state of the remote file, we can't remove it.
		// TODO: What about if the force flag is set?
		return fmt.Errorf("get remote file by path %q: %w", remotePath, err)
	}

	// If we are here, then we found a remote file.

	// Convert the remote file into a reconciler remote entry.
	remoteEntry, err := reconcile.RemoteEntryFromMCFile(remoteFile)
	if err != nil {
		return err
	}

	if remoteEntry == nil {
		// The remote entry is nil, and no error. Candidate for removal depending on flags.
		return r.possiblyRemoveLocalOnlyPath(ctx, remotePath)
	}

	if remoteEntry.Kind == reconcile.KindDir {
		return r.removeDirectory(ctx, remoteEntry.Path)
	}

	return r.removeFile(ctx, reconcile.Observation{
		RemotePath:  remoteEntry.Path,
		Name:        remoteEntry.Name,
		Dir:         remoteEntry.Dir,
		RemoteEntry: remoteEntry,
	})
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

func (r *remover) removeDirectory(ctx context.Context, remoteDir string) error {
	if !r.opts.Recursive {
		return fmt.Errorf("%s is a directory; use --recursive to remove directory", remoteDir)
	}

	// Construct a local and remote directory walker.

	// First create the local list dir func
	localListDirFunc := reconcile.MakeLocalNodeListDirFunc(r.translator, nil)

	// Then create the remote list dir func
	remoteListDirFunc := reconcile.MakeRemoteOnlyListDirFunc(r.project.ProjectID, r.translator, r.remoteGetter)

	// Finally created the merged local/remote list dir func
	mergedListDirFunc := reconcile.MakeMergedNodeListDirFunc(r.translator, localListDirFunc, remoteListDirFunc)

	walkOptions := reconcile.WalkOptions{
		Recursive:  true,
		Translator: r.translator,
	}

	return reconcile.WalkNodesAndReconcile(ctx, reconcile.WalkNode{RemotePath: remoteDir}, mergedListDirFunc, r.store,
		r.reconciler, walkOptions, func(ctx context.Context, node reconcile.WalkNode, states map[string]reconcile.FileState) error {
			for _, state := range states {
				if !stateIsFile(state) {
					continue
				}

				if err := r.removeFileFromState(ctx, state); err != nil {
					return err
				}
			}

			return nil
		})
}

func (r *remover) removeFileFromState(ctx context.Context, state reconcile.FileState) error {
	if !shouldRemoveFile(r.opts, state) {
		_, _ = fmt.Fprintf(r.opts.Out, "Skipping %s - %s\n", state.Observation.RemotePath, state.Decision.Reason)
		return nil
	}

	remotePath := state.Observation.RemotePath
	localPath, err := r.translator.RemoteToLocal(remotePath)
	if err != nil {
		return fmt.Errorf("failed to translate remote path %s to local path: %w", remotePath, err)
	}

	if r.opts.DryRun {
		fmt.Fprintf(r.opts.Out, "Would remove %s\n", remotePath)
		return nil
	}

	if r.opts.LocalOnly {
		if err := r.removeLocalFile(localPath); err != nil {
			return err
		}
	}

	if r.opts.RemoteOnly {
		if err := r.removeRemoteFile(ctx, state); err != nil {
			return err
		}
	}

	if err := r.store.DeleteByPath(ctx, remotePath); err != nil && errors.Is(err, filedb.ErrRecordNotFound) {
		return err
	}

	fmt.Fprintf(r.opts.Out, "Removed %s\n", remotePath)
	return nil
}

func (r *remover) removeRemoteFile(ctx context.Context, state reconcile.FileState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	switch {
	case state.Observation.RemoteEntry == nil:
		// Remote file doesn't exist, nothing to do
		return nil

	case state.Observation.RemoteEntry.RemoteFileID == nil:
		// Remote file exists, but for some reason there is no id...
		return fmt.Errorf("cannot remove %q: remote file id is missing", state.Observation.RemotePath)

	default:
		// Delete the remote file
		fileID := int(*state.Observation.RemoteEntry.RemoteFileID)
		if err := r.remoteFileDeleter.DeleteFile(r.project.ProjectID, fileID); err != nil {
			return fmt.Errorf("remove remote file %q: %w", state.Observation.RemotePath, err)
		}

		return nil
	}
}

func (r *remover) removeFile(ctx context.Context, observation reconcile.Observation) error {
	return nil
}

func (r *remover) removeLocalFile(path string) error {
	return nil
}

func shouldRemoveFile(opts rmOpts, state reconcile.FileState) bool {
	if opts.Force {
		// Force flag so skip all checks and return true
		return true
	}

	switch state.Decision.Action {
	case reconcile.ActionDownload:
		// If the action is Download, then we know that the file has been previously seen and we can delete it.
		return true
	case reconcile.ActionConflict, reconcile.ActionUpload, reconcile.ActionDBUpdate:
		// If the action is Conflict, Upload, or DBUpdate, then we know that the file has not been previously seen,
		// or we aren't sure on the state. So don't delete it.
		return false
	case reconcile.ActionSkip:
		// If we are skipping the file, then there isn't anything to do to it. Right now that means only delete
		// it if there is a remote entry. If there isn't a remote entry, then this is a file we haven't seen
		// before. That case should be covered by ActionUpload, but just in case we return false if the remote
		// entry doesn't exist.
		return state.Observation.RemoteEntry != nil
	default:
		// The default case is don't delete. All actions should have been covered by previous checks. This covers
		// the case where a new action was added, and this method hasn't been updated to handle it.
		return false
	}
}

func stateIsFile(state reconcile.FileState) bool {
	switch {
	case state.Observation.LocalEntry != nil && state.Observation.LocalEntry.Kind == reconcile.KindFile:
		// Local entry exists and is a file
		return true

	case state.Observation.RemoteEntry != nil && state.Observation.RemoteEntry.Kind == reconcile.KindFile:
		// Remote entry exists and is a file
		return true

	default:
		// Neither entry exists or is a file
		return false
	}
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
