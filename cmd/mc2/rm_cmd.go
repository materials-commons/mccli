package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	inputPath := strings.ReplaceAll(path, `\`, "/")
	inputPath = filepath.ToSlash(inputPath)
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
		// The remote called returned an error, let's check if it's not found.
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
		return r.removeDirectoryWithRootEntry(ctx, remoteEntry)
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

	fileRecord, hasRecord, err := r.loadFileRecordByPathFromDB(ctx, remotePath)
	if err != nil {
		return fmt.Errorf("load file record by path %q: %w", remotePath, err)
	}

	observation := reconcile.Observation{
		RemotePath: remotePath,
		Name:       filepath.Base(remotePath),
		Dir:        filepath.Dir(remotePath),
		LocalEntry: localEntry,
	}

	if observation.Dir == "." {
		observation.Dir = "/"
	}

	if hasRecord {
		observation.FileRecord = &fileRecord
	}

	decision, err := r.reconciler.Reconcile(ctx, observation)
	if err != nil {
		return fmt.Errorf("reconcile file %q: %w", observation.RemotePath, err)
	}

	return r.removeLocalOnlyReconciledFile(ctx, reconcile.FileState{
		Observation: observation,
		Decision:    decision,
	})
}

func (r *remover) removeLocalOnlyReconciledFile(ctx context.Context, fileState reconcile.FileState) error {
	// Check flags
	if !r.opts.LocalOnly && !r.opts.Force {
		// Skip if not local only and not force
		fmt.Fprintf(r.opts.Out, "Skipping %q - no remote file to remove\n", fileState.Observation.RemotePath)
		return nil
	}

	if fileState.Observation.LocalEntry == nil {
		// No local entry to remove
		return nil
	}

	if !shouldRemoveFile(r.opts, fileState) {
		_, _ = fmt.Fprintf(r.opts.Out, "Skipping %s - %s\n", fileState.Observation.RemotePath, fileState.Decision.Reason)
		return nil
	}

	if r.opts.DryRun {
		// Dry run. Show what would be removed
		fmt.Fprintf(r.opts.Out, "Would remove %s\n", fileState.Observation.RemotePath)
		return nil
	}

	// If we are here, then a local file exists and we can remove it
	if err := r.removeLocalPath(fileState.Observation.LocalEntry.Path, fileState.Observation.LocalEntry.Kind); err != nil {
		// failed to remove the file (permissions issue?)
		return fmt.Errorf("remove local path %q: %w", fileState.Observation.LocalEntry.Path, err)
	}

	// Remove the file record from the database
	err := r.store.DeleteByPath(ctx, fileState.Observation.RemotePath)
	switch {
	case err == nil:
		// Removed file entry from database
		fmt.Fprintf(r.opts.Out, "Removed %s\n", fileState.Observation.RemotePath)
		return nil

	case errors.Is(err, filedb.ErrRecordNotFound):
		// No entry in the database. Nothing to do.
		fmt.Fprintf(r.opts.Out, "Removed %s\n", fileState.Observation.RemotePath)
		return nil

	default:
		// err != nil, and not filedb.ErrRecordNotFound
		// File deleted on disk, but the database returned an error.... Hmmm...
		return err
	}
}

func (r *remover) removeDirectory(ctx context.Context, remoteDir string) error {
	return r.removeDirectoryContents(ctx, remoteDir)
}

func (r *remover) removeDirectoryWithRootEntry(ctx context.Context, rootEntry *reconcile.RemoteEntry) error {
	if rootEntry == nil {
		return fmt.Errorf("directory remote entry is required")
	}

	if err := r.removeDirectoryContents(ctx, rootEntry.Path); err != nil {
		return err
	}

	rootState := reconcile.FileState{
		Observation: reconcile.Observation{
			RemotePath:  rootEntry.Path,
			Name:        rootEntry.Name,
			Dir:         rootEntry.Dir,
			RemoteEntry: rootEntry,
		},
		Decision: reconcile.Decision{
			Action: reconcile.ActionSkip,
			Reason: "remove requested directory",
		},
	}

	return r.removeFileFromState(ctx, rootState)
}

func (r *remover) removeDirectoryContents(ctx context.Context, remoteDir string) error {
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

	// A list of directories we've visited.
	var directoryStates []reconcile.FileState

	walkParams := reconcile.WalkNodesAndReconcileParams{
		Root:             reconcile.WalkNode{RemotePath: remoteDir},
		ListDir:          mergedListDirFunc,
		DirRecordsGetter: r.store,
		Reconciler:       r.reconciler,
		Options:          walkOptions,
		CallbackFunc: func(ctx context.Context, node reconcile.WalkNode, states map[string]reconcile.FileState) error {
			for _, state := range states {
				if stateIsDir(state) {
					// Keep track of directories so we can remove them after we've removed all files from them.
					directoryStates = append(directoryStates, state)
				}

				if !stateIsFile(state) {
					continue
				}

				if err := r.removeFileFromState(ctx, state); err != nil {
					return err
				}
			}

			return nil
		},
	}

	if err := reconcile.WalkNodesAndReconcile(ctx, walkParams); err != nil {
		return err
	}

	// Now that we've removed files from the directories, we need to remove the directories themselves.
	sort.Slice(directoryStates, func(i, j int) bool {
		return stateRemotePathDepth(directoryStates[i]) > stateRemotePathDepth(directoryStates[j])
	})

	for _, state := range directoryStates {
		if err := r.removeFileFromState(ctx, state); err != nil {
			return err
		}
	}

	// Check if we should remove the local root, and if so, do it.
	removeLocalRoot := r.opts.LocalOnly || (!r.opts.LocalOnly && !r.opts.RemoteOnly)
	if removeLocalRoot && !r.opts.DryRun {
		localPath, err := r.translator.RemoteToLocal(remoteDir)
		if err != nil {
			return fmt.Errorf("failed to translate remote directory %s to local path: %w", remoteDir, err)
		}

		if err := r.removeLocalPath(localPath, reconcile.KindDir); err != nil {
			return err
		}
	}

	return nil
}

func (r *remover) removeFile(ctx context.Context, observation reconcile.Observation) error {
	localPath, err := r.translator.RemoteToLocal(observation.RemotePath)
	if err != nil {
		return err
	}

	localEntry, err := reconcile.ObserveLocal(ctx, r.translator, localPath, time.Now())
	if err != nil {
		return err
	}

	observation.LocalEntry = localEntry
	fileRecord, hasRecord, err := r.loadFileRecordByPathFromDB(ctx, observation.RemotePath)
	if err != nil {
		return fmt.Errorf("load file record by path %q: %w", observation.RemotePath, err)
	}

	if hasRecord {
		// found a record in the database, lets add it to the observation
		observation.FileRecord = &fileRecord
	}

	// Now lets reconcile to decide what to do
	reconcileDecision, err := r.reconciler.Reconcile(ctx, observation)
	if err != nil {
		return fmt.Errorf("reconcile file %q: %w", observation.RemotePath, err)
	}

	return r.removeFileFromState(ctx, reconcile.FileState{
		Observation: observation,
		Decision:    reconcileDecision,
	})
}

func (r *remover) removeLocalFile(localPath string) error {
	return r.removeLocalPath(localPath, reconcile.KindFile)
}

func (r *remover) removeLocalPath(localPath string, kind reconcile.Kind) error {
	if localPath == "" {
		return nil
	}

	if kind == reconcile.KindDir {
		if !r.opts.Recursive {
			return fmt.Errorf("%s is a directory; use --recursive to remove directory", localPath)
		}

		if err := os.RemoveAll(localPath); err != nil {
			return fmt.Errorf("remove local directory %q: %w", localPath, err)
		}
		return nil
	}

	err := os.Remove(localPath)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove local file %q: %w", localPath, err)
}

func (r *remover) removeFileFromState(ctx context.Context, state reconcile.FileState) error {
	if !shouldRemoveFile(r.opts, state) {
		_, _ = fmt.Fprintf(r.opts.Out, "Skipping %s - %s\n", state.Observation.RemotePath, state.Decision.Reason)
		return nil
	}

	remotePath := state.Observation.RemotePath

	// To determine if we should remove local and remote files we need to check if
	// either LocalOnly or RemoteOnly is set, or if neither is set. If neither is
	//  set, then we would remove both local and remote files.

	// Check if we should remove local files. Either the LocalOnly flag is set, or
	// LocalOnly is not set AND RemoteOnly is not set.
	removeLocal := r.opts.LocalOnly || (!r.opts.LocalOnly && !r.opts.RemoteOnly)

	// Check if we should remove remote files. Either RemoteOnly flag is set, or
	// LocalOnly is not set and RemoteOnly is not set
	removeRemote := r.opts.RemoteOnly || (!r.opts.LocalOnly && !r.opts.RemoteOnly)

	if r.opts.DryRun {
		fmt.Fprintf(r.opts.Out, "Would remove %s\n", remotePath)
		return nil
	}

	if removeLocal {
		localPath, err := r.translator.RemoteToLocal(remotePath)
		if err != nil {
			return fmt.Errorf("failed to translate remote path %s to local path: %w", remotePath, err)
		}

		if err := r.removeLocalPath(localPath, localRemovalKind(state)); err != nil {
			return err
		}
	}

	if removeRemote {
		if err := r.removeRemoteFile(ctx, state); err != nil {
			return err
		}
	}

	if err := r.store.DeleteByPath(ctx, remotePath); err != nil && !errors.Is(err, filedb.ErrRecordNotFound) {
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

func (r *remover) loadFileRecordByPathFromDB(ctx context.Context, remotePath string) (filedb.FileRecord, bool, error) {
	fileRecord, err := r.store.GetByPath(ctx, remotePath)
	switch {
	case err == nil:
		// Found record in database
		return fileRecord, true, nil

	case errors.Is(err, filedb.ErrRecordNotFound):
		// Record isn't found in database
		return filedb.FileRecord{}, false, nil

	default:
		// err != nil, and it's not filedb.ErrRecordNotFound
		return filedb.FileRecord{}, false, fmt.Errorf("get file record by path %q: %w", remotePath, err)
	}
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
		if opts.LocalOnly {
			return true
		}
		return state.Observation.RemoteEntry != nil
	default:
		// The default case is don't delete. All actions should have been covered by previous checks. This covers
		// the case where a new action was added, and this method hasn't been updated to handle it.
		return false
	}
}

func localRemovalKind(state reconcile.FileState) reconcile.Kind {
	switch {
	case state.Observation.LocalEntry != nil:
		return state.Observation.LocalEntry.Kind

	case state.Observation.RemoteEntry != nil:
		return state.Observation.RemoteEntry.Kind

	default:
		return reconcile.KindUnknown
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

func stateIsDir(state reconcile.FileState) bool {
	switch {
	case state.Observation.LocalEntry != nil && state.Observation.LocalEntry.Kind == reconcile.KindDir:
		// Local entry exists and is a directory
		return true

	case state.Observation.RemoteEntry != nil && state.Observation.RemoteEntry.Kind == reconcile.KindDir:
		// Remote entry exists and is a directory
		return true

	default:
		// Neither entry exists or is a directory
		return false
	}
}

func stateRemotePathDepth(state reconcile.FileState) int {
	if state.Observation.RemoteEntry != nil {
		remotePath := strings.Trim(state.Observation.RemotePath, "/")
		if remotePath == "" {
			return 0
		}
		return strings.Count(remotePath, "/") + 1
	}

	return 0
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
