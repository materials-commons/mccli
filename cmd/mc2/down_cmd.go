package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/filedb"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/reconcile"
	"github.com/materials-commons/mccli/internal/services"
	"github.com/materials-commons/mccli/internal/transfer"
	"github.com/urfave/cli/v3"
)

func downCommand() *cli.Command {
	return &cli.Command{
		Name:      "down",
		Usage:     "Download files or directories from Materials Commons",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "recursive",
				Aliases: []string{"r"},
				Usage:   "Download directory contents recursively",
			},
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "Overwrite local files even if they were not previously uploaded",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			paths := make([]string, 0, cmd.Args().Len())
			for i := 0; i < cmd.Args().Len(); i++ {
				paths = append(paths, cmd.Args().Get(i))
			}

			workingDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}

			return runDownCmd(ctx, downOpts{
				WorkingDir: workingDir,
				Paths:      paths,
				Recursive:  cmd.Bool("recursive"),
				Force:      cmd.Bool("force"),
				Out:        os.Stdout,
			})
		},
	}
}

// Options contains user-facing mc2 down command options.
type downOpts struct {
	// WorkingDir is the directory used to discover the current Materials Commons project.
	WorkingDir string

	// Paths are remote Materials Commons project paths to download.
	Paths []string

	// Recursive controls whether directory contents are downloaded recursively.
	Recursive bool

	// Force allows overwriting local files when reconciliation reports conflicts.
	Force bool

	// Out receives command output. If nil, os.Stdout is used.
	Out io.Writer
}

// Runner executes down with injected dependencies.
type downRunner struct {
	Deps di.Dependencies
}

// Run executes mc2 down using production dependencies.
func runDownCmd(ctx context.Context, opts downOpts) error {
	return downRunner{Deps: di.Production()}.Run(ctx, opts)
}

// Run executes the down command.
func (r downRunner) Run(ctx context.Context, opts downOpts) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	opts = normalizeOptions(opts)
	if len(opts.Paths) == 0 {
		return fmt.Errorf("at least one path is required")
	}

	deps := di.WithDefaults(r.Deps)

	container := services.NewContainer(deps)
	cmdCtx, err := container.LoadCommandContext(ctx, opts.WorkingDir)
	if err != nil {
		return err
	}

	remoteCfg, err := services.RequireConfiguredRemote(cmdCtx.Project, cmdCtx.Global)
	if err != nil {
		return err
	}
	if err := cmdCtx.RequireClientUUID("websocket downloads"); err != nil {
		return err
	}

	store, err := container.Store(ctx)
	if err != nil {
		return err
	}

	remoteAny, err := container.Remote()
	if err != nil {
		return err
	}

	remote, ok := remoteAny.(mc.FileDirectoryGetter)
	if !ok {
		return fmt.Errorf("remote is not a FileGetter")
	}

	translator, err := container.Translator()
	if err != nil {
		return err
	}

	manager, err := container.DownloadManager(ctx, services.DownloadManagerOptions{
		Out:           opts.Out,
		MaxConcurrent: 3,
	})
	if err != nil {
		return err
	}

	runtime := services.NewRuntime(container)
	err = runtime.Start(ctx, services.StartOptions{
		DownloadManager: manager,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = runtime.Stop(context.Background())
	}()

	reconciler := reconcile.New(reconcile.ModeDownload)
	transferIDs, err := r.queueDownloads(ctx, queueRequest{
		opts:       opts,
		project:    cmdCtx.Project,
		remoteCfg:  remoteCfg,
		manager:    manager,
		store:      store,
		remote:     remote,
		translator: translator,
		reconciler: reconciler,
	})
	if err != nil {
		return err
	}

	if err := services.WaitForDownloads(ctx, manager, transferIDs); err != nil {
		return err
	}

	return runtime.Stop(ctx)
}

type queueRequest struct {
	opts       downOpts
	project    config.Project
	remoteCfg  config.Remote
	manager    di.DownloadManager
	store      di.Store
	remote     mc.FileDirectoryGetter
	translator mc.ProjectPathTranslator
	reconciler *reconcile.Reconciler
}

func (r downRunner) queueDownloads(ctx context.Context, req queueRequest) ([]string, error) {
	var transferIDs []string

	for _, inputPath := range req.opts.Paths {
		remotePath, err := normalizeInputRemotePath(inputPath)
		if err != nil {
			return nil, err
		}

		ids, err := queueRemotePath(ctx, req, remotePath)
		if err != nil {
			return nil, err
		}
		transferIDs = append(transferIDs, ids...)
	}

	return transferIDs, nil
}

func queueRemotePath(ctx context.Context, req queueRequest, remotePath string) ([]string, error) {
	remoteFile, err := req.remote.GetFileByPath(req.project.ProjectID, remotePath)
	if err != nil {
		if reconcile.IsRemoteNotFound(err) {
			fmt.Fprintf(req.opts.Out, "%s: No such file or directory\n", remotePath)
			return nil, nil
		}
		return nil, fmt.Errorf("get remote file by path %q: %w", remotePath, err)
	}

	remoteEntry, err := reconcile.RemoteEntryFromMCFile(remoteFile)
	if err != nil {
		return nil, err
	}
	if remoteEntry == nil {
		fmt.Fprintf(req.opts.Out, "%s: No such file or directory\n", remotePath)
		return nil, nil
	}

	if remoteEntry.Kind == reconcile.KindDir {
		return queueDirectoryDownloads(ctx, req, remoteEntry.Path)
	}

	id, queued, err := queueFileDownload(ctx, req, reconcile.Observation{
		RemotePath:  remoteEntry.Path,
		Name:        remoteEntry.Name,
		Dir:         remoteEntry.Dir,
		RemoteEntry: remoteEntry,
	})
	if err != nil {
		return nil, err
	}
	if !queued {
		return nil, nil
	}

	return []string{id}, nil
}

func queueDirectoryDownloads(ctx context.Context, req queueRequest, remoteDir string) ([]string, error) {
	remoteList := reconcile.RemoteOnlyListDir(req.project.ProjectID, req.translator, req.remote)

	options := reconcile.WalkOptions{
		Recursive:  req.opts.Recursive,
		Ignore:     nil,
		Translator: req.translator,
	}

	var transferIDs []string

	err := reconcile.WalkNodesAndReconcile(
		ctx,
		reconcile.WalkNode{RemotePath: remoteDir},
		remoteList,
		req.store,
		req.reconciler,
		options,
		func(ctx context.Context, node reconcile.WalkNode, states map[string]reconcile.FileState) error {
			for _, state := range states {
				if state.Observation.RemoteEntry == nil || state.Observation.RemoteEntry.Kind != reconcile.KindFile {
					continue
				}

				id, queued, err := queueFileDownloadFromState(ctx, req, state)
				if err != nil {
					return err
				}
				if queued {
					transferIDs = append(transferIDs, id)
				}
			}

			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	return transferIDs, nil
}

func queueFileDownload(ctx context.Context, req queueRequest, obs reconcile.Observation) (string, bool, error) {
	record, hasRecord, err := loadFileRecord(ctx, req.store, obs.RemotePath)
	if err != nil {
		return "", false, err
	}
	if hasRecord {
		recordCopy := record
		obs.FileRecord = &recordCopy
	}

	decision, err := req.reconciler.Reconcile(ctx, obs)
	if err != nil {
		return "", false, err
	}

	return queueFileDownloadFromState(ctx, req, reconcile.FileState{
		Observation: obs,
		Decision:    decision,
	})
}

func queueFileDownloadFromState(ctx context.Context, req queueRequest, state reconcile.FileState) (string, bool, error) {
	if state.Decision.Action != reconcile.ActionDownload {
		if state.Decision.Action == reconcile.ActionConflict && req.opts.Force {
			// Force treats conflicts as explicit download requests.
		} else {
			fmt.Fprintf(req.opts.Out, "Skipping %s - %s\n", state.Observation.RemotePath, state.Decision.Reason)
			return "", false, nil
		}
	}

	if state.Observation.RemoteEntry == nil {
		return "", false, fmt.Errorf("cannot download %q: remote entry is missing", state.Observation.RemotePath)
	}
	if state.Observation.RemoteEntry.RemoteFileID == nil {
		return "", false, fmt.Errorf("cannot download %q: remote file id is missing", state.Observation.RemotePath)
	}

	localPath, err := req.translator.RemoteToLocal(state.Observation.RemotePath)
	if err != nil {
		return "", false, err
	}

	transferID, err := req.manager.QueueDownload(transfer.DownloadRequest{
		ProjectID:     req.project.ProjectID,
		ClientID:      "",
		BaseURL:       req.remoteCfg.MCURL,
		APIToken:      req.remoteCfg.APIKey,
		ProjectRoot:   req.translator.ProjectRoot(),
		LocalPath:     localPath,
		Observation:   state.Observation,
		UpdatedRecord: state.Decision.UpdatedRecord,
	})
	if err != nil {
		return "", false, err
	}

	return transferID, true, nil
}

func loadFileRecord(ctx context.Context, store di.Store, remotePath string) (filedb.FileRecord, bool, error) {
	record, err := store.GetByPath(ctx, remotePath)
	if err == nil {
		return record, true, nil
	}
	if errors.Is(err, filedb.ErrRecordNotFound) {
		return filedb.FileRecord{}, false, nil
	}
	return filedb.FileRecord{}, false, fmt.Errorf("get file record by path %q: %w", remotePath, err)
}

func normalizeOptions(opts downOpts) downOpts {
	if opts.WorkingDir == "" {
		opts.WorkingDir = "."
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	return opts
}

func normalizeInputRemotePath(inputPath string) (string, error) {
	if inputPath == "" {
		inputPath = "/"
	}

	inputPath = filepath.ToSlash(inputPath)
	if !strings.HasPrefix(inputPath, "/") {
		inputPath = "/" + inputPath
	}

	cleaned := path.Clean(inputPath)
	if cleaned == "." {
		cleaned = "/"
	}

	return mc.NormalizeRemoteProjectPath(cleaned)
}
