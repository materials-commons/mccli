package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"github.com/materials-commons/mccli/pkg/remote"
	"github.com/materials-commons/mccli/pkg/setup"
	"github.com/urfave/cli/v3"
)

func initCommand() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Initialize the current directory as a new Materials Commons project",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "name",
				Usage: "Name of the project (defaults to current directory name)",
			},
			&cli.StringFlag{
				Name:  "description",
				Usage: "Description of the project",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var opts initOpts
			// Init will initialize the current directory as a new Materials Commons project. The project
			// doesn't have to match the directory name (though that is the recommendation). If the user
			// doesn't provide a project name, then the project name will be the base name of the current
			// directory.
			opts.ProjectName = cmd.String("name")
			if opts.ProjectName == "" {
				currentDir, err := os.Getwd()
				if err != nil {
				}

				opts.ProjectName = filepath.Base(currentDir)
			}

			opts.Description = cmd.String("description")

			return runInitCmd(ctx, opts)
		},
	}
}

type initRunner struct {
	deps di.Dependencies
}

type initOpts struct {
	Description string
	ProjectName string
}

func runInitCmd(ctx context.Context, opts initOpts) error {
	return (&initRunner{deps: di.Production()}).Run(ctx, opts)
}

// Run will create a new project in the current directory and initialize it. It does
// some sanity checks to make sure that the project isn't either:
//
//	a. Being created in an existing project, or
//	b. That one of the subdirectories is already a project, or
//	c. That the current directory is already a project.
//
// If any of these are true, it returns an error.
func (r *initRunner) Run(ctx context.Context, opts initOpts) error {
	// Check for .mc in the current directory and its subdirectories. If there is one, then
	// the user is attempting to create a project in an existing project or one of its
	// subdirectories is already a project. In either case this is not allowed.
	if err := r.containsMCDir(); err != nil {
		// If the error is not nil, then that means a .mc dir was found.
		fmt.Println("You are attempting to create a project in an existing project or one of the subdirectories is already a project. In either case this is not allowed.")
		return err
	}

	// Check if we are attempting to create a project in an existing project.
	if _, err := projectpath.FindRoot(ctx, "."); err == nil {
		// If error is nil, then we found an existing project root.
		fmt.Println("You are attempting to create a project in an existing project or one of the subdirectories is already a project. In either case this is not allowed.")
		return errors.New("project already exists")
	}

	projectName, err := r.projectNameFromOptsOrCurrentDir(opts)
	if err != nil {
		return err
	}

	// Clean the project name for the directory path
	projectDir := projectpath.CleanProjectDirName(projectName)

	// Load Global config and create the remote client
	globalConfig, err := r.deps.LoadGlobal(ctx, "")
	if err != nil {
		return err
	}

	remoteClient, err := r.getRemoteClient(globalConfig)
	if err != nil {
		return err
	}

	// Create the project on the server. We will use the project name passed in.
	projReq := mcapi.CreateProjectRequest{
		Name:        projectName,
		Description: opts.Description,
	}

	proj, err := remoteClient.CreateProject(projReq)
	if err != nil {
		return err
	}

	// Now set up the project by creating the directory, its config and metadata.
	return setup.CreateLocalProject(ctx, projectDir, proj.ID)
}

func (r *initRunner) getRemoteClient(cfg config.Global) (remote.ProjectCreater, error) {
	remoteAny, err := r.deps.NewDefaultRemoteClient(cfg)
	if err != nil {
		return nil, err
	}

	remoteClient, ok := remoteAny.(remote.ProjectCreater)
	if !ok {
		return nil, errors.New("remote client is not a ProjectGetter")
	}

	return remoteClient, nil
}

// projectNameFromOptsOrCurrentDir ensures that we have a project name. It first
// checks if the user passed in a project name. If not, it uses the current directory name.
func (r *initRunner) projectNameFromOptsOrCurrentDir(opts initOpts) (string, error) {
	if opts.ProjectName != "" {
		// user passed in a project name
		return opts.ProjectName, nil
	}

	// No project name, so use the current directory as the project name
	dirPath, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Base(dirPath), nil
}

// containsMCDir walks the current dir and all its subdirectories and looks for a .mc directory
func (r *initRunner) containsMCDir() error {
	return filepath.WalkDir(".", func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == ".mc" {
			return errors.New("project already exists")
		}
		return nil
	})
}
