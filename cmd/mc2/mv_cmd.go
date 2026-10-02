package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/services"
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
	mover, err := newMover(ctx, deps)
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
	mover *Mover
}

type fileType int

const (
	FileTypeFile fileType = iota
	FileTypeDir
)

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

type FileInfo struct {
	FileInfo os.FileInfo
	FileType fileType
}

func (m moveRunner) runLocalAndRemote(ctx context.Context, opts mvOpts, files []string) error {
	destExists := true
	destType := FileTypeFile

	dest := files[len(files)-1]

	destInfo, err := os.Stat(dest)
	if err != nil {
		destExists = false
	} else {
		if destInfo.IsDir() {
			destType = FileTypeDir
		}
	}

	// TODO: This check, and thus the checks above probably need to be lifted up as all function will probably do them
	if destType == FileTypeFile && len(files) > 2 {
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
		srcInfo, err := m.mover.getFileInfo(src)
		if err != nil {
			return err
		}

		if srcInfo.FileType == FileTypeFile {
			if destType == FileTypeFile && destExists {
				// Attempt to overwrite an existing file
				return errors.New("cannot move multiple sources to a single destination file")
			}
		}

		if srcInfo.FileType == FileTypeDir {
			if destType == FileTypeFile {
				return errors.New("cannot move a directory to a file")
			}
		}

		// We've validated locally. There is still validation to do remotely. The
		// remote validation will be done in the methods that handle the move/rename.
		// Here we figure out if this is a move or rename, and what type of move or
		// rename it is.
		switch {
		case srcInfo.FileType == FileTypeFile && destType == FileTypeFile:
			return m.mover.renameFile(src, dest)
		case srcInfo.FileType == FileTypeDir && destType == FileTypeDir:
			return m.mover.moveDir(src, dest)
		case srcInfo.FileType == FileTypeFile && destType == FileTypeDir:
			return m.mover.moveFile(src, dest)
		case srcInfo.FileType == FileTypeDir && !destExists:
			return m.mover.renameDir(src, dest)
		}
	}

	return errors.New("local and remote not implemented")
}

func (m *Mover) renameFile(src, dest string) error {
	return errors.New("rename file not implemented")
}

func (m *Mover) renameDir(src, dest string) error {
	return errors.New("rename file not implemented")
}

func (m *Mover) moveFile(src, dest string) error {
	return errors.New("move file not implemented")
}

func (m *Mover) moveDir(src, dest string) error {
	return errors.New("move dir not implemented")
}

func (m *Mover) getFileInfo(path string) (*FileInfo, error) {
	var (
		finfo FileInfo
		err   error
	)
	finfo.FileInfo, err = os.Stat(path)
	if err != nil {
		return nil, err
	}

	if finfo.FileInfo.IsDir() {
		finfo.FileType = FileTypeDir
	} else {
		finfo.FileType = FileTypeFile
	}

	return &finfo, nil
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

type Mover struct {
	projectConfig         config.Project
	store                 di.Store
	projectPathTranslator mc.ProjectPathTranslator
	remoteGetter          mc.FileDirectoryGetter
	remoteMover           mc.FileMover
	remoteRenamer         mc.FileRenamer
	workingDir            string
}

func newMover(ctx context.Context, deps di.Dependencies) (*Mover, error) {
	var (
		m   Mover
		err error
	)

	m.workingDir, err = os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	container := services.NewContainer(deps)

	cmdCtx, err := container.LoadCommandContext(ctx, m.workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load command context: %w", err)
	}

	m.projectConfig = cmdCtx.Project

	if m.store, err = container.Store(ctx); err != nil {
		return nil, fmt.Errorf("failed to load store: %w", err)
	}

	if m.remoteGetter, m.remoteMover, m.remoteRenamer, err = getMoverRemotes(container); err != nil {
		return nil, fmt.Errorf("failed to load remote mover remotes: %w", err)
	}
	return &m, nil
}

func getMoverRemotes(container *services.Container) (mc.FileDirectoryGetter, mc.FileMover, mc.FileRenamer, error) {
	remoteAny, err := container.Remote()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load remote: %w", err)
	}

	remoteGetter, ok := remoteAny.(mc.FileDirectoryGetter)
	if !ok {
		return nil, nil, nil, fmt.Errorf("remote does not implement FileDirectoryGetter")
	}

	remoteMover, ok := remoteAny.(mc.FileMover)
	if !ok {
		return nil, nil, nil, fmt.Errorf("remote does not implement FileMover")
	}

	remoteRenamer, ok := remoteAny.(mc.FileRenamer)
	if !ok {
		return nil, nil, nil, fmt.Errorf("remote does not implement FileRename")
	}

	return remoteGetter, remoteMover, remoteRenamer, nil
}
