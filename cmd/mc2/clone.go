package main

import (
	"context"
	"errors"

	"github.com/materials-commons/mccli/pkg/app"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/mc"
	"github.com/urfave/cli/v3"
)

type cloneRunner struct {
	deps di.Dependencies
}

func cloneCommand() *cli.Command {
	return &cli.Command{
		Name:  "clone",
		Usage: "Clone an existing Materials Commons project",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "id",
				Usage:    "Materials Commons project id to clone",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			projectID := cmd.Int("id")
			return RunCloneCmd(ctx, projectID)
		},
	}
}

// RunCloneCmd runs the clone command with the production dependencies.
func RunCloneCmd(ctx context.Context, projectID int) error {
	return (&cloneRunner{deps: di.Production()}).Run(ctx, projectID)
}

// Run clones a project from the remote server. It creates a directory that
// is the name of the project. The name is cleaned to make sure it is a
// valid file path name. It then sets up the .mc directory, creates the
// config.json and mc2.sqlite files.
func (r *cloneRunner) Run(ctx context.Context, projectID int) error {
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
	projectDirName := mc.CleanProjectDirName(project.Name)

	// CreateLocalProject will create the project directory and initialize it.
	return app.CreateLocalProject(ctx, projectDirName, project.ID)
}

// getRemotelient creates an instance of the client and then casts it to a ProjectGetter.
func (r *cloneRunner) getRemoteClient(cfg config.Global) (mc.ProjectGetter, error) {
	remoteAny, err := r.deps.NewDefaultRemoteClient(cfg)
	if err != nil {
		return nil, err
	}

	remoteClient, ok := remoteAny.(mc.ProjectGetter)
	if !ok {
		return nil, errors.New("remote client is not a ProjectGetter")
	}

	return remoteClient, nil
}
