// Package up implements the mc2 up command.
package up

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"github.com/materials-commons/mccli/pkg/reconcile"
	"github.com/materials-commons/mccli/pkg/services"
	"github.com/materials-commons/mccli/pkg/upload"
	"github.com/materials-commons/mccli/pkg/wsclient"
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

	deps := di.WithDefaults(r.Deps)

	container := services.NewContainer(deps)
	cmdCtx, err := container.LoadCommandContext(ctx, opts.WorkingDir)
	if err != nil {
		return err
	}

	if _, err := services.RequireConfiguredRemote(cmdCtx.Project, cmdCtx.Global); err != nil {
		return err
	}
	if err := cmdCtx.RequireClientUUID("websocket uploads"); err != nil {
		return err
	}

	store, err := container.Store(ctx)
	if err != nil {
		return err
	}

	remote, err := container.Remote()
	if err != nil {
		return err
	}

	translator, err := container.Translator()
	if err != nil {
		return err
	}

	manager, err := container.UploadManager(ctx, services.UploadManagerOptions{
		Out:           opts.Out,
		MaxConcurrent: 3,
	})
	if err != nil {
		return err
	}

	ws, err := container.WebSocket(services.WebSocketOptions{
		URL: opts.WebSocketURL,
		Handle: func(ctx context.Context, msg wsclient.TextMessage) {
			manager.HandleMessage(msg)
		},
	})
	if err != nil {
		return err
	}

	runtime := services.NewRuntime(container)
	if err := runtime.Start(ctx, services.StartOptions{
		UploadManager: manager,
		WebSocket:     ws,
	}); err != nil {
		return err
	}
	defer func() {
		_ = runtime.Stop(context.Background())
	}()

	reconciler := reconcile.New(reconcile.ModeUpload)
	observer := reconcile.NewObservationRunner(
		cmdCtx.Project.ProjectID,
		translator,
		store,
		remote,
		reconcile.ModeUpload,
	)
	observer.Reconciler = reconciler
	observer.Now = deps.Now

	transferIDs, err := r.queueUploads(ctx, queueRequest{
		opts:       opts,
		project:    cmdCtx.Project,
		manager:    manager,
		observer:   observer,
		translator: translator,
		now:        deps.Now,
	})
	if err != nil {
		return err
	}

	if err := services.WaitForUploads(ctx, manager, transferIDs); err != nil {
		return err
	}

	return runtime.Stop(ctx)
}

type queueRequest struct {
	opts       Options
	project    config.Project
	manager    di.UploadManager
	observer   *reconcile.ObservationRunner
	translator projectpath.Translator
	now        func() time.Time
}

func (r Runner) queueUploads(ctx context.Context, req queueRequest) ([]string, error) {
	var transferIDs []string

	for _, inputPath := range req.opts.Paths {
		localPath, err := resolveInputPath(req.opts.WorkingDir, inputPath)
		if err != nil {
			return nil, err
		}

		info, err := os.Stat(localPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(req.opts.Out, "%s: No such file or directory\n", inputPath)
				continue
			}
			return nil, fmt.Errorf("stat %q: %w", localPath, err)
		}

		if info.IsDir() {
			ids, err := queueDirectoryUploads(ctx, req, localPath)
			if err != nil {
				return nil, err
			}
			transferIDs = append(transferIDs, ids...)
			continue
		}

		if !info.Mode().IsRegular() {
			continue
		}

		id, queued, err := queueFileUpload(ctx, req, localPath)
		if err != nil {
			return nil, err
		}
		if queued {
			transferIDs = append(transferIDs, id)
		}
	}

	return transferIDs, nil
}

func queueDirectoryUploads(ctx context.Context, req queueRequest, localDir string) ([]string, error) {
	localList := reconcile.LocalNodeListDir(req.translator, req.now)

	remotePath, err := req.translator.LocalToRemote(localDir)
	if err != nil {
		return nil, err
	}

	options := reconcile.WalkOptions{
		Recursive:  req.opts.Recursive,
		Ignore:     nil,
		Translator: req.translator,
	}

	var transferIDs []string

	err = reconcile.WalkNodes(
		ctx,
		reconcile.WalkNode{LocalPath: localDir, RemotePath: remotePath},
		localList,
		options,
		func(ctx context.Context, node reconcile.WalkNode, observations []reconcile.Observation) error {
			for _, obs := range observations {
				if obs.LocalEntry == nil || obs.LocalEntry.Kind != reconcile.KindFile {
					continue
				}

				id, queued, err := queueFileUpload(ctx, req, obs.LocalEntry.Path)
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

func queueFileUpload(ctx context.Context, req queueRequest, localPath string) (string, bool, error) {
	state, err := req.observer.ObserveAndReconcile(ctx, localPath)
	if err != nil {
		return "", false, err
	}

	if state.Decision.Action != reconcile.ActionUpload {
		fmt.Fprintf(req.opts.Out, "Skipping %s - already uploaded\n", localPath)
		return "", false, nil
	}

	if state.Observation.LocalEntry == nil {
		return "", false, fmt.Errorf("cannot upload %q: local entry is missing", localPath)
	}

	transferID, err := req.manager.QueueUpload(upload.Request{
		ProjectID:     req.project.ProjectID,
		ClientID:      "",
		Observation:   state.Observation,
		UpdatedRecord: state.Decision.UpdatedRecord,
	})
	if err != nil {
		return "", false, err
	}

	return transferID, true, nil
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
