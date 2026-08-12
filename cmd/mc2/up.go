package main

import (
	"context"
	"fmt"
	"os"

	upcmd "github.com/materials-commons/mccli/pkg/cmd/up"
	"github.com/urfave/cli/v3"
)

func upCommand() *cli.Command {
	return &cli.Command{
		Name:      "up",
		Usage:     "Upload files to Materials Commons",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "recursive",
				Aliases: []string{"r"},
				Usage:   "Upload directory contents recursively",
			},
			&cli.StringFlag{
				Name:  "ws-url",
				Usage: "WebSocket URL for upload commands",
				Value: defaultWebSocketURL,
			},
		},
		Action: runUpCmd,
	}
}

func runUpCmd(ctx context.Context, cmd *cli.Command) error {
	paths := make([]string, 0, cmd.Args().Len())
	for i := 0; i < cmd.Args().Len(); i++ {
		paths = append(paths, cmd.Args().Get(i))
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	return upcmd.Run(ctx, upcmd.Options{
		WorkingDir:   workingDir,
		Paths:        paths,
		Recursive:    cmd.Bool("recursive"),
		WebSocketURL: cmd.String("ws-url"),
		Out:          os.Stdout,
	})
}
