package up

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/upload"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

func TestRunnerRequiresPath(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)

	runner := Runner{Deps: testDeps(projectRoot, nil, nil, nil)}

	err := runner.Run(ctx, Options{
		WorkingDir:   projectRoot,
		WebSocketURL: "ws://example.test/ws",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}

func TestRunnerQueuesSingleFileUpload(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	filePath := filepath.Join(projectRoot, "file.txt")
	writeFile(t, filePath, "hello")

	store := openStore(t, ctx, projectRoot)
	remote := &fakeRemote{files: map[string]mcmodel.File{}}
	manager := newFakeManager()

	runner := Runner{Deps: testDeps(projectRoot, store, remote, manager)}

	err := runner.Run(ctx, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"file.txt"},
		WebSocketURL: "ws://example.test/ws",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(manager.requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(manager.requests))
	}

	req := manager.requests[0]
	if req.ProjectID != 1 {
		t.Fatalf("ProjectID = %d, want 1", req.ProjectID)
	}
	if req.Observation.RemotePath != "/file.txt" {
		t.Fatalf("RemotePath = %q, want /file.txt", req.Observation.RemotePath)
	}
	if req.UpdatedRecord.LocalChecksum == nil || *req.UpdatedRecord.LocalChecksum == "" {
		t.Fatal("LocalChecksum is empty, want checksum computed")
	}
}

func TestRunnerSkipsRemoteOnlyPath(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)

	store := openStore(t, ctx, projectRoot)
	remote := &fakeRemote{files: map[string]mcmodel.File{}}
	manager := newFakeManager()

	runner := Runner{Deps: testDeps(projectRoot, store, remote, manager)}

	err := runner.Run(ctx, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"missing.txt"},
		WebSocketURL: "ws://example.test/ws",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(manager.requests) != 0 {
		t.Fatalf("len(requests) = %d, want 0", len(manager.requests))
	}
}

func TestRunnerQueuesRecursiveDirectoryUploads(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)

	if err := os.MkdirAll(filepath.Join(projectRoot, "dir", "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, filepath.Join(projectRoot, "dir", "a.txt"), "a")
	writeFile(t, filepath.Join(projectRoot, "dir", "sub", "b.txt"), "b")

	store := openStore(t, ctx, projectRoot)
	remote := &fakeRemote{files: map[string]mcmodel.File{}}
	manager := newFakeManager()

	runner := Runner{Deps: testDeps(projectRoot, store, remote, manager)}

	err := runner.Run(ctx, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"dir"},
		Recursive:    true,
		WebSocketURL: "ws://example.test/ws",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(manager.requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(manager.requests))
	}
}

func TestRunnerNonRecursiveDirectoryQueuesOnlyImmediateFiles(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)

	if err := os.MkdirAll(filepath.Join(projectRoot, "dir", "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, filepath.Join(projectRoot, "dir", "a.txt"), "a")
	writeFile(t, filepath.Join(projectRoot, "dir", "sub", "b.txt"), "b")

	store := openStore(t, ctx, projectRoot)
	remote := &fakeRemote{files: map[string]mcmodel.File{}}
	manager := newFakeManager()

	runner := Runner{Deps: testDeps(projectRoot, store, remote, manager)}

	err := runner.Run(ctx, Options{
		WorkingDir:   projectRoot,
		Paths:        []string{"dir"},
		Recursive:    false,
		WebSocketURL: "ws://example.test/ws",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(manager.requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(manager.requests))
	}
	if manager.requests[0].Observation.Name != "a.txt" {
		t.Fatalf("queued name = %q, want a.txt", manager.requests[0].Observation.Name)
	}
}

func testDeps(projectRoot string, store RecordStore, remote RemoteClient, manager *fakeManager) Dependencies {
	if manager == nil {
		manager = newFakeManager()
	}
	if remote == nil {
		remote = &fakeRemote{files: map[string]mcmodel.File{}}
	}

	return Dependencies{
		LoadProject: func(ctx context.Context, start string) (config.Project, error) {
			return config.LoadProject(ctx, projectRoot)
		},
		LoadGlobal: func(ctx context.Context, cfgPath string) (config.Global, error) {
			return config.Global{
				DefaultRemote: config.Remote{
					MCURL:  "https://example.test/api",
					Email:  "user@example.test",
					APIKey: "apikey",
				},
				ClientUUID: "client-uuid",
			}, nil
		},
		OpenStore: func(ctx context.Context, root string) (RecordStore, error) {
			return store, nil
		},
		NewRemote: func(project config.Project, global config.Global) (RemoteClient, error) {
			return remote, nil
		},
		NewUploadManager: func(cfg upload.Config) (UploadManager, error) {
			manager.sendQueue = cfg.SendQueue
			manager.dbQueue = cfg.DBWriteQueue
			return manager, nil
		},
		NewWebSocket: func(cfg WebSocketConfig) WebSocketRunner {
			return &fakeWebSocket{}
		},
		Now: func() time.Time {
			return time.Unix(100, 0)
		},
	}
}

type fakeRemote struct {
	files map[string]mcmodel.File
}

func (f *fakeRemote) GetFileByPath(projectID int, remotePath string) (*mcmodel.File, error) {
	if file, ok := f.files[remotePath]; ok {
		return &file, nil
	}
	return nil, fakeNotFound()
}

func fakeNotFound() error {
	return &mcapi.APIError{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
	}
}

type fakeManager struct {
	mu       sync.Mutex
	requests []upload.Request
	results  map[string]upload.Result
	counter  int

	sendQueue *wsclient.Queue[wsclient.OutboundMessage]
	dbQueue   *wsclient.Queue[upload.DBWriteRequest]
}

func newFakeManager() *fakeManager {
	return &fakeManager{
		results: map[string]upload.Result{},
	}
}

func (f *fakeManager) StartWorkers(ctx context.Context) {}

func (f *fakeManager) StopWorkers() {}

func (f *fakeManager) QueueUpload(req upload.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.counter++
	id := fmt.Sprintf("transfer-%d", f.counter)

	f.requests = append(f.requests, req)
	f.results[id] = upload.Result{
		TransferID: id,
		Success:    true,
	}

	return id, nil
}

func (f *fakeManager) HandleMessage(msg wsclient.TextMessage) {}

func (f *fakeManager) Result(transferID string) (upload.Result, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	result, ok := f.results[transferID]
	return result, ok
}

type fakeWebSocket struct{}

func (f *fakeWebSocket) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func makeProject(t *testing.T) string {
	t.Helper()

	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".mc"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.mc) error = %v", err)
	}

	data := []byte(`{
  "remote": {"mcurl": "https://example.test/api", "email": "user@example.test"},
  "project_id": 1,
  "project_uuid": "project-uuid"
}
`)

	if err := os.WriteFile(filepath.Join(projectRoot, ".mc", "config.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	return projectRoot
}

func writeFile(t *testing.T, filePath string, content string) {
	t.Helper()

	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", filePath, err)
	}
}

func openStore(t *testing.T, ctx context.Context, projectRoot string) *filedb.Store {
	t.Helper()

	store, err := filedb.Open(ctx, projectRoot)
	if err != nil {
		t.Fatalf("filedb.Open() error = %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close(ctx)
	})

	return store
}

func remoteFile(remotePath string, id int, size int64) mcmodel.File {
	return mcmodel.File{
		ID:        id,
		Name:      path.Base(remotePath),
		Path:      remotePath,
		Size:      uint64(size),
		MimeType:  "text/plain",
		Checksum:  "remote-checksum",
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	}
}
