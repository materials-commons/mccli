package main

import (
	"context"
	"fmt"
	"os"

	lscmd "github.com/materials-commons/mccli/pkg/cmd/ls"
	"github.com/urfave/cli/v3"
)

func lsCommand() *cli.Command {
	return &cli.Command{
		Name:      "ls",
		Usage:     "List local and remote directory contents",
		ArgsUsage: "[paths...]",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "action",
				Usage: "Show the action that would be taken for each path and the reason",
			},
		},
		Action: runLSCmd,
	}
}

func runLSCmd(ctx context.Context, cmd *cli.Command) error {
	paths := make([]string, 0, cmd.Args().Len())
	for i := 0; i < cmd.Args().Len(); i++ {
		paths = append(paths, cmd.Args().Get(i))
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	return lscmd.Run(ctx, lscmd.Options{
		WorkingDir: workingDir,
		Paths:      paths,
		Action:     cmd.Bool("action"),
		Out:        os.Stdout,
	})
}
