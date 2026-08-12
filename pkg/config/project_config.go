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

// LoadProject reads the local project configuration associated with start.
//
// start may be the project root or any path beneath the project root.
func LoadProject(ctx context.Context, start string) (Project, error) {
	// project config will be in $PROJECT/.mc/config.json. We could be
	// in any directory, so search for the project root.
	projectRoot, err := projectpath.FindRoot(ctx, start)
	if err != nil {
		return Project{}, err
	}

	path := projectpath.ConfigPath(projectRoot)
	logger := mclogging.Logger(ctx)
	logger.Debug("loading project config", "project_root", projectRoot, "path", path)

	// Read the file and distinguish between not found and other errors.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Project{}, fmt.Errorf("%w: %s", ErrConfigNotFound, path)
		}
		return Project{}, fmt.Errorf("read project config %q: %w", path, err)
	}

	// Unmarshal the data into a Project struct.
	var cfg Project
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Project{}, fmt.Errorf("%w: decode project config %q: %w", ErrInvalidConfig, path, err)
	}

	cfg.projectRoot = projectRoot
	cfg.path = path

	if cfg.ProjectID == 0 {
		return Project{}, fmt.Errorf("%w: project_id is required in %s", ErrInvalidConfig, path)
	}

	// Right now none of these fields are used. They exist for future use. So we comment out the code
	// for now.
	//if cfg.ProjectUUID == "" {
	//	return Project{}, fmt.Errorf("%w: project_uuid is required in %s", ErrInvalidConfig, path)
	//}
	//if cfg.Remote.MCURL == "" {
	//	return Project{}, fmt.Errorf("%w: remote.mcurl is required in %s", ErrInvalidConfig, path)
	//}
	//if cfg.Remote.Email == "" {
	//	return Project{}, fmt.Errorf("%w: remote.email is required in %s", ErrInvalidConfig, path)
	//}

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
