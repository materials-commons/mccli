package up

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/transfer"
)

func TestNewUploaderRequiresClientUUID(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	store := openStore(t, ctx, projectRoot)

	deps := testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, newFakeManager())
	deps.LoadGlobal = func(ctx context.Context, cfgPath string) (config.Global, error) {
		return config.Global{
			DefaultRemote: config.Remote{
				MCURL:  "https://example.test/api",
				Email:  "user@example.test",
				APIKey: "apikey",
			},
			ClientUUID: "",
		}, nil
	}

	uploader, err := newUploader(ctx, deps, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("newUploader() error = nil, want error")
	}
	if uploader != nil {
		t.Fatal("newUploader() uploader != nil, want nil")
	}
}

func TestNewUploaderRequiresConfiguredRemote(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	store := openStore(t, ctx, projectRoot)

	deps := testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, newFakeManager())
	deps.LoadGlobal = func(ctx context.Context, cfgPath string) (config.Global, error) {
		return config.Global{
			DefaultRemote: config.Remote{
				MCURL: "https://example.test/api",
				Email: "user@example.test",
			},
			ClientUUID: "client-uuid",
		}, nil
	}

	uploader, err := newUploader(ctx, deps, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("newUploader() error = nil, want error")
	}
	if uploader != nil {
		t.Fatal("newUploader() uploader != nil, want nil")
	}
}

func TestNewUploaderRequiresRemoteFileGetter(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	store := openStore(t, ctx, projectRoot)

	uploader, err := newUploader(ctx, testDeps(projectRoot, store, struct{}{}, newFakeManager()), Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("newUploader() error = nil, want error")
	}
	if got, want := err.Error(), "remote is not a FileGetter"; got != want {
		t.Fatalf("newUploader() error = %q, want %q", got, want)
	}
	if uploader != nil {
		t.Fatal("newUploader() uploader != nil, want nil")
	}
}

func TestNewUploaderReturnsUploadManagerError(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	store := openStore(t, ctx, projectRoot)
	wantErr := errors.New("upload manager exploded")

	deps := testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, newFakeManager())
	deps.NewUploadManager = func(cfg transfer.UploadConfig) (di.UploadManager, error) {
		return nil, wantErr
	}

	uploader, err := newUploader(ctx, deps, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("newUploader() error = %v, want %v", err, wantErr)
	}
	if uploader != nil {
		t.Fatal("newUploader() uploader != nil, want nil")
	}
}

func TestNewUploaderConfiguresWebSocketFromOptions(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	store := openStore(t, ctx, projectRoot)

	var got di.WebSocketConfig
	deps := testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, newFakeManager())
	deps.NewWebSocket = func(cfg di.WebSocketConfig) di.WebSocketRunner {
		got = cfg
		return &fakeWebSocket{}
	}

	uploader, err := newUploader(ctx, deps, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://upload.example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("newUploader() error = %v, want nil", err)
	}
	defer uploader.stop(context.Background())

	if got.URL != "ws://upload.example.test/ws" {
		t.Fatalf("websocket URL = %q, want ws://upload.example.test/ws", got.URL)
	}
	if got.Token != "apikey" {
		t.Fatalf("websocket token = %q, want apikey", got.Token)
	}
	if got.ClientID != "client-uuid" {
		t.Fatalf("websocket client id = %q, want client-uuid", got.ClientID)
	}
	if len(got.ProjectIDs) != 1 || got.ProjectIDs[0] != 1 {
		t.Fatalf("websocket project ids = %v, want [1]", got.ProjectIDs)
	}
	if got.Outbound == nil {
		t.Fatal("websocket outbound queue is nil, want queue")
	}
	if got.Handle == nil {
		t.Fatal("websocket handler is nil, want handler")
	}
}

func TestNewUploaderDerivesWebSocketURLWhenNotProvided(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	store := openStore(t, ctx, projectRoot)

	var got di.WebSocketConfig
	deps := testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, newFakeManager())
	deps.NewWebSocket = func(cfg di.WebSocketConfig) di.WebSocketRunner {
		got = cfg
		return &fakeWebSocket{}
	}

	uploader, err := newUploader(ctx, deps, Options{
		WorkingDir: projectRoot,
		Paths:      []string{"file.txt"},
		Out:        &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("newUploader() error = %v, want nil", err)
	}
	defer uploader.stop(context.Background())

	if got.URL == "" {
		t.Fatal("websocket URL is empty, want derived URL")
	}
}

func TestUploaderUploadReturnsQueueUploadErrorAndStopsWithDeferredStop(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	filePath := filepath.Join(projectRoot, "file.txt")
	writeFile(t, filePath, "hello")

	store := openStore(t, ctx, projectRoot)
	manager := newFakeManager()
	wantErr := errors.New("queue upload failed")
	manager.queueErr = wantErr

	runner := Runner{Deps: testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, manager)}

	err := runner.Run(ctx, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if len(manager.requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(manager.requests))
	}
	if manager.stopCount != 1 {
		t.Fatalf("manager stop count = %d, want 1", manager.stopCount)
	}
}

func TestUploaderUploadReturnsFailedUploadResult(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	filePath := filepath.Join(projectRoot, "file.txt")
	writeFile(t, filePath, "hello")

	store := openStore(t, ctx, projectRoot)
	manager := newFakeManager()
	wantErr := errors.New("server rejected upload")
	manager.resultSuccess = false
	manager.resultErr = wantErr

	uploader, err := newUploader(ctx, testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, manager), Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("newUploader() error = %v, want nil", err)
	}
	defer uploader.stop(context.Background())

	err = uploader.upload(ctx)
	if !errors.Is(err, wantErr) {
		t.Fatalf("upload() error = %v, want %v", err, wantErr)
	}
	if manager.stopCount != 1 {
		t.Fatalf("manager stop count = %d, want 1", manager.stopCount)
	}
	if uploader.runtime != nil {
		t.Fatal("uploader runtime != nil, want nil after upload stops runtime")
	}
}

func TestUploaderUploadHonorsContextCancellationWhileWaiting(t *testing.T) {
	projectRoot := makeProject(t)
	filePath := filepath.Join(projectRoot, "file.txt")
	writeFile(t, filePath, "hello")

	ctx, cancel := context.WithCancel(context.Background())
	store := openStore(t, context.Background(), projectRoot)
	manager := newFakeManager()
	manager.hideResults = true

	uploader, err := newUploader(ctx, testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, manager), Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("newUploader() error = %v, want nil", err)
	}
	defer uploader.stop(context.Background())

	cancel()

	err = uploader.upload(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("upload() error = %v, want context.Canceled", err)
	}
}

func TestUploaderQueueUploadsSkipsNonRegularFile(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	dirPath := filepath.Join(projectRoot, "dir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	store := openStore(t, ctx, projectRoot)
	manager := newFakeManager()

	uploader, err := newUploader(ctx, testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, manager), Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"dir"},
		Recursive:    false,
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("newUploader() error = %v, want nil", err)
	}
	defer uploader.stop(context.Background())

	transferIDs, err := uploader.queueUploads(ctx)
	if err != nil {
		t.Fatalf("queueUploads() error = %v, want nil", err)
	}
	if len(transferIDs) != 0 {
		t.Fatalf("len(transferIDs) = %d, want 0", len(transferIDs))
	}
	if len(manager.requests) != 0 {
		t.Fatalf("len(requests) = %d, want 0", len(manager.requests))
	}
}

func TestUploaderQueueUploadsContinuesAfterMissingPath(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	writeFile(t, filepath.Join(projectRoot, "file.txt"), "hello")

	store := openStore(t, ctx, projectRoot)
	manager := newFakeManager()
	out := &bytes.Buffer{}

	uploader, err := newUploader(ctx, testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, manager), Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"missing.txt", "file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          out,
	})
	if err != nil {
		t.Fatalf("newUploader() error = %v, want nil", err)
	}
	defer uploader.stop(context.Background())

	transferIDs, err := uploader.queueUploads(ctx)
	if err != nil {
		t.Fatalf("queueUploads() error = %v, want nil", err)
	}
	if len(transferIDs) != 1 {
		t.Fatalf("len(transferIDs) = %d, want 1", len(transferIDs))
	}
	if len(manager.requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(manager.requests))
	}
	if got := out.String(); got != "missing.txt: No such file or directory\n" {
		t.Fatalf("output = %q, want missing path warning", got)
	}
}

func TestUploaderStopIsIdempotent(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	store := openStore(t, ctx, projectRoot)
	manager := newFakeManager()

	uploader, err := newUploader(ctx, testDeps(projectRoot, store, &fakeRemote{files: map[string]mcmodel.File{}}, manager), Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("newUploader() error = %v, want nil", err)
	}

	if err := uploader.stop(ctx); err != nil {
		t.Fatalf("first stop() error = %v, want nil", err)
	}
	if err := uploader.stop(ctx); err != nil {
		t.Fatalf("second stop() error = %v, want nil", err)
	}
	if manager.stopCount != 1 {
		t.Fatalf("manager stop count = %d, want 1", manager.stopCount)
	}
}
