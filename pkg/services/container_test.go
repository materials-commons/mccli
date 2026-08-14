package services

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/download"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/upload"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

func TestContainerLoadCommandContextDoesNotInitializeCommandSpecificServices(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeServiceTestProject(t)

	var uploadCalls int
	var downloadCalls int
	var websocketCalls int
	var storeCalls int
	var remoteCalls int

	container := NewContainer(di.Dependencies{
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
			storeCalls++
			return fakeStore{}, nil
		},
		NewRemote: func(project config.Project, global config.Global) (di.RemoteClient, error) {
			remoteCalls++
			return &fakeRemote{}, nil
		},
		NewUploadManager: func(cfg upload.Config) (di.UploadManager, error) {
			uploadCalls++
			return fakeUploadManager{}, nil
		},
		NewDownloadManager: func(cfg download.Config) (di.DownloadManager, error) {
			downloadCalls++
			return fakeDownloadManager{}, nil
		},
		NewWebSocket: func(cfg di.WebSocketConfig) di.WebSocketRunner {
			websocketCalls++
			return fakeWebSocket{}
		},
	})

	_, err := container.LoadCommandContext(ctx, projectRoot)
	if err != nil {
		t.Fatalf("LoadCommandContext() error = %v", err)
	}

	if storeCalls != 0 {
		t.Fatalf("storeCalls = %d, want 0", storeCalls)
	}
	if remoteCalls != 0 {
		t.Fatalf("remoteCalls = %d, want 0", remoteCalls)
	}
	if uploadCalls != 0 {
		t.Fatalf("uploadCalls = %d, want 0", uploadCalls)
	}
	if downloadCalls != 0 {
		t.Fatalf("downloadCalls = %d, want 0", downloadCalls)
	}
	if websocketCalls != 0 {
		t.Fatalf("websocketCalls = %d, want 0", websocketCalls)
	}
}

func TestContainerStoreIsLazyAndCached(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()

	var storeCalls int

	container := NewContainer(di.Dependencies{
		LoadProject: func(ctx context.Context, start string) (config.Project, error) {
			return config.Project{ProjectID: 10}, nil
		},
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{}, nil
		},
		OpenStore: func(ctx context.Context, projectRoot string) (di.Store, error) {
			storeCalls++
			return fakeStore{}, nil
		},
	})

	container.projectRoot = projectRoot

	first, err := container.Store(ctx)
	if err != nil {
		t.Fatalf("first Store() error = %v", err)
	}

	second, err := container.Store(ctx)
	if err != nil {
		t.Fatalf("second Store() error = %v", err)
	}

	if first != second {
		t.Fatal("Store() returned different cached instances")
	}
	if storeCalls != 1 {
		t.Fatalf("storeCalls = %d, want 1", storeCalls)
	}
}

func TestRequireConfiguredRemoteRejectsMissingAPIKey(t *testing.T) {
	_, err := RequireConfiguredRemote(
		config.Project{
			Remote: config.Remote{
				Email: "user@example.test",
				MCURL: "https://example.test/api",
			},
		},
		config.Global{
			DefaultRemote: config.Remote{
				Email: "user@example.test",
				MCURL: "https://example.test/api",
			},
		},
	)
	if err == nil {
		t.Fatal("RequireConfiguredRemote() error = nil, want error")
	}
}

func makeServiceTestProject(t *testing.T) string {
	t.Helper()

	projectRoot := t.TempDir()
	configDir := filepath.Join(projectRoot, ".mc")

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(.mc) error = %v", err)
	}

	data := []byte(`{
  "remote": {
    "mcurl": "https://example.test/api",
    "email": "user@example.test"
  },
  "project_id": 10,
  "project_uuid": "project-uuid"
}
`)

	if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(config.json) error = %v", err)
	}

	return projectRoot
}

type fakeStore struct{}

func (fakeStore) Close(ctx context.Context) error {
	return nil
}

func (fakeStore) Upsert(ctx context.Context, record filedb.FileRecord) error {
	return nil
}

func (fakeStore) GetByPath(ctx context.Context, path string) (filedb.FileRecord, error) {
	return filedb.FileRecord{}, filedb.ErrRecordNotFound
}

func (fakeStore) ListByDir(ctx context.Context, dir string) ([]filedb.FileRecord, error) {
	return nil, nil
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

func (f *fakeRemote) ListDirectoryByPath(projectID int, remotePath string) ([]mcmodel.File, error) {
	return nil, fakeNotFound()
}

func fakeNotFound() error {
	return &mcapi.APIError{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
	}
}

type fakeUploadManager struct{}

func (fakeUploadManager) StartWorkers(ctx context.Context) {}

func (fakeUploadManager) StopWorkers() {}

func (fakeUploadManager) QueueUpload(req upload.Request) (string, error) {
	return "upload-1", nil
}

func (fakeUploadManager) HandleMessage(msg wsclient.TextMessage) {}

func (fakeUploadManager) Result(transferID string) (upload.Result, bool) {
	return upload.Result{Success: true}, true
}

type fakeDownloadManager struct{}

func (fakeDownloadManager) StartWorkers(ctx context.Context) {}

func (fakeDownloadManager) StopWorkers() {}

func (fakeDownloadManager) QueueDownload(req download.Request) (string, error) {
	return "download-1", nil
}

func (fakeDownloadManager) Result(transferID string) (download.Result, bool) {
	return download.Result{Success: true}, true
}

type fakeWebSocket struct{}

func (fakeWebSocket) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
