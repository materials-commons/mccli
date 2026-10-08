package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
	"github.com/materials-commons/mccli/internal/services"
)

type Mover struct {
	projectConfig         config.Project
	store                 di.Store
	projectPathTranslator mc.ProjectPathTranslator
	remoteGetter          mc.FileDirectoryGetter
	remoteMover           mc.FileMover
	remoteRenamer         mc.FileRenamer
	remoteDest            *mcmodel.File
	opts                  MoverOpts
}

type MoverOpts struct {
	Force          bool
	MoveLocalOnly  bool
	MoveRemoteOnly bool
	DryRun         bool
	MoveBoth       bool
	WorkingDir     string
}

type remoteMoverFunc func(m *Mover, sourceProjectPath, destProjectPath string, sourceRemoteFile *mcmodel.File) error
type localMoverFunc func(m *Mover, src, dest, sourceProjectPath, destProjectPath string) error
type dryrunFunc func(src, dest string)
type doerFuncs struct {
	remoteMoveFunc remoteMoverFunc
	localMoveFunc  localMoverFunc
	dryrunFunc     dryrunFunc
}

func NewMover(ctx context.Context, deps di.Dependencies, dest string, opts MoverOpts) (*Mover, error) {
	var (
		m   Mover
		err error
	)

	m.opts = opts

	container, err := services.NewContainer(ctx, deps,
		services.WithProjectConfig(),
		services.WithProjectRoot(),
		services.WithGlobalConfig(),
		services.WithRemote(),
		services.WithProjectPathTranslator(),
		services.WithStore())
	if err != nil {
		return nil, err
	}

	m.projectConfig = container.MustProjectConfig()
	m.projectPathTranslator = container.MustProjectPathTranslator()
	m.store = container.MustStore()

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

	funcs := doerFuncs{
		remoteMoveFunc: func(m *Mover, sourceProjectPath, destProjectPath string, sourceRemoteFile *mcmodel.File) error {
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

			// We've renamed the file so it's now the remote destination. To track this change, we update
			// m.remoteDest to be the sourceRemoteFile. The Path for sourceRemoteFile will not be correct
			// because we aren't retrieving the updated remote version. However, all we really need is the ID.
			m.remoteDest = sourceRemoteFile

			return nil
		},
		localMoveFunc: func(m *Mover, src, dest, sourceProjectPath, destProjectPath string) error {
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
			f.Dir = path.Dir(destProjectPath)
			f.Name = path.Base(destProjectPath)
			if err := m.store.Upsert(context.Background(), f); err != nil {
				return fmt.Errorf("failed to update local file: %w", err)
			}

			return nil
		},
		dryrunFunc: func(src, dest string) {
			fmt.Printf("Rename File: %s to %s\n", m.normalizePath(src), m.normalizePath(dest))
		},
	}

	return m.do(src, dest, funcs)
}

func (m *Mover) RenameDir(src, dest string) error {
	if m.remoteDest != nil {
		return fmt.Errorf("destination already exists")
	}

	funcs := doerFuncs{
		remoteMoveFunc: func(m *Mover, sourceProjectPath, destProjectPath string, sourceRemoteDir *mcmodel.File) error {
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

			// We've renamed the directory so it's now the remote destination. To track this change, we update
			// m.remoteDest to be the sourceRemoteDir. The Path for sourceRemoteDir will not be correct
			// because we aren't retrieving the updated remote version. However, all we really need is the ID.
			m.remoteDest = sourceRemoteDir

			return nil
		},

		localMoveFunc: func(m *Mover, src, dest, sourceProjectPath, destProjectPath string) error {
			// Rename the local dir
			if err := os.Rename(m.normalizePath(src), m.normalizePath(dest)); err != nil {
				return fmt.Errorf("failed to rename local dir: %w", err)
			}

			if err := m.store.RenamePathPrefix(context.Background(), sourceProjectPath, destProjectPath); err != nil {
				return fmt.Errorf("failed to rename database paths: %w", err)
			}

			return nil
		},

		dryrunFunc: func(src, dest string) {
			fmt.Printf("Rename Dir: %s to %s\n", m.normalizePath(src), m.normalizePath(dest))
		},
	}

	return m.do(src, dest, funcs)
}

func (m *Mover) MoveFile(src, dest string) error {
	if m.remoteDest == nil {
		return errors.New("no remote destination")
	}

	if m.remoteDest.IsFile() == true {
		return fmt.Errorf("destination is a file")
	}

	funcs := doerFuncs{
		remoteMoveFunc: func(m *Mover, sourceProjectPath, destProjectPath string, sourceRemoteFile *mcmodel.File) error {
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

			return nil
		},

		localMoveFunc: func(m *Mover, src, dest, sourceProjectPath, destProjectPath string) error {
			// Move the local file
			destPath := filepath.Join(m.normalizePath(dest), filepath.Base(src))
			if err := os.Rename(m.normalizePath(src), destPath); err != nil {
				return fmt.Errorf("failed to move local file: %w", err)
			}

			// Update the local project database entry for this file
			f, err := m.store.GetByPath(context.Background(), sourceProjectPath)
			if err != nil {
				return fmt.Errorf("failed to get local file: %w", err)
			}

			f.Path = path.Join(destProjectPath, path.Base(sourceProjectPath))
			f.Dir = path.Dir(f.Path)
			f.Name = path.Base(f.Path)
			if err := m.store.Upsert(context.Background(), f); err != nil {
				return fmt.Errorf("failed to update local file: %w", err)
			}

			return nil
		},

		dryrunFunc: func(src, dest string) {
			fmt.Printf("Move File: %s to %s\n", m.normalizePath(src), m.normalizePath(dest))
		},
	}

	return m.do(src, dest, funcs)
}

func (m *Mover) MoveDir(src, dest string) error {
	if m.remoteDest == nil {
		return errors.New("no remote destination")
	}

	if m.remoteDest.IsFile() == true {
		return fmt.Errorf("destination is a file")
	}

	funcs := doerFuncs{
		remoteMoveFunc: func(m *Mover, sourceProjectPath, destProjectPath string, sourceRemoteDir *mcmodel.File) error {
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

			return nil
		},

		localMoveFunc: func(m *Mover, src, dest, sourceProjectPath, destProjectPath string) error {
			destPath := filepath.Join(m.normalizePath(dest), filepath.Base(src))
			if err := os.Rename(m.normalizePath(src), destPath); err != nil {
				return fmt.Errorf("failed to move local directory: %w", err)
			}

			if err := m.store.RenamePathPrefix(context.Background(), sourceProjectPath, destProjectPath); err != nil {
				return fmt.Errorf("failed to rename database paths: %w", err)
			}

			return nil
		},

		dryrunFunc: func(src, dest string) {
			fmt.Printf("Move Dir: %s to %s\n", m.normalizePath(src), m.normalizePath(dest))
		},
	}

	return m.do(src, dest, funcs)
}

func (m *Mover) GetRemoteFile(filePath string) (*mcmodel.File, error) {
	projectPath, err := m.projectPathTranslator.LocalToRemote(m.normalizePath(filePath))
	if err != nil {
		return nil, err
	}

	return m.remoteGetter.GetFileByPath(m.projectConfig.ProjectID, projectPath)
}

func (m *Mover) do(src, dest string, params doerFuncs) error {
	var (
		err               error
		sourceProjectPath string
		destProjectPath   string
		sourceRemoteFile  *mcmodel.File
	)
	// Turn source and dest into project paths
	sourceProjectPath, err = m.projectPathTranslator.LocalToRemote(m.normalizePath(src))
	if err != nil {
		return fmt.Errorf("failed to translate local path to remote path: %w", err)
	}

	destProjectPath, err = m.projectPathTranslator.LocalToRemote(m.normalizePath(dest))
	if err != nil {
		return fmt.Errorf("failed to translate local path to remote path: %w", err)
	}

	// Retrieve the most up-to-date remote source file. Only do this if the move involves remote files. This happens
	// when we are moving both local and remote files, or just remote files.
	if m.opts.MoveBoth || m.opts.MoveRemoteOnly {
		sourceRemoteFile, err = m.remoteGetter.GetFileByPath(m.projectConfig.ProjectID, sourceProjectPath)
		if err != nil {
			return fmt.Errorf("failed to get remote source file: %w", err)
		}
	}

	switch {
	case m.opts.DryRun:
		params.dryrunFunc(src, dest)
		return nil
	case m.opts.MoveBoth:
		if err := params.remoteMoveFunc(m, sourceProjectPath, destProjectPath, sourceRemoteFile); err != nil {
			return fmt.Errorf("failed to move remote file: %w", err)
		}
		return params.localMoveFunc(m, src, dest, sourceProjectPath, destProjectPath)
	case m.opts.MoveLocalOnly:
		return params.localMoveFunc(m, src, dest, sourceProjectPath, destProjectPath)
	case m.opts.MoveRemoteOnly:
		return params.remoteMoveFunc(m, sourceProjectPath, destProjectPath, sourceRemoteFile)
	default:
		return fmt.Errorf("invalid move option")
	}
}

//func (m *Mover) do(src, dest string, fn func(sourceProjectPath, destProjectPath string, sourceRemoteFile *mcmodel.File) error) error {
//	// Turn source and dest into project paths
//	sourceProjectPath, err := m.projectPathTranslator.LocalToRemote(m.normalizePath(src))
//	if err != nil {
//		return fmt.Errorf("failed to translate local path to remote path: %w", err)
//	}
//
//	destProjectPath, err := m.projectPathTranslator.LocalToRemote(m.normalizePath(dest))
//	if err != nil {
//		return fmt.Errorf("failed to translate local path to remote path: %w", err)
//	}
//
//	// Retrieve the remote source file
//	sourceRemoteFile, err := m.remoteGetter.GetFileByPath(m.projectConfig.ProjectID, sourceProjectPath)
//	if err != nil {
//		return fmt.Errorf("failed to get remote source file: %w", err)
//	}
//	return fn(sourceProjectPath, destProjectPath, sourceRemoteFile)
//}

func (m *Mover) createMoveTransaction(src, dest string) *MoveTransaction {
	now := time.Now()
	return &MoveTransaction{
		Source:            src,
		SourceFullPath:    m.normalizePath(src),
		Dest:              dest,
		DestFullPath:      m.normalizePath(dest),
		CreatedAt:         now,
		UpdatedAt:         now,
		CommandWorkingDir: m.opts.WorkingDir,
	}
}

func (m *Mover) normalizePath(path string) string {
	if strings.HasPrefix(path, "/") {
		// The user specified a full path. Let's clean it up to normalize it.
		return filepath.Clean(path)
	}

	// They specified a relative path to the working directory
	return filepath.Clean(filepath.Join(m.opts.WorkingDir, path))
}

// loadRemoteDest loads the remote destination file for the mover. It should only be called from
// the NewMover function.
func (m *Mover) loadRemoteDest(destFullPath string) error {
	destPath, err := m.projectPathTranslator.LocalToRemote(destFullPath)
	if err != nil {
		return fmt.Errorf("failed to translate local path to remote path: %w", err)
	}
	m.remoteDest, err = m.remoteGetter.GetFileByPath(m.projectConfig.ProjectID, destPath)
	// TODO: Check the type of error.
	// An error just means the remote destination doesn't exist yet. This is fine.
	//if err != nil {
	//	return fmt.Errorf("failed to get remote destination: %w", err)
	//}

	return nil
}

func getMoverRemotes(container *services.Container) (mc.FileDirectoryGetter, mc.FileMover, mc.FileRenamer, error) {

	remoteGetter, err := services.RequireRemoteAs[mc.FileDirectoryGetter](container, "FileDirectoryGetter")
	if err != nil {
		return nil, nil, nil, err
	}

	remoteMover, err := services.RequireRemoteAs[mc.FileMover](container, "FileMover")
	if err != nil {
		return nil, nil, nil, err
	}

	remoteRenamer, err := services.RequireRemoteAs[mc.FileRenamer](container, "FileRenamer")
	if err != nil {
		return nil, nil, nil, err
	}

	return remoteGetter, remoteMover, remoteRenamer, nil
}
