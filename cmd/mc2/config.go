package main

import (
	"context"

	"github.com/materials-commons/mccli/pkg/cmds"
	mclogging "github.com/materials-commons/mccli/pkg/logging"
	"github.com/urfave/cli/v3"
)

func configCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Show global or project configuration",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "proj",
				Usage: "Show project configuration instead of global configuration (must be in project)",
				Value: false,
			},
			&cli.BoolFlag{
				Name:  "all",
				Usage: "Show both global and project configuration (must be in project)",
			},
			&cli.BoolFlag{
				Name:  "apikey",
				Usage: "Show API keys, rather than skipping this entry (only applies to global config)",
				Value: false,
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			opts := cmds.ConfigOpts{
				ShowProject: command.Bool("proj"),
				ShowAPIKey:  command.Bool("apikey"),
				ShowAll:     command.Bool("all"),
				ShowGlobal:  !command.Bool("proj"),
			}
			return mclogging.SuppressError(cmds.RunConfigCmd(ctx, opts))
		},
	}
}
