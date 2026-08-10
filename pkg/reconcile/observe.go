package reconcile

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/projectpath"
)

// FileRecordGetter loads persisted file state for a Materials Commons remote
// project path.
type FileRecordGetter interface {
	GetByPath(ctx context.Context, filePath string) (filedb.FileRecord, error)
}

// RemoteFileGetter loads remote Materials Commons file metadata by project path.
//
// *mcapi.Client satisfies this interface.
type RemoteFileGetter interface {
	GetFileByPath(projectID int, remotePath string) (*mcmodel.File, error)
}

// ObservationRunner gathers local, remote, and database state before invoking
// the pure Reconciler.
type ObservationRunner struct {
	ProjectID  int
	Translator projectpath.Translator
	Records    FileRecordGetter
	Remote     RemoteFileGetter
	Reconciler *Reconciler
	Now        func() time.Time
}

// FileState is the observed state and reconciliation decision for one path.
type FileState struct {
	Observation Observation
	Decision    Decision
}

// NewObservationRunner creates an ObservationRunner.
func NewObservationRunner(projectID int, translator projectpath.Translator, records FileRecordGetter, remote RemoteFileGetter, mode Mode) *ObservationRunner {
	return &ObservationRunner{
		ProjectID:  projectID,
		Translator: translator,
		Records:    records,
		Remote:     remote,
		Reconciler: New(mode),
		Now:        time.Now,
	}
}

// ObserveAndReconcile observes localPath, reads matching project database state,
// looks up the remote entry, and returns the reconciler decision.
func (r *ObservationRunner) ObserveAndReconcile(ctx context.Context, localPath string) (FileState, error) {
	if err := ctx.Err(); err != nil {
		return FileState{}, err
	}

	if err := r.observationRunnerProperlySetup(); err != nil {
		return FileState{}, err
	}

	if localPath == "" {
		return FileState{}, fmt.Errorf("%w: local path is required", ErrInvalidObservation)
	}

	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}

	remotePath, err := r.Translator.LocalToRemote(localPath)
	if err != nil {
		return FileState{}, err
	}

	localEntry, err := ObserveLocal(ctx, r.Translator, localPath, now)
	if err != nil {
		return FileState{}, err
	}

	fileRecord, hasRecord, err := loadFileRecord(ctx, r.Records, remotePath)
	if err != nil {
		return FileState{}, err
	}

	remoteFile, err := r.Remote.GetFileByPath(r.ProjectID, remotePath)
	if err != nil {
		if IsRemoteNotFound(err) {
			remoteFile = nil
		} else {
			return FileState{}, fmt.Errorf("get remote file by path %q: %w", remotePath, err)
		}
	}

	remoteEntry, err := RemoteEntryFromMCFile(remoteFile)
	if err != nil {
		return FileState{}, err
	}
	if remoteEntry != nil && remoteEntry.Path != remotePath {
		return FileState{}, fmt.Errorf("%w: remote returned path %q for requested path %q",
			ErrInvalidObservation, remoteEntry.Path, remotePath)
	}

	name := path.Base(remotePath)
	dir := path.Dir(remotePath)
	if dir == "." {
		dir = "/"
	}
	if localEntry != nil {
		name = localEntry.Name
		dir = localEntry.Dir
	}
	if remoteEntry != nil {
		name = remoteEntry.Name
		dir = remoteEntry.Dir
	}

	var recordPtr *filedb.FileRecord
	if hasRecord {
		recordPtr = &fileRecord
	}

	observation := Observation{
		RemotePath:  remotePath,
		Name:        name,
		Dir:         dir,
		FileRecord:  recordPtr,
		LocalEntry:  localEntry,
		RemoteEntry: remoteEntry,
	}

	decision, err := r.Reconciler.Reconcile(ctx, observation)
	if err != nil {
		return FileState{}, err
	}

	return FileState{
		Observation: observation,
		Decision:    decision,
	}, nil
}

func (r *ObservationRunner) observationRunnerProperlySetup() error {
	if r == nil {
		return fmt.Errorf("observation runner is nil")
	}
	if r.Records == nil {
		return fmt.Errorf("file record store is required")
	}
	if r.Remote == nil {
		return fmt.Errorf("remote file getter is required")
	}
	if r.Reconciler == nil {
		return fmt.Errorf("reconciler is required")
	}
	if r.ProjectID <= 0 {
		return fmt.Errorf("%w: project id must be positive", ErrInvalidObservation)
	}
	if r.Translator.ProjectRoot() == "" {
		return fmt.Errorf("%w: project path translator is not configured", ErrInvalidObservation)
	}
	return nil
}

func loadFileRecord(ctx context.Context, records FileRecordGetter, remotePath string) (filedb.FileRecord, bool, error) {
	record, err := records.GetByPath(ctx, remotePath)
	if err == nil {
		return record, true, nil
	}

	if errors.Is(err, filedb.ErrRecordNotFound) {
		return filedb.FileRecord{}, false, nil
	}

	return filedb.FileRecord{}, false, fmt.Errorf("get file record by path %q: %w", remotePath, err)
}

// RemoteEntryFromMCFile converts a gomcapi Materials Commons file model into
// the reconciler's remote observation model.
//
// A nil file returns nil.
func RemoteEntryFromMCFile(file *mcmodel.File) (*RemoteEntry, error) {
	if file == nil {
		return nil, nil
	}

	remotePath := file.Path
	if remotePath == "" {
		return nil, fmt.Errorf("%w: remote file path is empty", ErrInvalidObservation)
	}
	if !strings.HasPrefix(remotePath, "/") {
		return nil, fmt.Errorf("%w: remote file path %q must start with /", ErrInvalidObservation, remotePath)
	}

	dir := path.Dir(remotePath)
	if dir == "." {
		dir = "/"
	}

	name := file.Name
	if name == "" {
		name = path.Base(remotePath)
	}
	if name == "" || name == "." {
		return nil, fmt.Errorf("%w: remote file name is empty for path %q", ErrInvalidObservation, remotePath)
	}

	kind := KindFile
	if isRemoteDirectory(file) {
		kind = KindDir
	}

	var remoteFileID *int64
	if file.ID != 0 {
		id := int64(file.ID)
		remoteFileID = &id
	}

	return &RemoteEntry{
		Path:         remotePath,
		Name:         name,
		Dir:          dir,
		Kind:         kind,
		RemoteFileID: remoteFileID,
		Size:         int64(file.Size),
		CTimeNS:      file.CreatedAt.UnixNano(),
		MTimeNS:      file.UpdatedAt.UnixNano(),
		Checksum:     file.Checksum,
	}, nil
}

func isRemoteDirectory(file *mcmodel.File) bool {
	mimeType := strings.ToLower(file.MimeType)
	return mimeType == "directory" || mimeType == "inode/directory"
}
