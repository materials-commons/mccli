package clone

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"github.com/materials-commons/mccli/pkg/remote"
)

type runner struct {
	deps di.Dependencies
}

// Run runs the clone command with the production dependencies.
func Run(ctx context.Context, projectID int) error {
	return (&runner{deps: di.Production()}).Run(ctx, projectID)
}

// Run clones a project from the remote server. It creates a directory that
// is the name of the project. The name is cleaned to make sure it is a
// valid file path name. It then sets up the .mc directory, creates the
// config.json and mc2.sqlite files.
func (r *runner) Run(ctx context.Context, projectID int) error {
	// Load Global Config and create the remote client
	globalConfig, err := r.deps.LoadGlobal(ctx, "")
	if err != nil {
		return err
	}

	remoteClient, err := r.getRemoteClient(globalConfig)
	if err != nil {
		return err
	}

	// Get the project - We will use the project name as the directory
	// name to use. However, we first need to normalize the name by
	// removing any special characters from it.
	project, err := remoteClient.GetProject(projectID)
	if err != nil {
		return err
	}

	// Clean the project name of any special characters and names to make sure
	// it is a valid directory name and that it is easy to type by removing
	// special characters.
	projectDirName := projectpath.CleanProjectDirName(project.Name)

	// Since we also want to create the .mc directory, we add this to the path and use
	// MkdirAll to create both sets of directories.
	if err := os.MkdirAll(filepath.Join(projectDirName, ".mc"), 0755); err != nil {
		return err
	}

	// Set up the project by creating the .mc/config.json and the .mc/mc2.sqlite files

	// For the project config the only used field is the project id
	projectConfig := config.Project{ProjectID: project.ID}
	if err := config.SaveProject(ctx, projectDirName, projectConfig); err != nil {
		return err
	}

	// Create the database. Open will create the database if it doesn't exist.
	s, err := filedb.Open(ctx, projectDirName)
	if err != nil {
		return err
	}

	return s.Close(ctx)
}

// getRemotelient creates an instance of the client and then casts it to a ProjectGetter.
func (r *runner) getRemoteClient(cfg config.Global) (remote.ProjectGetter, error) {
	remoteAny, err := r.deps.NewDefaultRemoteClient(cfg)
	if err != nil {
		return nil, err
	}

	remoteClient, ok := remoteAny.(remote.ProjectGetter)
	if !ok {
		return nil, errors.New("remote client is not a ProjectGetter")
	}

	return remoteClient, nil
}
