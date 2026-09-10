package cmds

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
)

type initRunner struct {
	deps di.Dependencies
}

type InitOpts struct {
	Description string
	ProjectName string
}

func RunInitCmd(ctx context.Context, opts InitOpts) error {
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
func (r *initRunner) Run(ctx context.Context, opts InitOpts) error {
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
func (r *initRunner) projectNameFromOptsOrCurrentDir(opts InitOpts) (string, error) {
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
