package services

import (
	"context"
	"errors"
	"sync"

	"github.com/materials-commons/mccli/pkg/di"
)

// Runtime starts and stops selected services with consistent ordering.
type Runtime struct {
	container *Container

	ctx    context.Context
	cancel context.CancelFunc

	startedUploadManager   bool
	startedDownloadManager bool
	startedWebSocket       bool

	uploadManager   di.UploadManager
	downloadManager di.DownloadManager
	websocket       di.WebSocketRunner

	wsErrCh chan error
	once    sync.Once
}

func NewRuntime(container *Container) *Runtime {
	return &Runtime{
		container: container,
	}
}

type StartOptions struct {
	UploadManager   di.UploadManager
	DownloadManager di.DownloadManager
	WebSocket       di.WebSocketRunner
}

func (r *Runtime) Start(ctx context.Context, opts StartOptions) error {
	if r.ctx == nil {
		r.ctx, r.cancel = context.WithCancel(ctx)
	}

	if opts.UploadManager != nil && !r.startedUploadManager {
		opts.UploadManager.StartWorkers(r.ctx)
		r.uploadManager = opts.UploadManager
		r.startedUploadManager = true
	}

	if opts.DownloadManager != nil && !r.startedDownloadManager {
		opts.DownloadManager.StartWorkers(r.ctx)
		r.downloadManager = opts.DownloadManager
		r.startedDownloadManager = true
	}

	if opts.WebSocket != nil && !r.startedWebSocket {
		r.websocket = opts.WebSocket
		r.wsErrCh = make(chan error, 1)
		r.startedWebSocket = true

		go func() {
			r.wsErrCh <- opts.WebSocket.Run(r.ctx)
		}()
	}

	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	var stopErr error

	r.once.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}

		if r.startedWebSocket && r.wsErrCh != nil {
			select {
			case err := <-r.wsErrCh:
				if err != nil && !errors.Is(err, context.Canceled) {
					stopErr = err
				}
			case <-ctx.Done():
				stopErr = ctx.Err()
			}
			r.startedWebSocket = false
		}

		if r.startedUploadManager && r.uploadManager != nil {
			r.uploadManager.StopWorkers()
			r.startedUploadManager = false
		}

		if r.startedDownloadManager && r.downloadManager != nil {
			r.downloadManager.StopWorkers()
			r.startedDownloadManager = false
		}

		if err := r.container.Close(ctx); err != nil && stopErr == nil {
			stopErr = err
		}
	})

	return stopErr
}
