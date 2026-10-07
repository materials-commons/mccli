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
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			remoteOnly := cmd.Bool("remote-only")
			localOnly := cmd.Bool("local-only")
			dryRun := cmd.Bool("dry-run")
			moveBoth := !remoteOnly && !localOnly
			opts := file.MoverOpts{
				MoveBoth:       moveBoth,
				DryRun:         dryRun,
				MoveLocalOnly:  localOnly,
				MoveRemoteOnly: remoteOnly,
			}
			return runMvCmd(ctx, opts, cmd.Args().Slice())
		},
	}
}

func runMvCmd(ctx context.Context, opts file.MoverOpts, slice []string) error {
	deps := di.Production()
	if len(slice) < 2 {
		return errors.New("mv requires at least source and dest arguments")
	}

	dest := slice[len(slice)-1]
	mover, err := file.NewMover(ctx, deps, dest, opts)
	if err != nil {
		return err
	}

	mr := moveRunner{
		mover: mover,
	}

	return mr.run(ctx, opts, slice)
}

type moveRunner struct {
	mover *file.Mover
}

type fileTyper interface {
	IsDir() bool
}

func isDir(finfo fileTyper) bool {
	if finfo == nil {
		return false
	}
	return finfo.IsDir()
}

func isFile(finfo fileTyper) bool {
	if finfo == nil {
		return false
	}
	return !finfo.IsDir()
}

type getFileTypeFunc func(path string) (fileTyper, error)

// run performs the move and rename operations.
func (m moveRunner) run(ctx context.Context, opts file.MoverOpts, files []string) error {
	if opts.MoveRemoteOnly {
		// Remote only move. Send a file lookup function that gets the type information from
		// the remote server.
		return m.doMoves(ctx, files, func(path string) (fileTyper, error) {
			return m.mover.GetRemoteFile(path)
		})
	}

	// LocalOnly, or Local/Remote. In all these cases we need to get the
	// type information for the files locally.
	return m.doMoves(ctx, files, func(path string) (fileTyper, error) {
		return os.Stat(path)
	})
}

// doMoves handles the file movement. The getFileType passed in function allows this method to work
// for remote only, and local files.
func (m moveRunner) doMoves(ctx context.Context, files []string, getFileType getFileTypeFunc) error {
	destExists := true
	dest := files[len(files)-1]

	// Figure out the state of the destination. A destination that doesn't exist would mean that we are doing a rename.
	destType, err := getFileType(dest)
	if err != nil {
		destExists = false
	}

	// Make sure the user isn't trying to move multiple files to a file destination. We don't allow overwrites.
	if isFile(destType) && len(files) > 2 {
		// The user has specified a move that sends multiple sources to a
		// destination file. This is a mistake as each of the sources will
		// overwrite the file. We catch this early and return an error as
		// running this is expensive with all the network calls.
		return errors.New("destination file already exists")
	}

	// Loop through and perform the moves/renames. We stop at the first error.
	for _, src := range files[:len(files)-1] {
		// Make sure that the source of the move exists.
		srcType, err := getFileType(src)
		if err != nil {
			// The user specified a source that doesn't exist.
			return err
		}

		// Check if the move is valid. A source file cannot be moved over an existing file. And a source directory
		// cannot be moved over an existing file.
		switch {
		case isFile(srcType):
			if destExists && isFile(destType) {
				// User has specified a move of a file to an existing file. This would overwrite the existing file,
				// which is not allowed.
				return errors.New("cannot move over an existing file")
			}
		case isDir(srcType):
			if destExists && isFile(destType) {
				// The user has specified a move of a directory to an existing file. A directory cannot be moved "over"
				// a file.
				return errors.New("cannot move a directory to a file")
			}
		}

		// We've done local validation. Now we need to check if this is a move or a rename.
		switch {
		case isFile(srcType) && !destExists:
			// Moving a file to a non-existing destination will result in a rename.
			if err := m.mover.RenameFile(src, dest); err != nil {
				return err
			}
		case isDir(srcType) && !destExists:
			// Moving a directory to a non-existing destination will result in a rename.
			if err := m.mover.RenameDir(src, dest); err != nil {
				return err
			}
		case isFile(srcType) && isDir(destType):
			// Moving a file to a directory will result in a move.
			if err := m.mover.MoveFile(src, dest); err != nil {
				return err
			}
		case isDir(srcType) && isDir(destType):
			// Moving a directory to a directory will result in a move.
			if err := m.mover.MoveDir(src, dest); err != nil {
				return err
			}
		}

		// Each time through the loop we are going to check if dest exists. This might seem weird since
		// we checked at the start of the method. However, each individual move may change the state of
		// dest. For example:
		//     `mc2 mv file dir non-existing-file`
		// Here the first move is valid, we are renaming "file" to "non-existing-file". Now the second time
		// through the loop "non-existing-file" exists and is a file. So we need to check if it exists again.
		// We only have to do these checks so long a destExists == false. Once destExists is true, the state
		// of dest won't change.
		if !destExists {
			// Last time through the loop dest didn't exist, lets re-establish its state.
			destType, err = getFileType(dest)
			if err == nil {
				destExists = true
			}
		}
	}

	return nil
}
