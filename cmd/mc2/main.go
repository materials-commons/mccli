package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"

	mcapi "github.com/materials-commons/gomcapi"
	lscmd "github.com/materials-commons/mccli/pkg/cmd/ls"
	mclogging "github.com/materials-commons/mccli/pkg/logging"
	"github.com/urfave/cli/v3"
)

const defaultWebSocketURL = "wss://materialscommons.org/ws"

// These variables are intended to be populated by release builds using -ldflags.
//
// Example:
//
//	go build -ldflags "\
//	  -X main.version=2.0.0 \
//	  -X main.gitTag=v2.0.0 \
//	  -X main.gitBranch=main \
//	  -X main.gitCommit=abc1234 \
//	  -X main.gitDate=2026-08-08T12:00:00Z \
//	  -X main.gitDirty=false" ./cmd/mc2
var (
	version   = "dev"
	gitTag    = ""
	gitBranch = ""
	gitCommit = ""
	gitDate   = ""
	gitDirty  = ""
)

// main runs the mc2 command.
func main() {
	var c mcapi.Client
	_ = c
	cmd := newCommand()

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		slog.Error("mc2 failed", "error", err)
		os.Exit(1)
	}
}

// newCommand constructs the mc2 command tree.
//
// Keeping command construction separate from main makes the CLI layout testable
// without executing the process.
func newCommand() *cli.Command {
	return &cli.Command{
		Name:                   "mc2",
		Usage:                  "Materials Commons command-line client",
		UseShortOptionHandling: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "log-level",
				Usage: "Set logging level: debug, info, warn, or error",
				Value: mclogging.DefaultLevelName,
			},
			&cli.StringFlag{
				Name:  "log-file",
				Usage: "Write logs to `FILE` instead of stderr",
			},
		},
		Before: configureLogging,
		Commands: []*cli.Command{
			versionCommand(),
			cloneCommand(),
			configCommand(),
			downCommand(),
			initCommand(),
			lsCommand(),
			mkdirCommand(),
			mvCommand(),
			projCommand(),
			rmCommand(),
			remotesCommand(),
			upCommand(),
		},
	}
}

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Show mc2 version and build information",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Println(formatVersion())
			return nil
		},
	}
}

func cloneCommand() *cli.Command {
	return &cli.Command{
		Name:  "clone",
		Usage: "Clone an existing Materials Commons project",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "id",
				Usage:    "Materials Commons project id to clone",
				Required: true,
			},
		},
		Action: notYetImplemented("clone"),
	}
}

func configCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Show global or project configuration",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "proj",
				Usage: "Show project configuration instead of global configuration",
			},
		},
		Action: notYetImplemented("config"),
	}
}

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
		Action: notYetImplemented("down"),
	}
}

func initCommand() *cli.Command {
	return &cli.Command{
		Name:   "init",
		Usage:  "Initialize the current directory as a new Materials Commons project",
		Action: notYetImplemented("init"),
	}
}

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
		Action: func(ctx context.Context, cmd *cli.Command) error {
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
		},
	}
}

func mkdirCommand() *cli.Command {
	return &cli.Command{
		Name:      "mkdir",
		Usage:     "Create directories locally and remotely",
		ArgsUsage: "paths...",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "remote-only",
				Usage: "Create directories only on the Materials Commons server",
			},
		},
		Action: notYetImplemented("mkdir"),
	}
}

func mvCommand() *cli.Command {
	return &cli.Command{
		Name:      "mv",
		Usage:     "Move or rename files and directories locally and remotely",
		ArgsUsage: "src target",
		Description: strings.TrimSpace(`
Use "mc2 mv <src> <target>" to move and/or rename a file or directory.
Use "mc2 mv <src> ... <directory>" to move a list of files or directories
into an existing directory.
`),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "remote-only",
				Usage: "Move files only on the Materials Commons server",
			},
		},
		Action: notYetImplemented("mv"),
	}
}

func projCommand() *cli.Command {
	return &cli.Command{
		Name:   "proj",
		Usage:  "List remote projects the current user can access",
		Action: notYetImplemented("proj"),
	}
}

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
		Action: notYetImplemented("rm"),
	}
}

func remotesCommand() *cli.Command {
	return &cli.Command{
		Name:  "remotes",
		Usage: "Show or update remote server settings",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "list",
				Aliases: []string{"l"},
				Usage:   "List known remote server URLs",
			},
			&cli.BoolFlag{
				Name:  "show-apikey",
				Usage: "Show API keys when printing configured remotes",
			},
			&cli.StringFlag{
				Name:  "add",
				Usage: "Add a new remote",
			},
			&cli.StringFlag{
				Name:  "remove",
				Usage: "Remove an existing remote",
			},
			&cli.StringFlag{
				Name:  "set-default",
				Usage: "Set the default remote URL",
			},
		},
		Action: notYetImplemented("remotes"),
	}
}

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
		Action: notYetImplemented("up"),
	}
}

func configureLogging(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	logCtx, cleanup, err := mclogging.Configure(ctx, mclogging.Config{
		Level: cmd.String("log-level"),
		File:  cmd.String("log-file"),
	})
	if err != nil {
		return ctx, err
	}

	if cleanup != nil {
		deferCleanup := func() {
			if err := cleanup(); err != nil {
				mclogging.Logger(logCtx).Warn("failed to close log output", "error", err)
			}
		}
		cmd.After = appendAfter(cmd.After, deferCleanup)
	}

	mclogging.Logger(logCtx).Debug("logging configured",
		"level", cmd.String("log-level"),
		"log_file", cmd.String("log-file"),
	)

	return logCtx, nil
}

func appendAfter(after cli.AfterFunc, cleanup func()) cli.AfterFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		var afterErr error
		if after != nil {
			afterErr = after(ctx, cmd)
		}

		cleanup()

		return afterErr
	}
}

func notYetImplemented(name string) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		mclogging.Logger(ctx).Debug("command invoked", "command", name)
		return fmt.Errorf("%s command is not implemented yet", name)
	}
}

// formatVersion returns human-readable version and build metadata.
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
	var b strings.Builder

	writeLine := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}

	writeLine("mc2", v.Version)
	writeLine("git tag", v.GitTag)
	writeLine("git branch", v.GitBranch)
	writeLine("git commit", v.GitCommit)
	writeLine("git date", v.GitDate)
	writeLine("git dirty", v.GitDirty)
	writeLine("go", v.GoVersion)

	return strings.TrimRight(b.String(), "\n")
}
