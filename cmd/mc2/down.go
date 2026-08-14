package main

import (
	"context"
	"fmt"
	"os"

	downcmd "github.com/materials-commons/mccli/pkg/cmd/down"
	"github.com/urfave/cli/v3"
)

func downCommand() *cli.Command {
	return &cli.Command{
		Name:      "down",
		Usage:     "Download files or directories from Materials Commons",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "recursive",
				Aliases: []string{"r"},
				Usage:   "Download directory contents recursively",
			},
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "Overwrite local files even if they were not previously uploaded",
			},
		},
		Action: runDownCmd,
	}
}

func runDownCmd(ctx context.Context, cmd *cli.Command) error {
	paths := make([]string, 0, cmd.Args().Len())
	for i := 0; i < cmd.Args().Len(); i++ {
		paths = append(paths, cmd.Args().Get(i))
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	return downcmd.Run(ctx, downcmd.Options{
		WorkingDir: workingDir,
		Paths:      paths,
		Recursive:  cmd.Bool("recursive"),
		Force:      cmd.Bool("force"),
		Out:        os.Stdout,
	})
}
