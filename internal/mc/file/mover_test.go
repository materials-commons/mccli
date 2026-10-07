package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/filedb"
	"github.com/materials-commons/mccli/internal/mc"
)

func TestMoverDoAllMoveOptionCombinations(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		opts := MoverOpts{
			DryRun:         mask&1 != 0,
			MoveLocalOnly:  mask&2 != 0,
			MoveRemoteOnly: mask&4 != 0,
			MoveBoth:       mask&8 != 0,
		}

		t.Run(fmt.Sprintf("mask=%04b", mask), func(t *testing.T) {
			projectRoot := t.TempDir()
			opts.WorkingDir = projectRoot
			translator, err := mc.NewProjectPathTranslator(projectRoot)
			if err != nil {
				t.Fatalf("NewProjectPathTranslator() error = %v", err)
			}

			remote := &fakeMoverRemote{
				filesByPath: map[string]*mcmodel.File{
					"/src.txt": moverFileModel(10, "/src.txt", "src.txt"),
				},
			}

			m := &Mover{
				projectConfig:         testMoverProject(),
				projectPathTranslator: translator,
				remoteGetter:          remote,
				opts:                  opts,
			}

			var (
				dryrunCalls int
				localCalls  int
				remoteCalls int
			)

			err = m.do("src.txt", "dest.txt", doerFuncs{
				dryrunFunc: func(src, dest string) {
					dryrunCalls++
					if src != "src.txt" || dest != "dest.txt" {
						t.Fatalf("dryrunFunc(src, dest) = (%q, %q), want (src.txt, dest.txt)", src, dest)
					}
				},
				localMoveFunc: func(m *Mover, src, dest, sourceProjectPath, destProjectPath string) error {
					localCalls++
					if sourceProjectPath != "/src.txt" {
						t.Fatalf("sourceProjectPath = %q, want /src.txt", sourceProjectPath)
					}
					if destProjectPath != "/dest.txt" {
						t.Fatalf("destProjectPath = %q, want /dest.txt", destProjectPath)
					}
					return nil
				},
				remoteMoveFunc: func(m *Mover, sourceProjectPath, destProjectPath string, sourceRemoteFile *mcmodel.File) error {
					remoteCalls++
					if sourceProjectPath != "/src.txt" {
						t.Fatalf("sourceProjectPath = %q, want /src.txt", sourceProjectPath)
					}
					if destProjectPath != "/dest.txt" {
						t.Fatalf("destProjectPath = %q, want /dest.txt", destProjectPath)
					}
					if sourceRemoteFile == nil || sourceRemoteFile.ID != 10 {
						t.Fatalf("sourceRemoteFile = %#v, want id 10", sourceRemoteFile)
					}
					return nil
				},
			})

			wantErr := !opts.DryRun && !opts.MoveBoth && !opts.MoveLocalOnly && !opts.MoveRemoteOnly
			if wantErr {
				if err == nil {
					t.Fatalf("do() error = nil, want invalid option error for opts %+v", opts)
				}
				return
			}
			if err != nil {
				t.Fatalf("do() error = %v", err)
			}

			wantDryrun := 0
			wantLocal := 0
			wantRemote := 0

			wantRemoteGet := 0
			if opts.MoveBoth || opts.MoveRemoteOnly {
				wantRemoteGet = 1
			}

			switch {
			case opts.DryRun:
				wantDryrun = 1
				//if opts.MoveBoth || opts.MoveRemoteOnly {
				//	wantRemoteGet = 1
				//}
			case opts.MoveBoth:
				wantRemote = 1
				wantLocal = 1
				//wantRemoteGet = 1
			case opts.MoveLocalOnly:
				wantLocal = 1
			case opts.MoveRemoteOnly:
				wantRemote = 1
				//wantRemoteGet = 1
			}

			if dryrunCalls != wantDryrun {
				t.Fatalf("dryrunCalls = %d, want %d for opts %+v", dryrunCalls, wantDryrun, opts)
			}
			if localCalls != wantLocal {
				t.Fatalf("localCalls = %d, want %d for opts %+v", localCalls, wantLocal, opts)
			}
			if remoteCalls != wantRemote {
				t.Fatalf("remoteCalls = %d, want %d for opts %+v", remoteCalls, wantRemote, opts)
			}
			if remote.getByPathCalls != wantRemoteGet {
				t.Fatalf("remote get calls = %d, want %d for opts %+v", remote.getByPathCalls, wantRemoteGet, opts)
			}
		})
	}
}

func TestMoverDoDryRunDoesNotMutateEvenWhenOtherFlagsAreSet(t *testing.T) {
	for _, opts := range []MoverOpts{
		{DryRun: true},
		{DryRun: true, MoveLocalOnly: true},
		{DryRun: true, MoveRemoteOnly: true},
		{DryRun: true, MoveBoth: true},
		{DryRun: true, MoveLocalOnly: true, MoveRemoteOnly: true, MoveBoth: true},
	} {
		t.Run(fmt.Sprintf("%+v", opts), func(t *testing.T) {
			projectRoot := t.TempDir()
			opts.WorkingDir = projectRoot

			src := filepath.Join(projectRoot, "src.txt")
			dest := filepath.Join(projectRoot, "dest.txt")
			if err := os.WriteFile(src, []byte("contents"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			translator, err := mc.NewProjectPathTranslator(projectRoot)
			if err != nil {
				t.Fatalf("NewProjectPathTranslator() error = %v", err)
			}

			remote := &fakeMoverRemote{
				filesByPath: map[string]*mcmodel.File{
					"/src.txt": moverFileModel(10, "/src.txt", "src.txt"),
				},
			}
			store := &fakeMoverStore{
				recordsByPath: map[string]filedb.FileRecord{
					"/src.txt": moverRecord("/src.txt"),
				},
			}

			m := &Mover{
				projectConfig:         testMoverProject(),
				store:                 store,
				projectPathTranslator: translator,
				remoteGetter:          remote,
				remoteRenamer:         remote,
				opts:                  opts,
			}

			err = m.RenameFile(src, dest)
			if err != nil {
				t.Fatalf("RenameFile() error = %v", err)
			}

			if _, err := os.Stat(src); err != nil {
				t.Fatalf("source stat error = %v, want source preserved", err)
			}
			if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("dest stat error = %v, want destination absent", err)
			}
			if len(remote.renamedFiles) != 0 {
				t.Fatalf("renamedFiles = %#v, want none", remote.renamedFiles)
			}
			if len(store.upserts) != 0 {
				t.Fatalf("upserts = %#v, want none", store.upserts)
			}
		})
	}
}

func TestMoverRenameFileWhenDestinationDoesNotExist(t *testing.T) {
	tests := []struct {
		name           string
		opts           MoverOpts
		wantLocalMoved bool
		wantRemoteMove bool
		wantDBUpdate   bool
	}{
		{
			name:           "local only",
			opts:           MoverOpts{MoveLocalOnly: true},
			wantLocalMoved: true,
			wantRemoteMove: false,
			wantDBUpdate:   true,
		},
		{
			name:           "remote only",
			opts:           MoverOpts{MoveRemoteOnly: true},
			wantLocalMoved: false,
			wantRemoteMove: true,
			wantDBUpdate:   false,
		},
		{
			name:           "move both",
			opts:           MoverOpts{MoveBoth: true},
			wantLocalMoved: true,
			wantRemoteMove: true,
			wantDBUpdate:   true,
		},
		{
			name:           "dry run",
			opts:           MoverOpts{DryRun: true, MoveBoth: true},
			wantLocalMoved: false,
			wantRemoteMove: false,
			wantDBUpdate:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			tt.opts.WorkingDir = projectRoot

			src := filepath.Join(projectRoot, "src.txt")
			dest := filepath.Join(projectRoot, "renamed.txt")
			if err := os.WriteFile(src, []byte("contents"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			translator, err := mc.NewProjectPathTranslator(projectRoot)
			if err != nil {
				t.Fatalf("NewProjectPathTranslator() error = %v", err)
			}

			store := &fakeMoverStore{
				recordsByPath: map[string]filedb.FileRecord{
					"/src.txt": moverRecord("/src.txt"),
				},
			}
			remote := &fakeMoverRemote{
				filesByPath: map[string]*mcmodel.File{
					"/src.txt": moverFileModel(101, "/src.txt", "src.txt"),
				},
			}

			m := &Mover{
				projectConfig:         testMoverProject(),
				store:                 store,
				projectPathTranslator: translator,
				remoteGetter:          remote,
				remoteRenamer:         remote,
				opts:                  tt.opts,
			}

			err = m.RenameFile(src, dest)
			if err != nil {
				t.Fatalf("RenameFile() error = %v", err)
			}

			assertLocalRename(t, src, dest, tt.wantLocalMoved)
			assertRemoteRenamed(t, remote.renamedFiles, 101, "/renamed.txt", tt.wantRemoteMove)

			if tt.wantDBUpdate {
				if len(store.upserts) != 1 {
					t.Fatalf("upserts = %#v, want exactly one DB update", store.upserts)
				}
				got := store.upserts[0]
				if got.Path != "/renamed.txt" || got.Dir != "/" || got.Name != "renamed.txt" {
					t.Fatalf("upsert = %+v, want path=/renamed.txt dir=/ name=renamed.txt", got)
				}
			} else if len(store.upserts) != 0 {
				t.Fatalf("upserts = %#v, want none", store.upserts)
			}
		})
	}
}

func TestMoverRenameDirWhenDestinationDoesNotExist(t *testing.T) {
	tests := []struct {
		name           string
		opts           MoverOpts
		wantLocalMoved bool
		wantRemoteMove bool
		wantDBRename   bool
	}{
		{
			name:           "local only",
			opts:           MoverOpts{MoveLocalOnly: true},
			wantLocalMoved: true,
			wantRemoteMove: false,
			wantDBRename:   true,
		},
		{
			name:           "remote only",
			opts:           MoverOpts{MoveRemoteOnly: true},
			wantLocalMoved: false,
			wantRemoteMove: true,
			wantDBRename:   false,
		},
		{
			name:           "move both",
			opts:           MoverOpts{MoveBoth: true},
			wantLocalMoved: true,
			wantRemoteMove: true,
			wantDBRename:   true,
		},
		{
			name:           "dry run",
			opts:           MoverOpts{DryRun: true, MoveBoth: true},
			wantLocalMoved: false,
			wantRemoteMove: false,
			wantDBRename:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			tt.opts.WorkingDir = projectRoot

			src := filepath.Join(projectRoot, "Dir")
			dest := filepath.Join(projectRoot, "RenamedDir")
			if err := os.MkdirAll(filepath.Join(src, "Sub"), 0o755); err != nil {
				t.Fatalf("MkdirAll() error = %v", err)
			}
			if err := os.WriteFile(filepath.Join(src, "Sub", "file.txt"), []byte("contents"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			translator, err := mc.NewProjectPathTranslator(projectRoot)
			if err != nil {
				t.Fatalf("NewProjectPathTranslator() error = %v", err)
			}

			store := &fakeMoverStore{}
			remote := &fakeMoverRemote{
				filesByPath: map[string]*mcmodel.File{
					"/Dir": moverDirModel(201, "/Dir", "Dir"),
				},
			}

			m := &Mover{
				projectConfig:         testMoverProject(),
				store:                 store,
				projectPathTranslator: translator,
				remoteGetter:          remote,
				remoteRenamer:         remote,
				opts:                  tt.opts,
			}

			err = m.RenameDir(src, dest)
			if err != nil {
				t.Fatalf("RenameDir() error = %v", err)
			}

			assertLocalRename(t, src, dest, tt.wantLocalMoved)
			assertRemoteRenamed(t, remote.renamedDirs, 201, "/RenamedDir", tt.wantRemoteMove)

			if tt.wantDBRename {
				if len(store.renamedPrefixes) != 1 {
					t.Fatalf("renamedPrefixes = %#v, want exactly one DB prefix rename", store.renamedPrefixes)
				}
				if store.renamedPrefixes[0] != [2]string{"/Dir", "/RenamedDir"} {
					t.Fatalf("renamedPrefixes[0] = %#v, want [/Dir /RenamedDir]", store.renamedPrefixes[0])
				}
			} else if len(store.renamedPrefixes) != 0 {
				t.Fatalf("renamedPrefixes = %#v, want none", store.renamedPrefixes)
			}
		})
	}
}

func TestMoverMoveFileToExistingDirectory(t *testing.T) {
	tests := []struct {
		name           string
		opts           MoverOpts
		wantLocalMoved bool
		wantRemoteMove bool
		wantDBUpdate   bool
	}{
		{
			name:           "local only",
			opts:           MoverOpts{MoveLocalOnly: true},
			wantLocalMoved: true,
			wantRemoteMove: false,
			wantDBUpdate:   true,
		},
		{
			name:           "remote only",
			opts:           MoverOpts{MoveRemoteOnly: true},
			wantLocalMoved: false,
			wantRemoteMove: true,
			wantDBUpdate:   false,
		},
		{
			name:           "move both",
			opts:           MoverOpts{MoveBoth: true},
			wantLocalMoved: true,
			wantRemoteMove: true,
			wantDBUpdate:   true,
		},
		{
			name:           "dry run",
			opts:           MoverOpts{DryRun: true, MoveBoth: true},
			wantLocalMoved: false,
			wantRemoteMove: false,
			wantDBUpdate:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			tt.opts.WorkingDir = projectRoot

			src := filepath.Join(projectRoot, "src.txt")
			destDir := filepath.Join(projectRoot, "Dest")
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				t.Fatalf("MkdirAll() error = %v", err)
			}
			if err := os.WriteFile(src, []byte("contents"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			translator, err := mc.NewProjectPathTranslator(projectRoot)
			if err != nil {
				t.Fatalf("NewProjectPathTranslator() error = %v", err)
			}

			store := &fakeMoverStore{
				recordsByPath: map[string]filedb.FileRecord{
					"/src.txt": moverRecord("/src.txt"),
				},
			}
			remote := &fakeMoverRemote{
				filesByPath: map[string]*mcmodel.File{
					"/src.txt": moverFileModel(301, "/src.txt", "src.txt"),
				},
			}

			m := &Mover{
				projectConfig:         testMoverProject(),
				store:                 store,
				projectPathTranslator: translator,
				remoteGetter:          remote,
				remoteMover:           remote,
				remoteDest:            moverDirModel(300, "/Dest", "Dest"),
				opts:                  tt.opts,
			}

			err = m.MoveFile(src, destDir)
			if err != nil {
				t.Fatalf("MoveFile() error = %v", err)
			}

			movedPath := filepath.Join(destDir, "src.txt")
			assertLocalRename(t, src, movedPath, tt.wantLocalMoved)
			assertRemoteMoved(t, remote.movedFiles, 301, 300, tt.wantRemoteMove)

			if tt.wantDBUpdate {
				if len(store.upserts) != 1 {
					t.Fatalf("upserts = %#v, want exactly one DB update", store.upserts)
				}
				got := store.upserts[0]
				if got.Path != "/Dest/src.txt" || got.Dir != "/Dest" || got.Name != "src.txt" {
					t.Fatalf("upsert = %+v, want path=/Dest/src.txt dir=/Dest name=src.txt", got)
				}
			} else if len(store.upserts) != 0 {
				t.Fatalf("upserts = %#v, want none", store.upserts)
			}
		})
	}
}

func TestMoverMoveDirToExistingDirectory(t *testing.T) {
	tests := []struct {
		name           string
		opts           MoverOpts
		wantLocalMoved bool
		wantRemoteMove bool
		wantDBRename   bool
	}{
		{
			name:           "local only",
			opts:           MoverOpts{MoveLocalOnly: true},
			wantLocalMoved: true,
			wantRemoteMove: false,
			wantDBRename:   true,
		},
		{
			name:           "remote only",
			opts:           MoverOpts{MoveRemoteOnly: true},
			wantLocalMoved: false,
			wantRemoteMove: true,
			wantDBRename:   false,
		},
		{
			name:           "move both",
			opts:           MoverOpts{MoveBoth: true},
			wantLocalMoved: true,
			wantRemoteMove: true,
			wantDBRename:   true,
		},
		{
			name:           "dry run",
			opts:           MoverOpts{DryRun: true, MoveBoth: true},
			wantLocalMoved: false,
			wantRemoteMove: false,
			wantDBRename:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			tt.opts.WorkingDir = projectRoot

			src := filepath.Join(projectRoot, "Dir")
			destDir := filepath.Join(projectRoot, "Dest")
			if err := os.MkdirAll(filepath.Join(src, "Sub"), 0o755); err != nil {
				t.Fatalf("MkdirAll(src) error = %v", err)
			}
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				t.Fatalf("MkdirAll(dest) error = %v", err)
			}
			if err := os.WriteFile(filepath.Join(src, "Sub", "file.txt"), []byte("contents"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			translator, err := mc.NewProjectPathTranslator(projectRoot)
			if err != nil {
				t.Fatalf("NewProjectPathTranslator() error = %v", err)
			}

			store := &fakeMoverStore{}
			remote := &fakeMoverRemote{
				filesByPath: map[string]*mcmodel.File{
					"/Dir": moverDirModel(401, "/Dir", "Dir"),
				},
			}

			m := &Mover{
				projectConfig:         testMoverProject(),
				store:                 store,
				projectPathTranslator: translator,
				remoteGetter:          remote,
				remoteMover:           remote,
				remoteDest:            moverDirModel(400, "/Dest", "Dest"),
				opts:                  tt.opts,
			}

			err = m.MoveDir(src, destDir)
			if err != nil {
				t.Fatalf("MoveDir() error = %v", err)
			}

			movedPath := filepath.Join(destDir, "Dir")
			assertLocalRename(t, src, movedPath, tt.wantLocalMoved)
			assertRemoteMoved(t, remote.movedDirs, 401, 400, tt.wantRemoteMove)

			if tt.wantDBRename {
				if len(store.renamedPrefixes) != 1 {
					t.Fatalf("renamedPrefixes = %#v, want exactly one DB prefix rename", store.renamedPrefixes)
				}
				if store.renamedPrefixes[0] != [2]string{"/Dir", "/Dest"} {
					t.Fatalf("renamedPrefixes[0] = %#v, want [/Dir /Dest]", store.renamedPrefixes[0])
				}
			} else if len(store.renamedPrefixes) != 0 {
				t.Fatalf("renamedPrefixes = %#v, want none", store.renamedPrefixes)
			}
		})
	}
}

func TestMoverRenameRefusesExistingRemoteDestination(t *testing.T) {
	projectRoot := t.TempDir()
	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	m := &Mover{
		projectPathTranslator: translator,
		remoteDest:            moverFileModel(99, "/dest.txt", "dest.txt"),
		opts:                  MoverOpts{MoveBoth: true, WorkingDir: projectRoot},
	}

	if err := m.RenameFile("src.txt", "dest.txt"); err == nil {
		t.Fatal("RenameFile() error = nil, want destination already exists")
	}
	if err := m.RenameDir("SrcDir", "DestDir"); err == nil {
		t.Fatal("RenameDir() error = nil, want destination already exists")
	}
}

func TestMoverRemoteTypeValidation(t *testing.T) {
	projectRoot := t.TempDir()
	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	tests := []struct {
		name       string
		call       func(*Mover) error
		remoteSrc  *mcmodel.File
		remoteDest *mcmodel.File
		wantErr    string
	}{
		{
			name:      "rename file rejects directory source",
			call:      func(m *Mover) error { return m.RenameFile("Dir", "renamed.txt") },
			remoteSrc: moverDirModel(10, "/Dir", "Dir"),
			wantErr:   "source is a directory",
		},
		{
			name:      "rename dir rejects file source",
			call:      func(m *Mover) error { return m.RenameDir("file.txt", "RenamedDir") },
			remoteSrc: moverFileModel(11, "/file.txt", "file.txt"),
			wantErr:   "source is a file",
		},
		{
			name:       "move file rejects directory source",
			call:       func(m *Mover) error { return m.MoveFile("Dir", "Dest") },
			remoteSrc:  moverDirModel(12, "/Dir", "Dir"),
			remoteDest: moverDirModel(20, "/Dest", "Dest"),
			wantErr:    "source is a directory",
		},
		{
			name:       "move dir rejects file source",
			call:       func(m *Mover) error { return m.MoveDir("file.txt", "Dest") },
			remoteSrc:  moverFileModel(13, "/file.txt", "file.txt"),
			remoteDest: moverDirModel(20, "/Dest", "Dest"),
			wantErr:    "source is a file",
		},
		{
			name:       "move file rejects file destination",
			call:       func(m *Mover) error { return m.MoveFile("file.txt", "dest.txt") },
			remoteSrc:  moverFileModel(14, "/file.txt", "file.txt"),
			remoteDest: moverFileModel(21, "/dest.txt", "dest.txt"),
			wantErr:    "destination is a file",
		},
		{
			name:       "move dir rejects file destination",
			call:       func(m *Mover) error { return m.MoveDir("Dir", "dest.txt") },
			remoteSrc:  moverDirModel(15, "/Dir", "Dir"),
			remoteDest: moverFileModel(22, "/dest.txt", "dest.txt"),
			wantErr:    "destination is a file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remotePath := tt.remoteSrc.Path
			remote := &fakeMoverRemote{
				filesByPath: map[string]*mcmodel.File{
					remotePath: tt.remoteSrc,
				},
			}

			m := &Mover{
				projectConfig:         testMoverProject(),
				projectPathTranslator: translator,
				remoteGetter:          remote,
				remoteMover:           remote,
				remoteRenamer:         remote,
				remoteDest:            tt.remoteDest,
				opts:                  MoverOpts{MoveRemoteOnly: true, WorkingDir: projectRoot},
			}

			err := tt.call(m)
			if err == nil {
				t.Fatalf("%s error = nil, want %q", tt.name, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("%s error = %v, want containing %q", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestMoverLoadRemoteDestMissingDestinationIsRenameFriendly(t *testing.T) {
	projectRoot := t.TempDir()
	translator, err := mc.NewProjectPathTranslator(projectRoot)
	if err != nil {
		t.Fatalf("NewProjectPathTranslator() error = %v", err)
	}

	remote := &fakeMoverRemote{
		getErr: errors.New("not found"),
	}

	m := &Mover{
		projectConfig:         testMoverProject(),
		projectPathTranslator: translator,
		remoteGetter:          remote,
		opts: MoverOpts{
			WorkingDir: projectRoot,
		},
	}

	err = m.loadRemoteDest(filepath.Join(projectRoot, "does-not-exist.txt"))
	if err != nil {
		t.Fatalf("loadRemoteDest() error = %v, want nil when destination is missing", err)
	}
	if m.remoteDest != nil {
		t.Fatalf("remoteDest = %#v, want nil for missing destination", m.remoteDest)
	}
	if remote.gotGetPath != "/does-not-exist.txt" {
		t.Fatalf("gotGetPath = %q, want /does-not-exist.txt", remote.gotGetPath)
	}
}

func assertLocalRename(t *testing.T, src, dest string, wantMoved bool) {
	t.Helper()

	_, srcErr := os.Stat(src)
	_, destErr := os.Stat(dest)

	if wantMoved {
		if !errors.Is(srcErr, os.ErrNotExist) {
			t.Fatalf("source stat error = %v, want source removed", srcErr)
		}
		if destErr != nil {
			t.Fatalf("dest stat error = %v, want destination present", destErr)
		}
		return
	}

	if srcErr != nil {
		t.Fatalf("source stat error = %v, want source preserved", srcErr)
	}
	if !errors.Is(destErr, os.ErrNotExist) {
		t.Fatalf("dest stat error = %v, want destination absent", destErr)
	}
}

func assertRemoteRenamed(t *testing.T, calls []moverRemoteRenameCall, wantID int, wantPath string, wantCalled bool) {
	t.Helper()

	if !wantCalled {
		if len(calls) != 0 {
			t.Fatalf("remote rename calls = %#v, want none", calls)
		}
		return
	}

	if len(calls) != 1 {
		t.Fatalf("remote rename calls = %#v, want exactly one", calls)
	}
	if calls[0].fileID != wantID || calls[0].newName != wantPath {
		t.Fatalf("remote rename call = %#v, want fileID=%d newName=%q", calls[0], wantID, wantPath)
	}
}

func assertRemoteMoved(t *testing.T, calls []moverRemoteMoveCall, wantID, wantDestID int, wantCalled bool) {
	t.Helper()

	if !wantCalled {
		if len(calls) != 0 {
			t.Fatalf("remote move calls = %#v, want none", calls)
		}
		return
	}

	if len(calls) != 1 {
		t.Fatalf("remote move calls = %#v, want exactly one", calls)
	}
	if calls[0].fileID != wantID || calls[0].toDirectoryID != wantDestID {
		t.Fatalf("remote move call = %#v, want fileID=%d toDirectoryID=%d", calls[0], wantID, wantDestID)
	}
}

func moverRecord(remotePath string) filedb.FileRecord {
	return filedb.FileRecord{
		Path: remotePath,
		Dir:  path.Dir(remotePath),
		Name: path.Base(remotePath),
	}
}

func moverFileModel(id int, remotePath, name string) *mcmodel.File {
	return &mcmodel.File{
		ID:        id,
		Path:      remotePath,
		Name:      name,
		Size:      8,
		MimeType:  "text/plain",
		Checksum:  "checksum",
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	}
}

func moverDirModel(id int, remotePath, name string) *mcmodel.File {
	return &mcmodel.File{
		ID:        id,
		Path:      remotePath,
		Name:      name,
		MimeType:  "directory",
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	}
}

func testMoverProject() config.Project {
	return config.Project{ProjectID: 123}
}

type fakeMoverStore struct {
	recordsByPath map[string]filedb.FileRecord
	getErr        error
	upsertErr     error
	renameErr     error

	upserts         []filedb.FileRecord
	renamedPrefixes [][2]string
	closed          bool
}

func (s *fakeMoverStore) GetByPath(ctx context.Context, filePath string) (filedb.FileRecord, error) {
	if s.getErr != nil {
		return filedb.FileRecord{}, s.getErr
	}

	record, ok := s.recordsByPath[filePath]
	if !ok {
		return filedb.FileRecord{}, filedb.ErrRecordNotFound
	}

	return record, nil
}

func (s *fakeMoverStore) ListByDir(ctx context.Context, dir string) ([]filedb.FileRecord, error) {
	var records []filedb.FileRecord
	for _, record := range s.recordsByPath {
		if record.Dir == dir {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *fakeMoverStore) DeleteByPath(ctx context.Context, filePath string) error {
	return nil
}

func (s *fakeMoverStore) Close(ctx context.Context) error {
	s.closed = true
	return nil
}

func (s *fakeMoverStore) Upsert(ctx context.Context, record filedb.FileRecord) error {
	s.upserts = append(s.upserts, record)
	if s.upsertErr != nil {
		return s.upsertErr
	}
	return nil
}

func (s *fakeMoverStore) RenamePathPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	s.renamedPrefixes = append(s.renamedPrefixes, [2]string{oldPrefix, newPrefix})
	if s.renameErr != nil {
		return s.renameErr
	}
	return nil
}

type moverRemoteRenameCall struct {
	projectID int
	fileID    int
	newName   string
}

type moverRemoteMoveCall struct {
	projectID     int
	fileID        int
	toDirectoryID int
}

type fakeMoverRemote struct {
	filesByPath map[string]*mcmodel.File

	getErr    error
	listErr   error
	moveErr   error
	renameErr error

	gotProjectID   int
	gotGetPath     string
	gotListPath    string
	getByPathCalls int

	movedFiles   []moverRemoteMoveCall
	movedDirs    []moverRemoteMoveCall
	renamedFiles []moverRemoteRenameCall
	renamedDirs  []moverRemoteRenameCall
}

func (r *fakeMoverRemote) GetFile(projectID, fileID int) (*mcmodel.File, error) {
	return nil, fmt.Errorf("GetFile should not be called by mover tests")
}

func (r *fakeMoverRemote) GetFileByPath(projectID int, filePath string) (*mcmodel.File, error) {
	r.gotProjectID = projectID
	r.gotGetPath = filePath
	r.getByPathCalls++

	if r.getErr != nil {
		return nil, r.getErr
	}

	file, ok := r.filesByPath[filePath]
	if !ok {
		return nil, fmt.Errorf("file not configured for path %q", filePath)
	}

	return file, nil
}

func (r *fakeMoverRemote) ListDirectoryByPath(projectID int, dirPath string) ([]mcmodel.File, error) {
	r.gotProjectID = projectID
	r.gotListPath = dirPath

	if r.listErr != nil {
		return nil, r.listErr
	}

	return nil, nil
}

func (r *fakeMoverRemote) MoveFile(projectID int, fileID int, toDirectoryID int) error {
	r.movedFiles = append(r.movedFiles, moverRemoteMoveCall{
		projectID:     projectID,
		fileID:        fileID,
		toDirectoryID: toDirectoryID,
	})

	if r.moveErr != nil {
		return r.moveErr
	}

	return nil
}

func (r *fakeMoverRemote) MoveDirectory(projectID int, dirID int, toDirectoryID int) error {
	r.movedDirs = append(r.movedDirs, moverRemoteMoveCall{
		projectID:     projectID,
		fileID:        dirID,
		toDirectoryID: toDirectoryID,
	})

	if r.moveErr != nil {
		return r.moveErr
	}

	return nil
}

func (r *fakeMoverRemote) RenameFile(projectID int, fileID int, newName string) error {
	r.renamedFiles = append(r.renamedFiles, moverRemoteRenameCall{
		projectID: projectID,
		fileID:    fileID,
		newName:   newName,
	})

	if r.renameErr != nil {
		return r.renameErr
	}

	return nil
}

func (r *fakeMoverRemote) RenameDirectory(projectID int, dirID int, newName string) error {
	r.renamedDirs = append(r.renamedDirs, moverRemoteRenameCall{
		projectID: projectID,
		fileID:    dirID,
		newName:   newName,
	})

	if r.renameErr != nil {
		return r.renameErr
	}

	return nil
}
