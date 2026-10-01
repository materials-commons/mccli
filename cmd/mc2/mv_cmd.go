package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

type mvOpts struct {
	remoteOnly bool
	localOnly  bool
	dryRun     bool
	rollback   bool
	view       bool
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
			&cli.BoolFlag{
				Name:  "local-only",
				Usage: "Move files only locally",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Do not actually move files",
			},
			&cli.BoolFlag{
				Name:  "rollback",
				Usage: "Rollback moves that succeeded on the remote, but failed locally",
			},
			&cli.BoolFlag{
				Name:  "view",
				Usage: "Show pending transactions",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			opts := mvOpts{
				remoteOnly: cmd.Bool("remote-only"),
				localOnly:  cmd.Bool("local-only"),
				dryRun:     cmd.Bool("dry-run"),
				rollback:   cmd.Bool("rollback"),
				view:       cmd.Bool("view"),
			}
			return runMvCmd(ctx, opts, cmd.Args().Slice())
		},
	}
}

func runMvCmd(ctx context.Context, opts mvOpts, slice []string) error {
	switch {
	case len(slice) < 2:
		return errors.New("mv requires at least source and dest arguments")
	case opts.dryRun:
		return runDryRun(ctx, opts, slice)
	case opts.remoteOnly:
		return runRemoteOnly(ctx, opts, slice)
	case opts.localOnly:
		return runLocalOnly(ctx, opts, slice)
	case opts.view:
		return runViewOutstandingTransactions(ctx, opts, slice)
	case opts.rollback:
		return runRollbackOutstandingTransactions(ctx, opts, slice)
	default:
		// If we are here then both remoteOnly and localOnly are false. This means
		// that the user didn't specify either. In this case we move both
		// local and remote files/directories.
		return runLocalAndRemote(ctx, opts, slice)
	}
}

type MoveTransaction struct {
	// Unique identifier for the transaction.
	ID string `json:"id"`

	// The working directory that the command associated with this transaction was run in.
	CommandWorkingDir string `json:"command_working_dir"`

	// The arguments that the command associated with this transaction was run with.
	CommandArgs []string `json:"command_args"`

	// Each transaction is a single item. This captures the item in the move args
	// that failed (e.g., the file or dir that was being moved).
	SourceArg string `json:"source_arg"`

	// The destination of the move. This could be a file or a directory. For example, you could move
	// a file to a file-destination to effectively do a rename.
	DestArg string `json:"dest_arg"`

	// This is the full local path of the source. For example, `mv file1 dir1`, since file1 is the source,
	// we'd store the full path to file1 here, e.g., /home/user/proj-name/file1
	SourceLocalPath string `json:"source_local_path"`

	// This is the full local path of the destination. For example, `mv file1 dir1`, since dir1 is the
	// destination, we'd store the full path to dir1 here, e.g., /home/user/proj-name/dir1
	DestLocalPath string `json:"dest_local_path"`

	// Since the move was successful on the remote, we store the remote file ID of the destination.
	RemoteDestFileID *int `json:"remote_dest_file_id"`

	// Date and Time this transaction was created
	CreatedAt time.Time `json:"created_at"`

	// Date and Time this transaction was updated. This would only happen if it failed again.
	UpdatedAt time.Time `json:"updated_at"`

	// A user-friendly error message if the transaction failed. This helps the user understand
	// why the transaction failed and what they can do to fix it.
	LastError string `json:"last_error"`
}

func runDryRun(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("dry run not implemented")
}

func runRemoteOnly(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("remote only not implemented")
}

func runLocalOnly(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("local only not implemented")
}

func runViewOutstandingTransactions(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("view outstanding transactions not implemented")
}

func runRollbackOutstandingTransactions(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("rollback outstanding transactions not implemented")
}

func runLocalAndRemote(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("local and remote not implemented")
}
