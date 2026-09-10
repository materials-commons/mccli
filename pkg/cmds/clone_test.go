package cmds

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"github.com/materials-commons/mccli/pkg/remote"
)

type fakeProjectGetter struct {
	project *mcmodel.Project
	err     error
}

func (f *fakeProjectGetter) GetProject(id int) (*mcmodel.Project, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.project, nil
}

func testDeps(getter remote.ProjectGetter, getterErr error) di.Dependencies {
	return di.Dependencies{
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{
				DefaultRemote: config.Remote{
					MCURL:  "https://example.test/api",
					Email:  "user@example.test",
					APIKey: "secret-key",
				},
			}, nil
		},
		NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
			if getterErr != nil {
				return nil, getterErr
			}
			return getter, nil
		},
	}
}

func TestRunner_Run_Success(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	projectName := "Test Project / Spec: 1"
	expectedDirName := projectpath.CleanProjectDirName(projectName)
	projectID := 42

	getter := &fakeProjectGetter{
		project: &mcmodel.Project{
			ID:   projectID,
			Name: projectName,
		},
	}

	r := &cloneRunner{deps: testDeps(getter, nil)}

	err := r.Run(ctx, projectID)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	// Verify project directory and .mc directory exist
	projectDir := filepath.Join(workDir, expectedDirName)
	mcDir := filepath.Join(projectDir, ".mc")
	if info, err := os.Stat(mcDir); err != nil || !info.IsDir() {
		t.Fatalf(".mc directory does not exist or is not a directory: %v", err)
	}

	// Verify project config
	projConfig, err := config.LoadProject(ctx, projectDir)
	if err != nil {
		t.Fatalf("config.LoadProject() error = %v", err)
	}
	if projConfig.ProjectID != projectID {
		t.Fatalf("projConfig.ProjectID = %d, want %d", projConfig.ProjectID, projectID)
	}

	// Verify mc2.sqlite filedb database was initialized and can be opened
	dbPath := filepath.Join(mcDir, "mc2.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("mc2.sqlite database file not found: %v", err)
	}

	s, err := filedb.Open(ctx, projectDir)
	if err != nil {
		t.Fatalf("filedb.Open() error = %v", err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatalf("s.Close() error = %v", err)
	}
}

func TestRunner_Run_LoadGlobalError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	expectedErr := errors.New("failed to load global config")
	deps := di.Dependencies{
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{}, expectedErr
		},
	}

	r := &cloneRunner{deps: deps}
	err := r.Run(ctx, 1)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run() error = %v, want %v", err, expectedErr)
	}
}

func TestRunner_Run_NewDefaultRemoteClientError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	expectedErr := errors.New("client creation error")
	r := &cloneRunner{deps: testDeps(nil, expectedErr)}

	err := r.Run(ctx, 1)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run() error = %v, want %v", err, expectedErr)
	}
}

func TestRunner_Run_RemoteClientNotProjectGetter(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	deps := di.Dependencies{
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{}, nil
		},
		NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
			// Return an object that does not implement remote.ProjectGetter
			return struct{}{}, nil
		},
	}

	r := &cloneRunner{deps: deps}
	err := r.Run(ctx, 1)
	if err == nil || err.Error() != "remote client is not a ProjectGetter" {
		t.Fatalf("Run() error = %v, want 'remote client is not a ProjectGetter'", err)
	}
}

func TestRunner_Run_GetProjectError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	expectedErr := errors.New("get project failed")
	getter := &fakeProjectGetter{err: expectedErr}
	r := &cloneRunner{deps: testDeps(getter, nil)}

	err := r.Run(ctx, 1)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run() error = %v, want %v", err, expectedErr)
	}
}

func TestRunner_Run_MkdirError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	projectName := "ConflictingFileProject"
	expectedDirName := projectpath.CleanProjectDirName(projectName)

	// Create a regular file with the same name as the project directory
	if err := os.WriteFile(filepath.Join(workDir, expectedDirName), []byte("conflict"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	getter := &fakeProjectGetter{
		project: &mcmodel.Project{
			ID:   10,
			Name: projectName,
		},
	}

	r := &cloneRunner{deps: testDeps(getter, nil)}
	err := r.Run(ctx, 10)
	if err == nil {
		t.Fatal("Run() error = nil, want error due to existing file conflict")
	}
}

func TestRunner_Run_SaveProjectError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	projectName := "ProjectWithConfigDirConflict"
	expectedDirName := projectpath.CleanProjectDirName(projectName)

	// Pre-create .mc/config.json as a directory so SaveProject fails
	configPath := filepath.Join(workDir, expectedDirName, ".mc", "config.json")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	getter := &fakeProjectGetter{
		project: &mcmodel.Project{
			ID:   15,
			Name: projectName,
		},
	}

	r := &cloneRunner{deps: testDeps(getter, nil)}
	err := r.Run(ctx, 15)
	if err == nil {
		t.Fatal("Run() error = nil, want error when SaveProject fails")
	}
}

func TestRunner_Run_OpenDBError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	projectName := "ProjectWithDBDirConflict"
	expectedDirName := projectpath.CleanProjectDirName(projectName)

	// Pre-create .mc/mc2.sqlite as a directory so filedb.Open fails
	dbPath := filepath.Join(workDir, expectedDirName, ".mc", "mc2.sqlite")
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	getter := &fakeProjectGetter{
		project: &mcmodel.Project{
			ID:   20,
			Name: projectName,
		},
	}

	r := &cloneRunner{deps: testDeps(getter, nil)}
	err := r.Run(ctx, 20)
	if err == nil {
		t.Fatal("Run() error = nil, want error when filedb.Open fails")
	}
}

func TestRunner_getRemoteClient(t *testing.T) {
	cfg := config.Global{}

	t.Run("success", func(t *testing.T) {
		getter := &fakeProjectGetter{}
		r := &cloneRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return getter, nil
				},
			},
		}

		client, err := r.getRemoteClient(cfg)
		if err != nil {
			t.Fatalf("getRemoteClient() error = %v, want nil", err)
		}
		if client != getter {
			t.Fatalf("getRemoteClient() client = %v, want %v", client, getter)
		}
	})

	t.Run("client creation error", func(t *testing.T) {
		expectedErr := errors.New("new client failed")
		r := &cloneRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return nil, expectedErr
				},
			},
		}

		_, err := r.getRemoteClient(cfg)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("getRemoteClient() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("not ProjectGetter", func(t *testing.T) {
		r := &cloneRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return "invalid", nil
				},
			},
		}

		_, err := r.getRemoteClient(cfg)
		if err == nil || err.Error() != "remote client is not a ProjectGetter" {
			t.Fatalf("getRemoteClient() error = %v, want 'remote client is not a ProjectGetter'", err)
		}
	})
}

func TestRun_Production(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	// Set HOME to empty temp directory so LoadGlobal fails with no config found
	t.Setenv("HOME", t.TempDir())

	err := RunCloneCmd(ctx, 1)
	if err == nil {
		t.Fatal("Run() error = nil, want error when global config is not found")
	}
}
