package main

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/urfave/cli/v3"
)

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Show mc2 version and build information",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Println("mc2", formatVersion())
			return nil
		},
	}
}

// formatVersion returns a human-readable version and build metadata.
func formatVersion() string {
	info := versionInfo{
		Version:   version,
		GitTag:    gitTag,
		GitBranch: gitBranch,
		GitCommit: gitCommit,
		GitDate:   gitDate,
		GitDirty:  gitDirty,
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = bi.GoVersion

		for _, setting := range bi.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.GitCommit == "" {
					info.GitCommit = setting.Value
				}
			case "vcs.time":
				if info.GitDate == "" {
					info.GitDate = setting.Value
				}
			case "vcs.modified":
				if info.GitDirty == "" {
					info.GitDirty = setting.Value
				}
			}
		}
	}

	return info.String()
}

type versionInfo struct {
	Version   string
	GitTag    string
	GitBranch string
	GitCommit string
	GitDate   string
	GitDirty  string
	GoVersion string
}

func (v versionInfo) String() string {
	tag := v.GitTag
	if tag == "" {
		tag = "untagged release"
	}

	return fmt.Sprintf("%s (%s) for branch %s, on %s", v.Version, tag, v.GitBranch, v.GitDate)
}
