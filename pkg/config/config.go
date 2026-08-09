// Package config reads and writes Materials Commons CLI configuration files.
//
// The mc2 command intentionally keeps compatibility with the existing Python
// CLI configuration layout:
//
//   - global config: $HOME/.materialscommons/config.json
//   - project config: $PROJECT/.mc/config.json
//
// Global configuration contains account credentials and remote definitions.
// Project configuration identifies the Materials Commons project associated
// with a local directory tree.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	mclogging "github.com/materials-commons/mccli/pkg/logging"
	"github.com/materials-commons/mccli/pkg/projectpath"
)

const (
	// GlobalConfigDirName is the directory under the user's home directory that
	// stores the global Materials Commons configuration.
	GlobalConfigDirName = ".materialscommons"

	// ConfigFileName is the JSON file name used for both global and project
	// configuration.
	ConfigFileName = projectpath.ConfigFileName

	// ProjectConfigDirName is the local project metadata directory name.
	ProjectConfigDirName = projectpath.ProjectConfigDirName
)

var (
	// ErrNoProject indicates that no local Materials Commons project could be
	// found at or above the requested path.
	ErrNoProject = projectpath.ErrNoProject

	// ErrConfigNotFound indicates that the requested configuration file does
	// not exist.
	ErrConfigNotFound = errors.New("configuration file not found")

	// ErrInvalidConfig indicates that a configuration file exists but is not
	// usable.
	ErrInvalidConfig = errors.New("invalid configuration")
)

// Remote describes one Materials Commons remote account.
//
// The JSON field names intentionally match the existing Python configuration
// format.
type Remote struct {
	MCURL  string `json:"mcurl,omitempty"`
	Email  string `json:"email,omitempty"`
	APIKey string `json:"mcapikey,omitempty"`
}

// Matches reports whether r and other refer to the same configured remote
// account. API keys are intentionally ignored because project configuration
// stores only the remote URL and email.
func (r Remote) Matches(other Remote) bool {
	return r.MCURL == other.MCURL && r.Email == other.Email
}

// Globus describes optional Globus configuration preserved from the Python CLI.
type Globus struct {
	TransferRefreshToken *string `json:"transfer_rt"`
	EndpointID           *string `json:"endpoint_id"`
}

// Global is the user's global Materials Commons configuration.
//
// This is loaded from $HOME/.materialscommons/config.json by default.
type Global struct {
	DefaultRemote Remote   `json:"default_remote"`
	Remotes       []Remote `json:"remotes"`
	Globus        Globus   `json:"globus"`
	DeveloperMode bool     `json:"developer_mode"`
	RESTLogging   bool     `json:"REST_logging"`
	ClientUUID    string   `json:"client_uuid"`

	path string
}

// Path returns the file path this global configuration was loaded from or saved
// to. It is intentionally excluded from JSON.
func (g Global) Path() string {
	return g.path
}

// FindRemote returns the configured remote matching email and mcurl.
func (g Global) FindRemote(email, mcurl string) (Remote, bool) {
	target := Remote{
		MCURL: mcurl,
		Email: email,
	}

	if g.DefaultRemote.Matches(target) {
		return g.DefaultRemote, true
	}

	for _, remote := range g.Remotes {
		if remote.Matches(target) {
			return remote, true
		}
	}

	return Remote{}, false
}

// DefaultGlobalConfigPath returns the default global configuration path:
//
//	$HOME/.materialscommons/config.json
func DefaultGlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home directory: %w", err)
	}

	return filepath.Join(home, GlobalConfigDirName, ConfigFileName), nil
}

// LoadGlobal reads a global configuration file.
//
// If path is empty, LoadGlobal reads $HOME/.materialscommons/config.json.
func LoadGlobal(ctx context.Context, path string) (Global, error) {
	if path == "" {
		defaultPath, err := DefaultGlobalConfigPath()
		if err != nil {
			return Global{}, err
		}
		path = defaultPath
	}

	logger := mclogging.Logger(ctx)
	logger.Debug("loading global config", "path", path)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Global{}, fmt.Errorf("%w: %s", ErrConfigNotFound, path)
		}
		return Global{}, fmt.Errorf("read global config %q: %w", path, err)
	}

	var cfg Global
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Global{}, fmt.Errorf("%w: decode global config %q: %w", ErrInvalidConfig, path, err)
	}

	cfg.path = path
	return cfg, nil
}

// SaveGlobal writes cfg to path as JSON.
//
// If path is empty, SaveGlobal writes to cfg.Path(). If cfg.Path() is also
// empty, SaveGlobal writes $HOME/.materialscommons/config.json.
func SaveGlobal(ctx context.Context, cfg Global, path string) error {
	if path == "" {
		path = cfg.path
	}
	if path == "" {
		defaultPath, err := DefaultGlobalConfigPath()
		if err != nil {
			return err
		}
		path = defaultPath
	}

	logger := mclogging.Logger(ctx)
	logger.Debug("saving global config", "path", path)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create global config directory %q: %w", filepath.Dir(path), err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode global config: %w", ErrInvalidConfig, err)
	}

	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write global config %q: %w", path, err)
	}

	return nil
}

// Project is the local project configuration stored in $PROJECT/.mc/config.json.
type Project struct {
	Remote           Remote   `json:"remote"`
	ProjectID        int      `json:"project_id"`
	ProjectUUID      string   `json:"project_uuid"`
	ExperimentID     *int     `json:"experiment_id"`
	ExperimentUUID   *string  `json:"experiment_uuid"`
	RemoteUpdateTime *float64 `json:"remote_updatetime"`
	GlobusUploadID   *int     `json:"globus_upload_id"`
	GlobusDownloadID *int     `json:"globus_download_id"`

	projectRoot string
	path        string
}

// ProjectRoot returns the local project root directory containing .mc/config.json.
func (p Project) ProjectRoot() string {
	return p.projectRoot
}

// Path returns the path to the local project configuration file.
func (p Project) Path() string {
	return p.path
}

// FindProjectRoot walks upward from start until it finds a directory containing
// .mc/config.json.
//
// start may be either a file or directory path. If start does not exist,
// FindProjectRoot still walks from start itself, which is useful for commands
// that are validating paths that may be created later.
func FindProjectRoot(ctx context.Context, start string) (string, error) {
	return projectpath.FindRoot(ctx, start)
}

// ProjectConfigPath returns the local project configuration path for
// projectRoot.
func ProjectConfigPath(projectRoot string) string {
	return projectpath.ConfigPath(projectRoot)
}

// LoadProject reads the local project configuration associated with start.
//
// start may be the project root or any path beneath the project root.
func LoadProject(ctx context.Context, start string) (Project, error) {
	projectRoot, err := projectpath.FindRoot(ctx, start)
	if err != nil {
		return Project{}, err
	}

	path := projectpath.ConfigPath(projectRoot)
	logger := mclogging.Logger(ctx)
	logger.Debug("loading project config", "project_root", projectRoot, "path", path)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Project{}, fmt.Errorf("%w: %s", ErrConfigNotFound, path)
		}
		return Project{}, fmt.Errorf("read project config %q: %w", path, err)
	}

	var cfg Project
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Project{}, fmt.Errorf("%w: decode project config %q: %w", ErrInvalidConfig, path, err)
	}

	cfg.projectRoot = projectRoot
	cfg.path = path

	if cfg.ProjectID == 0 {
		return Project{}, fmt.Errorf("%w: project_id is required in %s", ErrInvalidConfig, path)
	}
	if cfg.ProjectUUID == "" {
		return Project{}, fmt.Errorf("%w: project_uuid is required in %s", ErrInvalidConfig, path)
	}
	if cfg.Remote.MCURL == "" {
		return Project{}, fmt.Errorf("%w: remote.mcurl is required in %s", ErrInvalidConfig, path)
	}
	if cfg.Remote.Email == "" {
		return Project{}, fmt.Errorf("%w: remote.email is required in %s", ErrInvalidConfig, path)
	}

	return cfg, nil
}

// SaveProject writes cfg to $projectRoot/.mc/config.json.
func SaveProject(ctx context.Context, projectRoot string, cfg Project) error {
	if projectRoot == "" {
		projectRoot = cfg.projectRoot
	}
	if projectRoot == "" {
		return fmt.Errorf("%w: project root is required", ErrInvalidConfig)
	}

	path := projectpath.ConfigPath(projectRoot)

	logger := mclogging.Logger(ctx)
	logger.Debug("saving project config", "project_root", projectRoot, "path", path)

	configDir := filepath.Dir(path)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create project config directory %q: %w", configDir, err)
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("%w: encode project config: %w", ErrInvalidConfig, err)
	}

	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write project config %q: %w", path, err)
	}

	return nil
}
