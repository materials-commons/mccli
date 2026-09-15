package main

import (
	"context"

	"github.com/materials-commons/mccli/pkg/cmds"
	"github.com/urfave/cli/v3"
)

func rmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Usage:     "Remove files and directories locally and remotely",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "recursive",
				Aliases: []string{"r"},
				Usage:   "Remove remote directories recursively",
			},
			&cli.BoolFlag{
				Name:  "remote-only",
				Usage: "Remove files only on the Materials Commons server",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			opts := cmds.RmOpts{
				Recursive:  cmd.Bool("recursive"),
				RemoteOnly: cmd.Bool("remote-only"),
			}
			return cmds.RunRmCmd(ctx, opts, cmd.Args().Slice())
		},
	}
}
