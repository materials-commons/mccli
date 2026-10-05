package main

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc/file"
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
	deps := di.Production()
	// TODO: Fix NewMover, dest needs to be passed in
	mover, err := file.NewMover(ctx, deps, "")
	if err != nil {
		return err
	}

	mr := moveRunner{
		deps:  di.Production(),
		mover: mover,
	}
	switch {
	case len(slice) < 2:
		return errors.New("mv requires at least source and dest arguments")
	case opts.dryRun:
		return mr.runDryRun(ctx, opts, slice)
	case opts.remoteOnly:
		return mr.runRemoteOnly(ctx, opts, slice)
	case opts.localOnly:
		return mr.runLocalOnly(ctx, opts, slice)
	case opts.view:
		return mr.runViewOutstandingTransactions(ctx, opts, slice)
	case opts.rollback:
		return mr.runRollbackOutstandingTransactions(ctx, opts, slice)
	default:
		// If we are here, then both remoteOnly and localOnly are false. This
		// means that the user didn't specify either. In this case we move both
		// local and remote files/directories.
		return mr.runLocalAndRemote(ctx, opts, slice)
	}
}

type moveRunner struct {
	deps  di.Dependencies
	mover *file.Mover
}

func (m moveRunner) runDryRun(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("dry run not implemented")
}

func (m moveRunner) runRemoteOnly(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("remote only not implemented")
}

func (m moveRunner) runLocalOnly(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("local only not implemented")
}

func (m moveRunner) runViewOutstandingTransactions(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("view outstanding transactions not implemented")
}

func (m moveRunner) runRollbackOutstandingTransactions(ctx context.Context, opts mvOpts, slice []string) error {
	return errors.New("rollback outstanding transactions not implemented")
}

func (m moveRunner) runLocalAndRemote(ctx context.Context, opts mvOpts, files []string) error {
	destExists := true
	destType := file.FileTypeFile

	dest := files[len(files)-1]

	destInfo, err := os.Stat(dest)
	if err != nil {
		destExists = false
	} else {
		if destInfo.IsDir() {
			destType = file.FileTypeDir
		}
	}

	// TODO: This check, and thus the checks above probably need to be lifted up as all function will probably do them
	if destType == file.FileTypeFile && len(files) > 2 {
		// The user has specified a move that sends multiple sources to a
		// destination file. This is a mistake as each of the sources will
		// overwrite the file. We catch this early and return an error as
		// running this is expensive with all the network calls.
		return errors.New("cannot move multiple sources to a single destination file")
	}

	for _, src := range files[:len(files)-1] {
		// We need to figure out if this is a move or a rename.
		// A rename would be file -> file, or dir -> to non-existant.
		// If file -> file, then we want to prevent overwriting the
		// destination file (if it exists), as a safety precaution.
		// We instead force the user to deal with this by having
		// them remove the destination file if that is really their
		// intent.
		srcInfo, err := m.mover.GetFileInfo(src)
		if err != nil {
			return err
		}

		if srcInfo.FileType == file.FileTypeFile {
			if destType == file.FileTypeFile && destExists {
				// Attempt to overwrite an existing file
				return errors.New("cannot move multiple sources to a single destination file")
			}
		}

		if srcInfo.FileType == file.FileTypeDir {
			if destType == file.FileTypeFile {
				return errors.New("cannot move a directory to a file")
			}
		}

		// We've validated locally. There is still validation to do remotely. The
		// remote validation will be done in the methods that handle the move/rename.
		// Here we figure out if this is a move or rename, and what type of move or
		// rename it is.
		switch {
		case srcInfo.FileType == file.FileTypeFile && destType == file.FileTypeFile:
			return m.mover.RenameFile(src, dest)
		case srcInfo.FileType == file.FileTypeDir && destType == file.FileTypeDir:
			return m.mover.MoveDir(src, dest)
		case srcInfo.FileType == file.FileTypeFile && destType == file.FileTypeDir:
			return m.mover.MoveFile(src, dest)
		case srcInfo.FileType == file.FileTypeDir && !destExists:
			return m.mover.RenameDir(src, dest)
		}
	}

	return errors.New("local and remote not implemented")
}
