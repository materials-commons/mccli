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
	// Default remote. This is the remote configured under default_remote is
	// what will be used by default.
	DefaultRemote Remote `json:"default_remote"`

	// List of other remotes that are configured. This is mostly used (right now) for
	// local testing.
	Remotes []Remote `json:"remotes"`

	// Though we don't support Globus in mc2, this is preserved for future use.
	Globus        Globus `json:"globus"`
	DeveloperMode bool   `json:"developer_mode"`
	RESTLogging   bool   `json:"REST_logging"`

	// The ClientUUID configured for the server.
	ClientUUID string `json:"client_uuid"`

	path string
}

// Path returns the file path this global configuration was loaded from or saved
// to. It is intentionally excluded from JSON.
func (g Global) Path() string {
	return g.path
}

// FindRemote returns the configured remote matching email and mcurl. Returns
// true if the remote was found, false otherwise.
func (g Global) FindRemote(email, mcurl string) (Remote, bool) {
	target := Remote{
		MCURL: mcurl,
		Email: email,
	}

	// First check if the default remote matches the target.
	if g.DefaultRemote.Matches(target) {
		return g.DefaultRemote, true
	}

	// If we are here then the default remote did not match. Check any other
	// remotes that are configured.
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

	// Attempt to read the config file. Distinguish between a file not existing and other errors.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Global{}, fmt.Errorf("%w: %s", ErrConfigNotFound, path)
		}
		return Global{}, fmt.Errorf("read global config %q: %w", path, err)
	}

	// Unmarshal the config from JSON.
	var cfg Global
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Global{}, fmt.Errorf("%w: decode global config %q: %w", ErrInvalidConfig, path, err)
	}

	// Save the path to the config. This field is not exported.
	cfg.path = path
	return cfg, nil
}

// SaveGlobal writes cfg to path as JSON. SaveGlobal will create the config directory
// if it does not exist. SaveGlobal determines the path in a few different ways. If
// the path parameter is not blank then it will use that path. If path is blank, then
// it will use the path set in Global. If this is blank then it will use the default
// path ($HOME/.materialscommons/config.json).
func SaveGlobal(ctx context.Context, cfg Global, path string) error {
	// Use cfg.path if path is blank.
	if path == "" {
		path = cfg.path
	}

	// If path is still blank, then use the default path.
	if path == "" {
		defaultPath, err := DefaultGlobalConfigPath()
		if err != nil {
			return err
		}
		path = defaultPath
	}

	logger := mclogging.Logger(ctx)
	logger.Debug("saving global config", "path", path)

	// Create the config directory if it does not exist.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create global config directory %q: %w", filepath.Dir(path), err)
	}

	// Marshal the config into JSON. Format it so that it can be easier for users to read.
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode global config: %w", ErrInvalidConfig, err)
	}

	data = append(data, '\n')

	// The config file is only readable by the user. This is set because the config contains the users
	// APIKey which is secret and shouldn't be shared.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write global config %q: %w", path, err)
	}

	return nil
}
