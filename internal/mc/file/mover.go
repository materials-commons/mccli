package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/services"
)

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

type FileType int

const (
	FileTypeFile FileType = iota
	FileTypeDir
)

type FileInfo struct {
	FileInfo os.FileInfo
	FileType FileType
}

func NewMover(ctx context.Context, deps di.Dependencies) (*Mover, error) {
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

func (m *Mover) RenameFile(src, dest string) error {
	return errors.New("rename file not implemented")
}

func (m *Mover) RenameDir(src, dest string) error {
	return errors.New("rename file not implemented")
}

func (m *Mover) MoveFile(src, dest string) error {
	return errors.New("move file not implemented")
}

func (m *Mover) MoveDir(src, dest string) error {
	return errors.New("move dir not implemented")
}

func (m *Mover) GetFileInfo(path string) (*FileInfo, error) {
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
