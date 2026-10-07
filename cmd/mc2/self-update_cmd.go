package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/urfave/cli/v3"
)

func selfUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:      "self-update",
		Usage:     "Update mc2 to the latest available version",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "check",
				Usage: "Check if there is a new version available",
			},
		},
		Action: runSelfUpdateCmd,
	}
}

func runSelfUpdateCmd(ctx context.Context, cmd *cli.Command) error {
	latest, found, err := selfupdate.DetectLatest(ctx, selfupdate.ParseSlug("materials-commons/mccli"))
	if err != nil {
		return fmt.Errorf("error occurred while detecting version: %w", err)
	}
	if !found {
		return fmt.Errorf("latest version for %s/%s could not be found from github repository", runtime.GOOS, runtime.GOARCH)
	}

	checkVersion := cmd.Bool("check")
	if checkVersion {
		showVersionCheckResults(latest)
		return nil
	}

	return updateToLatest(ctx, latest)
}

func showVersionCheckResults(latest *selfupdate.Release) {
	if latest.LessOrEqual(version) {
		log.Printf("Current version (%s) is the latest version", version)
	} else {
		log.Printf("New version (%s) available, you are running version (%s)", latest.Version(), version)
	}
}

func updateToLatest(ctx context.Context, latest *selfupdate.Release) error {
	if latest.LessOrEqual(version) {
		log.Printf("Current version (%s) is the latest", version)
		return nil
	}

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return errors.New("could not locate executable path")
	}
	if err := selfupdate.UpdateTo(ctx, latest.AssetURL, latest.AssetName, exe); err != nil {
		return fmt.Errorf("error occurred while updating binary: %w", err)
	}
	log.Printf("Successfully updated to version %s", latest.Version())
	return nil
}
