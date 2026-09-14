package main

import (
	"context"

	"github.com/materials-commons/mccli/pkg/cmds"
	"github.com/urfave/cli/v3"
)

func loginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: `Login sets up the cli by authenticating with Materials Commons, and storing the credentials locally.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "url",
				Aliases: []string{"u"},
				Value:   "https://materialscommons.org/api",
				Usage:   "MCAPI Url",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var opts cmds.LoginOpts
			opts.MCAPIUrl = cmd.String("url")
			return cmds.RunLoginCmd(ctx, opts)
		},
	}
}
