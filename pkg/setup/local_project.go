package setup

import (
	"context"
	"os"
	"path/filepath"

	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/filedb"
)

// CreateLocalProject creates the .mc directory and the config.json and mc2.sqlite files.
func CreateLocalProject(ctx context.Context, projectPath string, projectID int) error {
	// Since we also want to create the .mc directory, we add this to the path and use
	// MkdirAll to create both sets of directories.
	if err := os.MkdirAll(filepath.Join(projectPath, ".mc"), 0755); err != nil {
		return err
	}

	// Set up the project by creating the .mc/config.json and the .mc/mc2.sqlite files

	// For the project config the only used field is the project id
	projectConfig := config.Project{ProjectID: projectID}
	if err := config.SaveProject(ctx, projectPath, projectConfig); err != nil {
		return err
	}

	// Create the database. Open will create the database if it doesn't exist.
	s, err := filedb.Open(ctx, projectPath)
	if err != nil {
		return err
	}

	return s.Close(ctx)
}
