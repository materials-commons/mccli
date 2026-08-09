package projectpath

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalToRemoteProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()

	got, err := LocalToRemote(projectRoot, projectRoot)
	if err != nil {
		t.Fatalf("LocalToRemote() error = %v", err)
	}

	if got != "/" {
		t.Fatalf("LocalToRemote() = %q, want /", got)
	}
}

func TestLocalToRemoteNestedPath(t *testing.T) {
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "Dir1", "file.txt")

	got, err := LocalToRemote(projectRoot, localPath)
	if err != nil {
		t.Fatalf("LocalToRemote() error = %v", err)
	}

	if got != "/Dir1/file.txt" {
		t.Fatalf("LocalToRemote() = %q, want /Dir1/file.txt", got)
	}
}

func TestLocalToRemoteOutsideProject(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "project")
	outside := filepath.Join(t.TempDir(), "outside.txt")

	got, err := LocalToRemote(projectRoot, outside)
	if !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("LocalToRemote() = %q, error = %v, want ErrPathOutsideProject", got, err)
	}
}

func TestRemoteToLocalProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()

	got, err := RemoteToLocal(projectRoot, "/")
	if err != nil {
		t.Fatalf("RemoteToLocal() error = %v", err)
	}

	want, err := filepath.Abs(projectRoot)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	if got != want {
		t.Fatalf("RemoteToLocal() = %q, want %q", got, want)
	}
}

func TestRemoteToLocalNestedPath(t *testing.T) {
	projectRoot := t.TempDir()

	got, err := RemoteToLocal(projectRoot, "/Dir1/file.txt")
	if err != nil {
		t.Fatalf("RemoteToLocal() error = %v", err)
	}

	want := filepath.Join(projectRoot, "Dir1", "file.txt")
	if got != want {
		t.Fatalf("RemoteToLocal() = %q, want %q", got, want)
	}
}

func TestRemoteToLocalRequiresAbsoluteRemotePath(t *testing.T) {
	got, err := RemoteToLocal(t.TempDir(), "Dir1/file.txt")
	if !errors.Is(err, ErrInvalidRemotePath) {
		t.Fatalf("RemoteToLocal() = %q, error = %v, want ErrInvalidRemotePath", got, err)
	}
}

func TestNormalizeRemoteCleansPath(t *testing.T) {
	got, err := NormalizeRemote("/Dir1/../Dir2//file.txt")
	if err != nil {
		t.Fatalf("NormalizeRemote() error = %v", err)
	}

	if got != "/Dir2/file.txt" {
		t.Fatalf("NormalizeRemote() = %q, want /Dir2/file.txt", got)
	}
}

func TestFindRootFromNestedDirectory(t *testing.T) {
	ctx := context.Background()
	projectRoot := filepath.Join(t.TempDir(), "Aging")
	nested := filepath.Join(projectRoot, "Dir1", "Subdir")

	if err := os.MkdirAll(filepath.Join(projectRoot, ProjectConfigDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(.mc) error = %v", err)
	}
	if err := os.WriteFile(ConfigPath(projectRoot), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(nested) error = %v", err)
	}

	got, err := FindRoot(ctx, nested)
	if err != nil {
		t.Fatalf("FindRoot() error = %v", err)
	}

	if got != projectRoot {
		t.Fatalf("FindRoot() = %q, want %q", got, projectRoot)
	}
}

func TestFindRootFromFile(t *testing.T) {
	ctx := context.Background()
	projectRoot := filepath.Join(t.TempDir(), "Aging")
	filePath := filepath.Join(projectRoot, "Dir1", "file.txt")

	if err := os.MkdirAll(filepath.Join(projectRoot, ProjectConfigDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(.mc) error = %v", err)
	}
	if err := os.WriteFile(ConfigPath(projectRoot), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(file dir) error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
		t.Fatalf("WriteFile(file) error = %v", err)
	}

	got, err := FindRoot(ctx, filePath)
	if err != nil {
		t.Fatalf("FindRoot() error = %v", err)
	}

	if got != projectRoot {
		t.Fatalf("FindRoot() = %q, want %q", got, projectRoot)
	}
}

func TestFindRootNoProject(t *testing.T) {
	_, err := FindRoot(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNoProject) {
		t.Fatalf("FindRoot() error = %v, want ErrNoProject", err)
	}
}

func TestExists(t *testing.T) {
	ctx := context.Background()
	projectRoot := filepath.Join(t.TempDir(), "Aging")

	if err := os.MkdirAll(filepath.Join(projectRoot, ProjectConfigDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(.mc) error = %v", err)
	}
	if err := os.WriteFile(ConfigPath(projectRoot), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	exists, err := Exists(ctx, projectRoot)
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Fatal("Exists() = false, want true")
	}

	exists, err = Exists(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Exists() outside project error = %v", err)
	}
	if exists {
		t.Fatal("Exists() outside project = true, want false")
	}
}
