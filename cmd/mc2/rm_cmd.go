package main

import (
	"context"
	"fmt"
	"os"

	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc/file"
	"github.com/urfave/cli/v3"
)

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
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Force removal of files and directories",
			},
			&cli.BoolFlag{
				Name:  "local-only",
				Usage: "Remove files only locally",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Show what would be removed without actually removing anything",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			workingDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}
			opts := file.RemoverOpts{
				WorkingDir: workingDir,
				Recursive:  cmd.Bool("recursive"),
				RemoteOnly: cmd.Bool("remote-only"),
				LocalOnly:  cmd.Bool("local-only"),
				Force:      cmd.Bool("force"),
				DryRun:     cmd.Bool("dry-run"),
				Out:        os.Stdout,
			}
			return runRmCmd(ctx, opts, cmd.Args().Slice())
		},
	}
}

//type rmOpts struct {
//	WorkingDir string
//	Recursive  bool
//	RemoteOnly bool
//	LocalOnly  bool
//	Force      bool
//	DryRun     bool
//	Out        io.Writer
//}

type rmRunner struct {
	deps di.Dependencies
}

func runRmCmd(ctx context.Context, opts file.RemoverOpts, args []string) error {
	return rmRunner{deps: di.Production()}.run(ctx, opts, args)
}

func (r rmRunner) run(ctx context.Context, opts file.RemoverOpts, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(args) == 0 {
		return fmt.Errorf("at least one path argument is required")
	}

	opts = r.normalizeOptions(opts)

	if opts.RemoteOnly && opts.LocalOnly {
		return fmt.Errorf("cannot specify both --remote-only and --local-only")
	}

	remover, err := file.NewRemover(r.deps, ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to create remover: %w", err)
	}

	defer remover.Close()

	// Go through each of the file paths and remove them. Stop on the first error.
	for _, arg := range args {
		remotePath, err := remover.NormalizeRemotePath(arg)
		if err != nil {
			return err
		}

		if err := remover.RemovePath(ctx, remotePath); err != nil {
			return err
		}
	}

	return nil
}

func (r rmRunner) normalizeOptions(opts file.RemoverOpts) file.RemoverOpts {
	if opts.WorkingDir == "" {
		opts.WorkingDir = "."
	}

	if opts.Out == nil {
		opts.Out = os.Stdout
	}

	return opts
}
