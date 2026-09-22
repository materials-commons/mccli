package main

import (
	"github.com/urfave/cli/v3"
)

func findCommand() *cli.Command {
	return &cli.Command{
		Name:      "find",
		Usage:     "Find files in your local projects. If in a project finds files in that project, otherwise looks in all projects",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "all",
				Usage: "Look in all projects (overrides only finding files in the current project) ",
			},
		},
		Action: notYetImplemented("find"),
	}
}
