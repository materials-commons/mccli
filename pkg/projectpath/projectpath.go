// Package projectpath translates between local filesystem paths and Materials
// Commons remote project paths.
//
// Materials Commons project paths always use slash separators and always start
// with "/". The local project root maps to the remote path "/".
//
// For example, if the local project root is:
//
//	/home/gtarcea/projs/Aging
//
// then:
//
//	/home/gtarcea/projs/Aging              -> /
//	/home/gtarcea/projs/Aging/Dir1/file.txt -> /Dir1/file.txt
//
// and:
//
//	/Dir1/file.txt -> /home/gtarcea/projs/Aging/Dir1/file.txt
package projectpath

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	mclogging "github.com/materials-commons/mccli/pkg/logging"
)

const (
	// ProjectConfigDirName is the local project metadata directory name.
	ProjectConfigDirName = ".mc"

	// ConfigFileName is the JSON file name used for local project
	// configuration.
	ConfigFileName = "config.json"
)

var (
	// ErrNoProject indicates that no local Materials Commons project could be
	// found at or above the requested path.
	ErrNoProject = errors.New("no Materials Commons project found")

	// ErrPathOutsideProject indicates that a local path is not contained by the
	// local project root.
	ErrPathOutsideProject = errors.New("path is outside project")

	// ErrInvalidRemotePath indicates that a Materials Commons remote path is
	// malformed.
	ErrInvalidRemotePath = errors.New("invalid remote project path")
)

// Translator translates paths for one local Materials Commons project.
type Translator struct {
	projectRoot string
}

// New constructs a Translator for projectRoot.
//
// projectRoot is converted to an absolute, cleaned local path.
func New(projectRoot string) (Translator, error) {
	if projectRoot == "" {
		return Translator{}, fmt.Errorf("project root is required")
	}

	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return Translator{}, fmt.Errorf("resolve project root %q: %w", projectRoot, err)
	}

	return Translator{
		projectRoot: filepath.Clean(absRoot),
	}, nil
}

// ProjectRoot returns the absolute local project root.
func (t Translator) ProjectRoot() string {
	return t.projectRoot
}

// LocalToRemote converts a local filesystem path to a Materials Commons remote
// project path.
//
// The returned remote path always starts with "/". The project root itself maps
// to "/".
func (t Translator) LocalToRemote(localPath string) (string, error) {
	if t.projectRoot == "" {
		return "", fmt.Errorf("project root is required")
	}
	if localPath == "" {
		localPath = "."
	}

	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return "", fmt.Errorf("resolve local path %q: %w", localPath, err)
	}
	absLocal = filepath.Clean(absLocal)

	rel, err := filepath.Rel(t.projectRoot, absLocal)
	if err != nil {
		return "", fmt.Errorf("make %q relative to project root %q: %w", absLocal, t.projectRoot, err)
	}

	if rel == "." {
		return "/", nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s is not under %s", ErrPathOutsideProject, absLocal, t.projectRoot)
	}

	return "/" + filepath.ToSlash(rel), nil
}

// RemoteToLocal converts a Materials Commons remote project path to a local
// filesystem path beneath the project root.
//
// The remote project root "/" maps to the local project root.
func (t Translator) RemoteToLocal(remotePath string) (string, error) {
	if t.projectRoot == "" {
		return "", fmt.Errorf("project root is required")
	}

	cleanRemote, err := NormalizeRemote(remotePath)
	if err != nil {
		return "", err
	}

	if cleanRemote == "/" {
		return t.projectRoot, nil
	}

	relativeRemote := strings.TrimPrefix(cleanRemote, "/")
	localPath := filepath.Join(t.projectRoot, filepath.FromSlash(relativeRemote))

	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return "", fmt.Errorf("resolve translated local path %q: %w", localPath, err)
	}

	rel, err := filepath.Rel(t.projectRoot, absLocal)
	if err != nil {
		return "", fmt.Errorf("validate translated local path %q: %w", absLocal, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: remote path %q resolves outside %s", ErrInvalidRemotePath, remotePath, t.projectRoot)
	}

	return filepath.Clean(absLocal), nil
}

// NormalizeRemote validates and normalizes a Materials Commons remote project
// path.
//
// Remote paths must be absolute slash paths. Empty paths are invalid. The
// returned path is cleaned and always starts with "/".
func NormalizeRemote(remotePath string) (string, error) {
	if remotePath == "" {
		return "", fmt.Errorf("%w: path is empty", ErrInvalidRemotePath)
	}
	if !strings.HasPrefix(remotePath, "/") {
		return "", fmt.Errorf("%w: %q must start with /", ErrInvalidRemotePath, remotePath)
	}

	cleanRemote := path.Clean(remotePath)
	if cleanRemote == "." {
		return "/", nil
	}
	if !strings.HasPrefix(cleanRemote, "/") {
		return "", fmt.Errorf("%w: %q", ErrInvalidRemotePath, remotePath)
	}

	return cleanRemote, nil
}

// FindRoot walks upward from start until it finds a directory containing
// .mc/config.json.
//
// start may be either a file or directory path. If start does not exist,
// FindRoot still walks from start itself, which is useful for commands that are
// validating paths that may be created later.
func FindRoot(ctx context.Context, start string) (string, error) {
	if start == "" {
		start = "."
	}

	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve project search path %q: %w", start, err)
	}

	info, err := os.Stat(abs)
	if err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat project search path %q: %w", abs, err)
	}

	logger := mclogging.Logger(ctx)
	logger.Debug("searching for project config", "start", abs)

	for {
		configPath := ConfigPath(abs)
		info, err := os.Stat(configPath)
		if err == nil && !info.IsDir() {
			logger.Debug("found project config", "project_root", abs, "path", configPath)
			return abs, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat project config %q: %w", configPath, err)
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("%w at or above %s", ErrNoProject, start)
		}

		abs = parent
	}
}

// Exists reports whether start is inside a Materials Commons project.
func Exists(ctx context.Context, start string) (bool, error) {
	_, err := FindRoot(ctx, start)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNoProject) {
		return false, nil
	}
	return false, err
}

// ConfigPath returns $PROJECT/.mc/config.json.
func ConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ProjectConfigDirName, ConfigFileName)
}

// ConfigDir returns $PROJECT/.mc.
func ConfigDir(projectRoot string) string {
	return filepath.Join(projectRoot, ProjectConfigDirName)
}

// LocalToRemote converts localPath to a remote path using projectRoot.
func LocalToRemote(projectRoot, localPath string) (string, error) {
	translator, err := New(projectRoot)
	if err != nil {
		return "", err
	}

	return translator.LocalToRemote(localPath)
}

// RemoteToLocal converts remotePath to a local path using projectRoot.
func RemoteToLocal(projectRoot, remotePath string) (string, error) {
	translator, err := New(projectRoot)
	if err != nil {
		return "", err
	}

	return translator.RemoteToLocal(remotePath)
}
