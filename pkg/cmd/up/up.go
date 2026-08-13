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
	if opts.WebSocketURL == "" {
		return fmt.Errorf("websocket url is required")
	}

	deps := di.WithDefaults(r.Deps)

	projectCfg, err := deps.LoadProject(ctx, opts.WorkingDir)
	if err != nil {
		return err
	}

	projectRoot := projectCfg.ProjectRoot()
	if projectRoot == "" {
		projectRoot, err = projectpath.FindRoot(ctx, opts.WorkingDir)
		if err != nil {
			return err
		}
	}

	globalCfg, err := deps.LoadGlobal(ctx, "")
	if err != nil {
		return err
	}

	remoteCfg, ok := globalCfg.FindRemote(projectCfg.Remote.Email, projectCfg.Remote.MCURL)
	if !ok {
		return fmt.Errorf("remote %s %s is not configured in global config", projectCfg.Remote.Email, projectCfg.Remote.MCURL)
	}
	if remoteCfg.APIKey == "" {
		return fmt.Errorf("remote %s %s is missing an API key", projectCfg.Remote.Email, projectCfg.Remote.MCURL)
	}
	if globalCfg.ClientUUID == "" {
		return fmt.Errorf("global config client_uuid is required for websocket uploads")
	}

	store, err := deps.OpenStore(ctx, projectRoot)
	if err != nil {
		return err
	}
	defer func() {
		_ = store.Close(ctx)
	}()

	remote, err := deps.NewRemote(projectCfg, globalCfg)
	if err != nil {
		return err
	}

	var webSocketURL string
	if opts.WebSocketURL != "" {
		webSocketURL = opts.WebSocketURL
	} else {
		webSocketURL, err = config.ToWebSocketURLFromRemoteURL(projectCfg.Remote.MCURL)
		if err != nil {
			return fmt.Errorf("invalid remote MCURL (%s) can't construct websocket URL: %w", projectCfg.Remote.MCURL, err)
		}
	}

	translator, err := projectpath.New(projectRoot)
	if err != nil {
		return err
	}

	sendQueue := wsclient.NewQueue[wsclient.OutboundMessage]()
	dbQueue := wsclient.NewQueue[upload.DBWriteRequest]()

	progressFactory := upload.NewMPBProgressFactory(opts.Out)
	progress := upload.NewUploadProgress(progressFactory)
	defer progress.Wait()

	manager, err := deps.NewUploadManager(upload.Config{
		SendQueue:     sendQueue,
		DBWriteQueue:  dbQueue,
		ClientID:      globalCfg.ClientUUID,
		MaxConcurrent: 3,
		Progress:      progress,
	})
	if err != nil {
		return err
	}

	ws := deps.NewWebSocket(di.WebSocketConfig{
		URL:      webSocketURL,
		Token:    remoteCfg.APIKey,
		ClientID: globalCfg.ClientUUID,
		Outbound: sendQueue,
		Handle: func(ctx context.Context, msg wsclient.TextMessage) {
			manager.HandleMessage(msg)
		},
		ProjectIDs: []int{projectCfg.ProjectID},
	})

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	dbDone := startDBWriter(runCtx, store, dbQueue)

	manager.StartWorkers(runCtx)

	wsErrCh := make(chan error, 1)
	go func() {
		wsErrCh <- ws.Run(runCtx)
	}()

	reconciler := reconcile.New(reconcile.ModeUpload)
	observer := reconcile.NewObservationRunner(
		projectCfg.ProjectID,
		translator,
		store,
		remote,
		reconcile.ModeUpload,
	)
	observer.Reconciler = reconciler
	observer.Now = deps.Now

	transferIDs, err := r.queueUploads(ctx, queueRequest{
		opts:       opts,
		project:    projectCfg,
		manager:    manager,
		observer:   observer,
		translator: translator,
		now:        deps.Now,
	})
	if err != nil {
		cancel()
		return err
	}

	if err := waitForTransfers(ctx, manager, transferIDs); err != nil {
		cancel()
		return err
	}

	cancel()
	manager.StopWorkers()
	dbQueue.Close()
	<-dbDone

	select {
	case err := <-wsErrCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	default:
	}

	return nil
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

func startDBWriter(ctx context.Context, store di.Store, queue *wsclient.Queue[upload.DBWriteRequest]) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		defer close(done)

		for {
			req, ok, err := queue.Pop(ctx)
			if err != nil {
				return
			}
			if !ok {
				return
			}

			_ = store.Upsert(ctx, req.Record)
		}
	}()

	return done
}

func waitForTransfers(ctx context.Context, manager di.UploadManager, transferIDs []string) error {
	pending := map[string]bool{}
	for _, id := range transferIDs {
		pending[id] = true
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		for id := range pending {
			result, ok := manager.Result(id)
			if !ok {
				continue
			}
			if !result.Success {
				if result.Err != nil {
					return result.Err
				}
				return fmt.Errorf("upload %s failed", id)
			}
			delete(pending, id)
		}

		if len(pending) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}

	return nil
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
