package services

import (
	"context"
	"reflect"
	"testing"

	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/download"
	"github.com/materials-commons/mccli/pkg/upload"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

func TestRuntimeStartsAndStopsInOrder(t *testing.T) {
	ctx := context.Background()
	container := NewContainer(testDependencies())

	var events []string

	uploadManager := &recordingUploadManager{
		events: &events,
	}
	downloadManager := &recordingDownloadManager{
		events: &events,
	}
	websocket := &recordingWebSocket{
		events: &events,
	}

	runtime := NewRuntime(container)

	if err := runtime.Start(ctx, StartOptions{
		UploadManager:   uploadManager,
		DownloadManager: downloadManager,
		WebSocket:       websocket,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := runtime.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	want := []string{
		"upload:start",
		"download:start",
		"websocket:run",
		"upload:stop",
		"download:stop",
	}

	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestRuntimeStopIsIdempotent(t *testing.T) {
	ctx := context.Background()
	container := NewContainer(testDependencies())

	var events []string

	runtime := NewRuntime(container)
	if err := runtime.Start(ctx, StartOptions{
		UploadManager: &recordingUploadManager{events: &events},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := runtime.Stop(ctx); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err := runtime.Stop(ctx); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}

	want := []string{
		"upload:start",
		"upload:stop",
	}

	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

type recordingUploadManager struct {
	events *[]string
}

func (m *recordingUploadManager) StartWorkers(ctx context.Context) {
	*m.events = append(*m.events, "upload:start")
}

func (m *recordingUploadManager) StopWorkers() {
	*m.events = append(*m.events, "upload:stop")
}

func (m *recordingUploadManager) QueueUpload(req upload.Request) (string, error) {
	return "upload-1", nil
}

func (m *recordingUploadManager) HandleMessage(msg wsclient.TextMessage) {}

func (m *recordingUploadManager) Result(transferID string) (upload.Result, bool) {
	return upload.Result{Success: true}, true
}

type recordingDownloadManager struct {
	events *[]string
}

func (m *recordingDownloadManager) StartWorkers(ctx context.Context) {
	*m.events = append(*m.events, "download:start")
}

func (m *recordingDownloadManager) StopWorkers() {
	*m.events = append(*m.events, "download:stop")
}

func (m *recordingDownloadManager) QueueDownload(req download.Request) (string, error) {
	return "download-1", nil
}

func (m *recordingDownloadManager) Result(transferID string) (download.Result, bool) {
	return download.Result{Success: true}, true
}

type recordingWebSocket struct {
	events *[]string
}

func (w *recordingWebSocket) Run(ctx context.Context) error {
	*w.events = append(*w.events, "websocket:run")
	<-ctx.Done()
	return ctx.Err()
}

func testDependencies() di.Dependencies {
	return di.Dependencies{
		LoadProject: func(ctx context.Context, start string) (config.Project, error) {
			return config.Project{
				ProjectID: 10,
				Remote: config.Remote{
					Email: "user@example.test",
					MCURL: "https://example.test/api",
				},
			}, nil
		},
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{
				ClientUUID: "client-1",
				DefaultRemote: config.Remote{
					Email:  "user@example.test",
					MCURL:  "https://example.test/api",
					APIKey: "token",
				},
			}, nil
		},
		OpenStore: func(ctx context.Context, projectRoot string) (di.Store, error) {
			return fakeStore{}, nil
		},
		NewRemote: func(project config.Project, global config.Global) (di.RemoteClient, error) {
			return &fakeRemote{}, nil
		},
		NewUploadManager: func(cfg upload.Config) (di.UploadManager, error) {
			return fakeUploadManager{}, nil
		},
		NewDownloadManager: func(cfg download.Config) (di.DownloadManager, error) {
			return fakeDownloadManager{}, nil
		},
		NewWebSocket: func(cfg di.WebSocketConfig) di.WebSocketRunner {
			return fakeWebSocket{}
		},
	}
}
