package main

import (
	"context"

	"github.com/materials-commons/mccli/pkg/cmds"
	"github.com/urfave/cli/v3"
)

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
			return cmds.RunCloneCmd(ctx, projectID)
		},
	}
}
