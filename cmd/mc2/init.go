package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/materials-commons/mccli/pkg/cmds"
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
			var opts cmds.InitOpts
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

			return cmds.RunInitCmd(ctx, opts)
		},
	}
}
