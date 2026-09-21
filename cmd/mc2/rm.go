package main

import (
	"context"

	"github.com/materials-commons/mccli/internal/di"
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
			opts := rmOpts{
				Recursive:  cmd.Bool("recursive"),
				RemoteOnly: cmd.Bool("remote-only"),
			}
			return runRmCmd(ctx, opts, cmd.Args().Slice())
		},
	}
}

type rmOpts struct {
	Recursive  bool
	RemoteOnly bool
}

type rmRunner struct {
	deps di.Dependencies
}

func runRmCmd(ctx context.Context, opts rmOpts, args []string) error {
	return rmRunner{deps: di.Production()}.run(ctx, opts, args)
}

func (r rmRunner) run(ctx context.Context, opts rmOpts, args []string) error {
	return nil
}
