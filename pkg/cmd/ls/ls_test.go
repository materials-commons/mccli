package ls

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/filedb"
)

func TestRunnerListsMergedDirectory(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	writeFile(t, filepath.Join(projectRoot, "local.txt"), "local")

	store := openStore(t, ctx, projectRoot)

	remoteID := 22
	remote := &fakeRemote{
		dirs: map[string][]mcmodel.File{
			"/": {
				remoteFile("/remote.txt", remoteID, 2048),
			},
		},
		files: map[string]mcmodel.File{
			"/remote.txt": remoteFile("/remote.txt", remoteID, 2048),
		},
	}

	var out bytes.Buffer
	runner := Runner{Deps: testDeps(projectRoot, store, remote)}

	err := runner.Run(ctx, Options{
		WorkingDir: projectRoot,
		Paths:      []string{"."},
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"l_updated_at",
		"r_updated_at",
		"local.txt",
		"remote.txt",
		"2K",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRunnerActionOutput(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	writeFile(t, filepath.Join(projectRoot, "local.txt"), "local")

	store := openStore(t, ctx, projectRoot)
	remote := &fakeRemote{
		dirs:  map[string][]mcmodel.File{"/": nil},
		files: map[string]mcmodel.File{},
	}

	var out bytes.Buffer
	runner := Runner{Deps: testDeps(projectRoot, store, remote)}

	err := runner.Run(ctx, Options{
		WorkingDir: projectRoot,
		Paths:      []string{"."},
		Action:     true,
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"name",
		"local/remote",
		"action",
		"reason",
		"local.txt",
		"L",
		"upload",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRunnerListsSingleFile(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)
	writeFile(t, filepath.Join(projectRoot, "file.txt"), "local")

	store := openStore(t, ctx, projectRoot)
	remote := &fakeRemote{
		dirs:  map[string][]mcmodel.File{},
		files: map[string]mcmodel.File{},
	}

	var out bytes.Buffer
	runner := Runner{Deps: testDeps(projectRoot, store, remote)}

	err := runner.Run(ctx, Options{
		WorkingDir: projectRoot,
		Paths:      []string{"file.txt"},
		Action:     true,
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "file.txt") {
		t.Fatalf("output missing file name:\n%s", got)
	}
	if !strings.Contains(got, "upload") {
		t.Fatalf("output missing upload action:\n%s", got)
	}
}

func TestRunnerPrintsMissingPath(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeProject(t)

	store := openStore(t, ctx, projectRoot)
	remote := &fakeRemote{
		dirs:  map[string][]mcmodel.File{},
		files: map[string]mcmodel.File{},
	}

	var out bytes.Buffer
	runner := Runner{Deps: testDeps(projectRoot, store, remote)}

	err := runner.Run(ctx, Options{
		WorkingDir: projectRoot,
		Paths:      []string{"missing.txt"},
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out.String(), "missing.txt: No such file or directory") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func testDeps(projectRoot string, store RecordStore, remote RemoteClient) Dependencies {
	return Dependencies{
		LoadProject: func(ctx context.Context, start string) (config.Project, error) {
			return config.LoadProject(ctx, projectRoot)
		},
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{
				DefaultRemote: config.Remote{
					MCURL:  "https://example.test/api",
					Email:  "user@example.test",
					APIKey: "apikey",
				},
			}, nil
		},
		OpenStore: func(ctx context.Context, root string) (RecordStore, error) {
			return store, nil
		},
		NewRemote: func(project config.Project, global config.Global) (RemoteClient, error) {
			return remote, nil
		},
		Now: func() time.Time {
			return time.Unix(100, 0)
		},
	}
}

type fakeRemote struct {
	dirs  map[string][]mcmodel.File
	files map[string]mcmodel.File
}

func (f *fakeRemote) ListDirectoryByPath(projectID int, remotePath string) ([]mcmodel.File, error) {
	if files, ok := f.dirs[remotePath]; ok {
		return files, nil
	}
	return nil, fakeNotFound()
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

func makeProject(t *testing.T) string {
	t.Helper()

	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".mc"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.mc) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".mc", "config.json"), []byte(`{
  "remote": {"mcurl": "https://example.test/api", "email": "user@example.test"},
  "project_id": 1,
  "project_uuid": "project-uuid"
}
`), 0o644); err != nil {
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
		if err := store.Close(ctx); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Fatalf("store.Close() error = %v", err)
		}
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
