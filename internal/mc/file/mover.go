package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/services"
)

type FileType int

const (
	FileTypeFile FileType = iota
	FileTypeDir
)

type FileInfo struct {
	FileInfo os.FileInfo
	FileType FileType
}

type Mover struct {
	projectConfig         config.Project
	store                 di.Store
	projectPathTranslator mc.ProjectPathTranslator
	remoteGetter          mc.FileDirectoryGetter
	remoteMover           mc.FileMover
	remoteRenamer         mc.FileRenamer
	workingDir            string
	remoteDest            *mcmodel.File
}

func NewMover(ctx context.Context, deps di.Dependencies, dest string) (*Mover, error) {
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

	err = m.loadRemoteDest(m.normalizePath(dest))
	if err != nil {
		return nil, fmt.Errorf("failed to load remote destination: %w", err)
	}

	return &m, nil
}

// RenameFile renames a file on the remote and locally. The src and dest paths are the paths passed in by the user.
// These paths will be normalized to the remote's working directory.
func (m *Mover) RenameFile(src, dest string) error {
	if m.remoteDest != nil {
		// We are renaming a file, the destination can't exist
		return fmt.Errorf("destination already exists")
	}

	return m.do(src, dest, func(sourceProjectPath, destProjectPath string, sourceRemoteFile *mcmodel.File) error {
		if sourceRemoteFile == nil {
			return fmt.Errorf("source remote file not found")
		}

		if sourceRemoteFile.IsDir() == true {
			return fmt.Errorf("source is a directory")
		}

		// Rename the remote file
		err := m.remoteRenamer.RenameFile(m.projectConfig.ProjectID, sourceRemoteFile.ID, destProjectPath)
		if err != nil {
			return fmt.Errorf("failed to rename remote file: %w", err)
		}

		// Rename the local file
		if err := os.Rename(m.normalizePath(src), m.normalizePath(dest)); err != nil {
			return fmt.Errorf("failed to rename local file: %w", err)
		}

		// Update the local project database entry for this file
		f, err := m.store.GetByPath(context.Background(), sourceProjectPath)
		if err != nil {
			return fmt.Errorf("failed to get local file: %w", err)
		}

		f.Path = destProjectPath
		if err := m.store.Upsert(context.Background(), f); err != nil {
			return fmt.Errorf("failed to update local file: %w", err)
		}

		return nil
	})
}

func (m *Mover) RenameDir(src, dest string) error {
	if m.remoteDest != nil {
		return fmt.Errorf("destination already exists")
	}

	return m.do(src, dest, func(sourceProjectPath, destProjectPath string, sourceRemoteDir *mcmodel.File) error {
		if sourceRemoteDir == nil {
			return fmt.Errorf("source remote dir not found")
		}

		if sourceRemoteDir.IsFile() == true {
			return fmt.Errorf("source is a file")
		}

		// Rename the remote dir
		if err := m.remoteRenamer.RenameDirectory(m.projectConfig.ProjectID, sourceRemoteDir.ID, destProjectPath); err != nil {
			return fmt.Errorf("failed to rename remote dir: %w", err)
		}

		// Rename the local dir
		if err := os.Rename(m.normalizePath(src), m.normalizePath(dest)); err != nil {
			return fmt.Errorf("failed to rename local dir: %w", err)
		}

		// TODO: Rename database paths from old to new path for all files in that path

		return nil
	})
}

func (m *Mover) MoveFile(src, dest string) error {
	if m.remoteDest == nil {
		return errors.New("no remote destination")
	}

	if m.remoteDest.IsFile() == true {
		return fmt.Errorf("destination is a file")
	}

	return m.do(src, dest, func(sourceProjectPath, destProjectPath string, sourceRemoteFile *mcmodel.File) error {
		if sourceRemoteFile == nil {
			return fmt.Errorf("source remote file not found")
		}

		if sourceRemoteFile.IsDir() == true {
			return fmt.Errorf("source is a directory")
		}

		// Move the remote file
		if err := m.remoteMover.MoveFile(m.projectConfig.ProjectID, sourceRemoteFile.ID, m.remoteDest.ID); err != nil {
			return fmt.Errorf("failed to move remote file: %w", err)
		}

		// Move the local file
		if err := os.Rename(sourceProjectPath, destProjectPath); err != nil {
			return fmt.Errorf("failed to move local file: %w", err)
		}

		// Update the local project database entry for this file
		f, err := m.store.GetByPath(context.Background(), sourceProjectPath)
		if err != nil {
			return fmt.Errorf("failed to get local file: %w", err)
		}

		f.Path = filepath.Join(destProjectPath, filepath.Base(sourceProjectPath))
		if err := m.store.Upsert(context.Background(), f); err != nil {
			return fmt.Errorf("failed to update local file: %w", err)
		}

		return nil
	})
}

func (m *Mover) MoveDir(src, dest string) error {
	if m.remoteDest == nil {
		return errors.New("no remote destination")
	}

	if m.remoteDest.IsFile() == true {
		return fmt.Errorf("destination is a file")
	}

	return m.do(src, dest, func(sourceProjectPath, destProjectPath string, sourceRemoteDir *mcmodel.File) error {
		if sourceRemoteDir == nil {
			return fmt.Errorf("source remote file not found")
		}

		if sourceRemoteDir.IsFile() == true {
			return fmt.Errorf("source is a file")
		}

		// Move the remote directory
		if err := m.remoteMover.MoveDirectory(m.projectConfig.ProjectID, sourceRemoteDir.ID, m.remoteDest.ID); err != nil {
			return fmt.Errorf("failed to move remote directory: %w", err)
		}

		// Move the local directory
		if err := os.Rename(sourceProjectPath, destProjectPath); err != nil {
			return fmt.Errorf("failed to move local directory: %w", err)
		}

		// TODO: Rename database paths from old to new path for all files in that path

		return nil
	})
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

func (m *Mover) do(src, dest string, fn func(sourceProjectPath, destProjectPath string, sourceRemoteFile *mcmodel.File) error) error {
	// Turn source and dest into project paths
	sourceProjectPath, err := m.projectPathTranslator.LocalToRemote(m.normalizePath(src))
	if err != nil {
		return fmt.Errorf("failed to translate local path to remote path: %w", err)
	}

	destProjectPath, err := m.projectPathTranslator.LocalToRemote(m.normalizePath(dest))
	if err != nil {
		return fmt.Errorf("failed to translate local path to remote path: %w", err)
	}

	// Retrieve the remote source file
	sourceRemoteFile, err := m.remoteGetter.GetFileByPath(m.projectConfig.ProjectID, sourceProjectPath)
	if err != nil {
		return fmt.Errorf("failed to get remote source file: %w", err)
	}
	return fn(sourceProjectPath, destProjectPath, sourceRemoteFile)
}

func (m *Mover) createMoveTransaction(src, dest string) *MoveTransaction {
	now := time.Now()
	return &MoveTransaction{
		Source:            src,
		SourceFullPath:    m.normalizePath(src),
		Dest:              dest,
		DestFullPath:      m.normalizePath(dest),
		CreatedAt:         now,
		UpdatedAt:         now,
		CommandWorkingDir: m.workingDir,
	}
}

func (m *Mover) normalizePath(path string) string {
	if strings.HasPrefix(path, "/") {
		// The user specified a full path. Let's clean it up to normalize it.
		return filepath.Clean(path)
	}

	// They specified a relative path to the working directory
	return filepath.Clean(filepath.Join(m.workingDir, path))
}

// loadRemoteDest loads the remote destination file for the mover. It should only be called from
// the NewMover function.
func (m *Mover) loadRemoteDest(destFullPath string) error {
	destPath, err := m.projectPathTranslator.LocalToRemote(destFullPath)
	if err != nil {
		return fmt.Errorf("failed to translate local path to remote path: %w", err)
	}
	m.remoteDest, err = m.remoteGetter.GetFileByPath(m.projectConfig.ProjectID, destPath)
	if err != nil {
		return fmt.Errorf("failed to get remote destination: %w", err)
	}

	return nil
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
