package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
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
	if r == nil {
		return FileState{}, fmt.Errorf("observation runner is nil")
	}
	if r.Records == nil {
		return FileState{}, fmt.Errorf("file record store is required")
	}
	if r.Remote == nil {
		return FileState{}, fmt.Errorf("remote file getter is required")
	}
	if r.Reconciler == nil {
		return FileState{}, fmt.Errorf("reconciler is required")
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
		if isRemoteNotFound(err) {
			remoteFile = nil
		} else {
			return FileState{}, fmt.Errorf("get remote file by path %q: %w", remotePath, err)
		}
	}

	remoteEntry := RemoteEntryFromMCFile(remoteFile)

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
func RemoteEntryFromMCFile(file *mcmodel.File) *RemoteEntry {
	if file == nil {
		return nil
	}

	remotePath := file.Path
	dir := path.Dir(remotePath)
	if dir == "." {
		dir = "/"
	}

	kind := KindFile
	if isRemoteDirectory(file) {
		kind = KindDir
	}

	return &RemoteEntry{
		Path:         remotePath,
		Name:         file.Name,
		Dir:          dir,
		Kind:         kind,
		RemoteFileID: int64(file.ID),
		Size:         int64(file.Size),
		CTimeNS:      file.CreatedAt.UnixNano(),
		MTimeNS:      file.UpdatedAt.UnixNano(),
		Checksum:     file.Checksum,
	}
}

func isRemoteDirectory(file *mcmodel.File) bool {
	mimeType := strings.ToLower(file.MimeType)
	return mimeType == "directory" || mimeType == "inode/directory"
}

func isRemoteNotFound(err error) bool {
	var apiErr *mcapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}

	return false
}
