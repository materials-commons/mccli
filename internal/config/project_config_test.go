package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
