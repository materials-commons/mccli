package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGlobal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	const input = `{
  "default_remote": {
    "mcurl": "https://spelljammer/api",
    "email": "gtarcea@umich.edu",
    "mcapikey": "secret"
  },
  "remotes": [
    {
      "mcurl": "https://spelljammer/api",
      "email": "gtarcea@umich.edu",
      "mcapikey": "secret"
    }
  ],
  "globus": {
    "transfer_rt": null,
    "endpoint_id": null
  },
  "developer_mode": false,
  "REST_logging": false,
  "client_uuid": "bd6f019f-d262-48d4-8a0c-b13d6465d887"
}`

	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadGlobal(ctx, path)
	if err != nil {
		t.Fatalf("LoadGlobal() error = %v", err)
	}

	if cfg.Path() != path {
		t.Fatalf("Path() = %q, want %q", cfg.Path(), path)
	}
	if cfg.DefaultRemote.MCURL != "https://spelljammer/api" {
		t.Fatalf("DefaultRemote.MCURL = %q", cfg.DefaultRemote.MCURL)
	}
	if cfg.DefaultRemote.Email != "gtarcea@umich.edu" {
		t.Fatalf("DefaultRemote.Email = %q", cfg.DefaultRemote.Email)
	}
	if cfg.DefaultRemote.APIKey != "secret" {
		t.Fatalf("DefaultRemote.APIKey = %q", cfg.DefaultRemote.APIKey)
	}
	if len(cfg.Remotes) != 1 {
		t.Fatalf("len(Remotes) = %d, want 1", len(cfg.Remotes))
	}
	if cfg.Globus.TransferRefreshToken != nil {
		t.Fatalf("Globus.TransferRefreshToken = %q, want nil", *cfg.Globus.TransferRefreshToken)
	}
	if cfg.Globus.EndpointID != nil {
		t.Fatalf("Globus.EndpointID = %q, want nil", *cfg.Globus.EndpointID)
	}
	if cfg.ClientUUID != "bd6f019f-d262-48d4-8a0c-b13d6465d887" {
		t.Fatalf("ClientUUID = %q", cfg.ClientUUID)
	}
}

func TestSaveGlobal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, ".materialscommons", "config.json")

	cfg := Global{
		DefaultRemote: Remote{
			MCURL:  "https://spelljammer/api",
			Email:  "gtarcea@umich.edu",
			APIKey: "secret",
		},
		Remotes: []Remote{
			{
				MCURL:  "https://spelljammer/api",
				Email:  "gtarcea@umich.edu",
				APIKey: "secret",
			},
		},
		ClientUUID: "bd6f019f-d262-48d4-8a0c-b13d6465d887",
	}

	if err := SaveGlobal(ctx, cfg, path); err != nil {
		t.Fatalf("SaveGlobal() error = %v", err)
	}

	loaded, err := LoadGlobal(ctx, path)
	if err != nil {
		t.Fatalf("LoadGlobal() error = %v", err)
	}

	if loaded.DefaultRemote.APIKey != "secret" {
		t.Fatalf("DefaultRemote.APIKey = %q", loaded.DefaultRemote.APIKey)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("global config permissions = %v, want 0600", got)
	}
}

func TestLoadGlobalNotFound(t *testing.T) {
	_, err := LoadGlobal(context.Background(), filepath.Join(t.TempDir(), "missing.json"))
	if !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("LoadGlobal() error = %v, want ErrConfigNotFound", err)
	}
}

func TestFindRemote(t *testing.T) {
	cfg := Global{
		DefaultRemote: Remote{
			MCURL:  "https://default/api",
			Email:  "default@example.com",
			APIKey: "default-key",
		},
		Remotes: []Remote{
			{
				MCURL:  "https://spelljammer/api",
				Email:  "gtarcea@umich.edu",
				APIKey: "secret",
			},
		},
	}

	remote, ok := cfg.FindRemote("gtarcea@umich.edu", "https://spelljammer/api")
	if !ok {
		t.Fatal("FindRemote() ok = false, want true")
	}
	if remote.APIKey != "secret" {
		t.Fatalf("remote.APIKey = %q", remote.APIKey)
	}

	_, ok = cfg.FindRemote("missing@example.com", "https://spelljammer/api")
	if ok {
		t.Fatal("FindRemote() ok = true, want false")
	}
}

func TestLoadProjectFromNestedDirectory(t *testing.T) {
	ctx := context.Background()
	projectRoot := filepath.Join(t.TempDir(), "project")
	nested := filepath.Join(projectRoot, "a", "b", "c")

	if err := os.MkdirAll(filepath.Join(projectRoot, ProjectConfigDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	const input = `{"remote": {"mcurl": "https://spelljammer/api", "email": "gtarcea@umich.edu"}, "project_id": 438, "project_uuid": "ddd3c23a-a85c-4afa-ad3d-3950c63776f0", "experiment_id": null, "experiment_uuid": null, "remote_updatetime": null, "globus_upload_id": null, "globus_download_id": null}`

	configPath := filepath.Join(projectRoot, ProjectConfigDirName, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(input), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadProject(ctx, nested)
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}

	if cfg.ProjectRoot() != projectRoot {
		t.Fatalf("ProjectRoot() = %q, want %q", cfg.ProjectRoot(), projectRoot)
	}
	if cfg.Path() != configPath {
		t.Fatalf("Path() = %q, want %q", cfg.Path(), configPath)
	}
	if cfg.ProjectID != 438 {
		t.Fatalf("ProjectID = %d, want 438", cfg.ProjectID)
	}
	if cfg.ProjectUUID != "ddd3c23a-a85c-4afa-ad3d-3950c63776f0" {
		t.Fatalf("ProjectUUID = %q", cfg.ProjectUUID)
	}
	if cfg.Remote.MCURL != "https://spelljammer/api" {
		t.Fatalf("Remote.MCURL = %q", cfg.Remote.MCURL)
	}
	if cfg.Remote.Email != "gtarcea@umich.edu" {
		t.Fatalf("Remote.Email = %q", cfg.Remote.Email)
	}
	if cfg.Remote.APIKey != "" {
		t.Fatalf("Remote.APIKey = %q, want empty because project config should not store API key", cfg.Remote.APIKey)
	}
}

func TestFindProjectRootNoProject(t *testing.T) {
	_, err := FindProjectRoot(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNoProject) {
		t.Fatalf("FindProjectRoot() error = %v, want ErrNoProject", err)
	}
}

func TestSaveProject(t *testing.T) {
	ctx := context.Background()
	projectRoot := filepath.Join(t.TempDir(), "project")

	cfg := Project{
		Remote: Remote{
			MCURL: "https://spelljammer/api",
			Email: "gtarcea@umich.edu",
		},
		ProjectID:   438,
		ProjectUUID: "ddd3c23a-a85c-4afa-ad3d-3950c63776f0",
	}

	if err := SaveProject(ctx, projectRoot, cfg); err != nil {
		t.Fatalf("SaveProject() error = %v", err)
	}

	loaded, err := LoadProject(ctx, projectRoot)
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}

	if loaded.ProjectRoot() != projectRoot {
		t.Fatalf("ProjectRoot() = %q, want %q", loaded.ProjectRoot(), projectRoot)
	}
	if loaded.ProjectID != 438 {
		t.Fatalf("ProjectID = %d, want 438", loaded.ProjectID)
	}
	if loaded.Remote.APIKey != "" {
		t.Fatalf("Remote.APIKey = %q, want empty", loaded.Remote.APIKey)
	}
}
