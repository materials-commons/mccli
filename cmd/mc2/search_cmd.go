package main

import (
	"github.com/urfave/cli/v3"
)

func searchCommand() *cli.Command {
	return &cli.Command{
		Name:      "search",
		Usage:     "Search files in your local projects. If in a project searches files in that project, otherwise searches in all projects",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "all",
				Usage: "Search in all projects (overrides only searching the current project) ",
			},
		},
		Action: notYetImplemented("search"),
	}
}
