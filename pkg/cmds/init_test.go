package cmds

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"github.com/materials-commons/mccli/pkg/remote"
)

type fakeProjectCreater struct {
	project     *mcmodel.Project
	lastRequest mcapi.CreateProjectRequest
	err         error
}

func (f *fakeProjectCreater) CreateProject(req mcapi.CreateProjectRequest) (*mcmodel.Project, error) {
	f.lastRequest = req
	if f.err != nil {
		return nil, f.err
	}
	return f.project, nil
}

func testInitDeps(creater remote.ProjectCreater, createrErr error) di.Dependencies {
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
			if createrErr != nil {
				return nil, createrErr
			}
			return creater, nil
		},
	}
}

func TestInitRunner_Run_Success_WithProjectName(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	projectName := "Init Project / Spec: 1"
	expectedDirName := projectpath.CleanProjectDirName(projectName)
	projectID := 101
	description := "A test project description"

	creater := &fakeProjectCreater{
		project: &mcmodel.Project{
			ID:          projectID,
			Name:        projectName,
			Description: description,
		},
	}

	r := &initRunner{deps: testInitDeps(creater, nil)}

	opts := InitOpts{
		ProjectName: projectName,
		Description: description,
	}

	err := r.Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if creater.lastRequest.Name != projectName {
		t.Errorf("CreateProject request Name = %q, want %q", creater.lastRequest.Name, projectName)
	}
	if creater.lastRequest.Description != description {
		t.Errorf("CreateProject request Description = %q, want %q", creater.lastRequest.Description, description)
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

func TestInitRunner_Run_Success_WithoutProjectName(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	expectedProjectName := filepath.Base(workDir)
	expectedDirName := projectpath.CleanProjectDirName(expectedProjectName)
	projectID := 102
	description := "Description without project name"

	creater := &fakeProjectCreater{
		project: &mcmodel.Project{
			ID:          projectID,
			Name:        expectedProjectName,
			Description: description,
		},
	}

	r := &initRunner{deps: testInitDeps(creater, nil)}

	opts := InitOpts{
		ProjectName: "",
		Description: description,
	}

	err := r.Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if creater.lastRequest.Name != expectedProjectName {
		t.Errorf("CreateProject request Name = %q, want %q", creater.lastRequest.Name, expectedProjectName)
	}

	projectDir := filepath.Join(workDir, expectedDirName)
	mcDir := filepath.Join(projectDir, ".mc")
	if info, err := os.Stat(mcDir); err != nil || !info.IsDir() {
		t.Fatalf(".mc directory does not exist or is not a directory: %v", err)
	}
}

func TestInitRunner_Run_ContainsMCDir_CurrentDir(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	// Create .mc directory in current working directory
	if err := os.Mkdir(filepath.Join(workDir, ".mc"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	creater := &fakeProjectCreater{}
	r := &initRunner{deps: testInitDeps(creater, nil)}

	err := r.Run(ctx, InitOpts{ProjectName: "NewProj"})
	if err == nil {
		t.Fatal("Run() error = nil, want error when .mc exists in current dir")
	}
}

func TestInitRunner_Run_ContainsMCDir_SubDir(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	// Create a subdirectory containing .mc
	subDirMC := filepath.Join(workDir, "subdir", ".mc")
	if err := os.MkdirAll(subDirMC, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	creater := &fakeProjectCreater{}
	r := &initRunner{deps: testInitDeps(creater, nil)}

	err := r.Run(ctx, InitOpts{ProjectName: "NewProj"})
	if err == nil {
		t.Fatal("Run() error = nil, want error when .mc exists in subdirectory")
	}
}

func TestInitRunner_Run_ExistingProjectInParentDir(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()

	// Create a .mc/config.json in parent directory to simulate an existing project
	parentMC := filepath.Join(workDir, ".mc")
	if err := os.Mkdir(parentMC, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(parentMC, "config.json"), []byte(`{"project_id": 99}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Switch to a child subdirectory that doesn't have .mc itself
	childDir := filepath.Join(workDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	t.Chdir(childDir)

	creater := &fakeProjectCreater{}
	r := &initRunner{deps: testInitDeps(creater, nil)}

	err := r.Run(ctx, InitOpts{ProjectName: "NewProj"})
	if err == nil || err.Error() != "project already exists" {
		t.Fatalf("Run() error = %v, want 'project already exists'", err)
	}
}

func TestInitRunner_Run_LoadGlobalError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	expectedErr := errors.New("failed to load global config")
	deps := di.Dependencies{
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{}, expectedErr
		},
	}

	r := &initRunner{deps: deps}
	err := r.Run(ctx, InitOpts{ProjectName: "TestProj"})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run() error = %v, want %v", err, expectedErr)
	}
}

func TestInitRunner_Run_NewDefaultRemoteClientError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	expectedErr := errors.New("client creation error")
	r := &initRunner{deps: testInitDeps(nil, expectedErr)}

	err := r.Run(ctx, InitOpts{ProjectName: "TestProj"})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run() error = %v, want %v", err, expectedErr)
	}
}

func TestInitRunner_Run_RemoteClientNotProjectCreater(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	deps := di.Dependencies{
		LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
			return config.Global{}, nil
		},
		NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
			return struct{}{}, nil
		},
	}

	r := &initRunner{deps: deps}
	err := r.Run(ctx, InitOpts{ProjectName: "TestProj"})
	if err == nil || err.Error() != "remote client is not a ProjectGetter" {
		t.Fatalf("Run() error = %v, want 'remote client is not a ProjectGetter'", err)
	}
}

func TestInitRunner_Run_CreateProjectError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	expectedErr := errors.New("create project failed")
	creater := &fakeProjectCreater{err: expectedErr}
	r := &initRunner{deps: testInitDeps(creater, nil)}

	err := r.Run(ctx, InitOpts{ProjectName: "TestProj"})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run() error = %v, want %v", err, expectedErr)
	}
}

func TestInitRunner_Run_CreateLocalProjectError(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	projectName := "ConflictingFileProject"
	expectedDirName := projectpath.CleanProjectDirName(projectName)

	// Create a regular file with the same name as the project directory
	if err := os.WriteFile(filepath.Join(workDir, expectedDirName), []byte("conflict"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	creater := &fakeProjectCreater{
		project: &mcmodel.Project{
			ID:   10,
			Name: projectName,
		},
	}

	r := &initRunner{deps: testInitDeps(creater, nil)}
	err := r.Run(ctx, InitOpts{ProjectName: projectName})
	if err == nil {
		t.Fatal("Run() error = nil, want error due to existing file conflict")
	}
}

func TestInitRunner_projectNameFromOptsOrCurrentDir(t *testing.T) {
	t.Run("opts contains project name", func(t *testing.T) {
		r := &initRunner{}
		name, err := r.projectNameFromOptsOrCurrentDir(InitOpts{ProjectName: "ExplicitName"})
		if err != nil {
			t.Fatalf("projectNameFromOptsOrCurrentDir() error = %v", err)
		}
		if name != "ExplicitName" {
			t.Fatalf("projectNameFromOptsOrCurrentDir() = %q, want %q", name, "ExplicitName")
		}
	})

	t.Run("opts does not contain project name", func(t *testing.T) {
		workDir := t.TempDir()
		t.Chdir(workDir)

		r := &initRunner{}
		name, err := r.projectNameFromOptsOrCurrentDir(InitOpts{})
		if err != nil {
			t.Fatalf("projectNameFromOptsOrCurrentDir() error = %v", err)
		}
		expectedName := filepath.Base(workDir)
		if name != expectedName {
			t.Fatalf("projectNameFromOptsOrCurrentDir() = %q, want %q", name, expectedName)
		}
	})
}

func TestInitRunner_getRemoteClient(t *testing.T) {
	cfg := config.Global{}

	t.Run("success", func(t *testing.T) {
		creater := &fakeProjectCreater{}
		r := &initRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return creater, nil
				},
			},
		}

		client, err := r.getRemoteClient(cfg)
		if err != nil {
			t.Fatalf("getRemoteClient() error = %v, want nil", err)
		}
		if client != creater {
			t.Fatalf("getRemoteClient() client = %v, want %v", client, creater)
		}
	})

	t.Run("client creation error", func(t *testing.T) {
		expectedErr := errors.New("new client failed")
		r := &initRunner{
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

	t.Run("not ProjectCreater", func(t *testing.T) {
		r := &initRunner{
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

func TestInitRunner_containsMCDir(t *testing.T) {
	t.Run("no mc dir", func(t *testing.T) {
		workDir := t.TempDir()
		t.Chdir(workDir)

		r := &initRunner{}
		if err := r.containsMCDir(); err != nil {
			t.Fatalf("containsMCDir() error = %v, want nil", err)
		}
	})

	t.Run("mc in current dir", func(t *testing.T) {
		workDir := t.TempDir()
		t.Chdir(workDir)

		if err := os.Mkdir(filepath.Join(workDir, ".mc"), 0o755); err != nil {
			t.Fatalf("Mkdir() error = %v", err)
		}

		r := &initRunner{}
		if err := r.containsMCDir(); err == nil {
			t.Fatal("containsMCDir() error = nil, want error")
		}
	})

	t.Run("mc in subdir", func(t *testing.T) {
		workDir := t.TempDir()
		t.Chdir(workDir)

		if err := os.MkdirAll(filepath.Join(workDir, "nested", "sub", ".mc"), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		r := &initRunner{}
		if err := r.containsMCDir(); err == nil {
			t.Fatal("containsMCDir() error = nil, want error")
		}
	})
}

func TestRunInitCmd_Production(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	t.Chdir(workDir)

	// Set HOME to empty temp directory so LoadGlobal fails with no config found
	t.Setenv("HOME", t.TempDir())

	err := RunInitCmd(ctx, InitOpts{ProjectName: "TestProj"})
	if err == nil {
		t.Fatal("RunInitCmd() error = nil, want error due to missing config")
	}
}
