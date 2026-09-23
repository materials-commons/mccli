package up

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/reconcile"
	"github.com/materials-commons/mccli/internal/services"
	"github.com/materials-commons/mccli/internal/transfer"
	"github.com/materials-commons/mccli/internal/wsclient"
)

type uploader struct {
	opts       Options
	project    config.Project
	manager    di.UploadManager
	observer   *reconcile.ObservationRunner
	translator mc.ProjectPathTranslator
	now        func() time.Time
	runtime    *services.Runtime
}

// newUploader performs all the setup nescessary to create an uploader.
func newUploader(ctx context.Context, deps di.Dependencies, opts Options) (*uploader, error) {
	deps = di.WithDefaults(deps)

	// Create the service container that manages the dependencies for the uploader.
	container := services.NewContainer(deps)
	cmdCtx, err := container.LoadCommandContext(ctx, opts.WorkingDir)
	if err != nil {
		return nil, err
	}

	// Validate that everything is correctly configured.
	if _, err := services.RequireConfiguredRemote(cmdCtx.Project, cmdCtx.Global); err != nil {
		return nil, err
	}
	if err := cmdCtx.RequireClientUUID("websocket uploads"); err != nil {
		return nil, err
	}

	// Create the store where file state is stored.
	store, err := container.Store(ctx)
	if err != nil {
		return nil, err
	}

	// Create the remote client.
	remoteAny, err := container.Remote()
	if err != nil {
		return nil, err
	}

	// Cast it to just the interface we need.
	remote, ok := remoteAny.(mc.FileGetter)
	if !ok {
		return nil, fmt.Errorf("remote is not a FileGetter")
	}

	// Crate the project path translator for handling local and remote project paths.
	translator, err := container.Translator()
	if err != nil {
		return nil, err
	}

	// Create the upload manager. This manages the queue of upload requests and controls parallelism.
	manager, err := container.UploadManager(ctx, services.UploadManagerOptions{
		Out:           opts.Out,
		MaxConcurrent: 3,
	})
	if err != nil {
		return nil, err
	}

	// Setup the WebSocket we need to communicate with the remote upload server.
	ws, err := container.WebSocket(services.WebSocketOptions{
		URL: opts.WebSocketURL,
		Handle: func(ctx context.Context, msg wsclient.TextMessage) {
			manager.HandleMessage(msg)
		},
	})
	if err != nil {
		return nil, err
	}

	// Create a runtime to manage the lifecycle for these services.
	runtime := services.NewRuntime(container)
	if err := runtime.Start(ctx, services.StartOptions{
		UploadManager: manager,
		WebSocket:     ws,
	}); err != nil {
		return nil, err
	}

	// Create the reconciler and observation runner. The reconciler is responsible for deciding what
	// to do with each file. The Observer looks at each file and calls the reconciler on it.
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

	return &uploader{
		opts:       opts,
		project:    cmdCtx.Project,
		manager:    manager,
		observer:   observer,
		translator: translator,
		now:        deps.Now,
		runtime:    runtime,
	}, nil
}

// upload will start queueing the uploads, and then wait for the uploadss to complete.
func (u *uploader) upload(ctx context.Context) error {
	defer u.stop(context.Background())

	transferIDs, err := u.queueUploads(ctx)
	if err != nil {
		return err
	}

	if err := services.WaitForUploads(ctx, u.manager, transferIDs); err != nil {
		return err
	}

	return nil
}

// stop will stop the runtime and return any errors. This is called after all uploads have finished.
func (u *uploader) stop(ctx context.Context) error {
	if u.runtime == nil {
		return nil
	}

	err := u.runtime.Stop(ctx)
	u.runtime = nil
	return err
}

type queueRequest struct {
	opts       Options
	project    config.Project
	manager    di.UploadManager
	observer   *reconcile.ObservationRunner
	translator mc.ProjectPathTranslator
	now        func() time.Time
}

func (u *uploader) queueUploads(ctx context.Context) ([]string, error) {
	return resolveAndQueueUploadsFromPaths(ctx, queueRequest{
		opts:       u.opts,
		project:    u.project,
		manager:    u.manager,
		observer:   u.observer,
		translator: u.translator,
		now:        u.now,
	})
}

func resolveAndQueueUploadsFromPaths(ctx context.Context, req queueRequest) ([]string, error) {
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
			ids, err := uploadDirectory(ctx, req, localPath)
			if err != nil {
				return nil, err
			}
			transferIDs = append(transferIDs, ids...)
			continue
		}

		if !info.Mode().IsRegular() {
			continue
		}

		id, queued, err := uploadFile(ctx, req, localPath)
		if err != nil {
			return nil, err
		}
		if queued {
			transferIDs = append(transferIDs, id)
		}
	}

	return transferIDs, nil
}

func uploadDirectory(ctx context.Context, req queueRequest, localDir string) ([]string, error) {
	localListFn := reconcile.MakeLocalNodeListDirFunc(req.translator, req.now)

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

	uploadFn := func(ctx context.Context, node reconcile.WalkNode, observations []reconcile.Observation) error {
		for _, obs := range observations {
			if obs.LocalEntry == nil || obs.LocalEntry.Kind != reconcile.KindFile {
				continue
			}

			id, queued, err := uploadFile(ctx, req, obs.LocalEntry.Path)
			if err != nil {
				return err
			}
			if queued {
				transferIDs = append(transferIDs, id)
			}
		}
		return nil
	}

	node := reconcile.WalkNode{LocalPath: localDir, RemotePath: remotePath}

	err = reconcile.WalkNodes(ctx, node, localListFn, options, uploadFn)
	if err != nil {
		return nil, err
	}

	return transferIDs, nil
}

func uploadFile(ctx context.Context, req queueRequest, localPath string) (string, bool, error) {
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

	transferID, err := req.manager.QueueUpload(transfer.UploadRequest{
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
