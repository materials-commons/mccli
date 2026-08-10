package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcapi "github.com/materials-commons/gomcapi"
	"github.com/materials-commons/hydra/pkg/mcdb/mcmodel"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/projectpath"
)

func TestObservationRunnerLocalOnlyUpload(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "Dir1", "file.txt")
	writeTestFile(t, localPath, "hello")

	translator := mustTranslator(t, projectRoot)
	records := fakeRecordStore{}
	remote := &fakeRemoteGetter{
		err: &mcapi.APIError{StatusCode: http.StatusNotFound, Status: "404 Not Found"},
	}

	runner := NewObservationRunner(123, translator, records, remote, ModeUpload)
	runner.Reconciler.WithChecksumFunc(fakeChecksum("local-md5"))
	runner.Now = fixedNow

	state, err := runner.ObserveAndReconcile(ctx, localPath)
	if err != nil {
		t.Fatalf("ObserveAndReconcile() error = %v", err)
	}

	if state.Observation.RemotePath != "/Dir1/file.txt" {
		t.Fatalf("RemotePath = %q, want /Dir1/file.txt", state.Observation.RemotePath)
	}
	if state.Observation.LocalEntry == nil {
		t.Fatal("LocalEntry = nil, want local entry")
	}
	if state.Observation.RemoteEntry != nil {
		t.Fatalf("RemoteEntry = %#v, want nil", state.Observation.RemoteEntry)
	}
	if state.Observation.FileRecord != nil {
		t.Fatalf("FileRecord = %#v, want nil", state.Observation.FileRecord)
	}
	assertDecision(t, state.Decision, ActionUpload, true)
}

func TestObservationRunnerRemoteOnlyDownload(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "Dir1", "missing.txt")

	translator := mustTranslator(t, projectRoot)
	records := fakeRecordStore{}
	remote := &fakeRemoteGetter{
		file: &mcmodel.File{
			ID:        77,
			Path:      "/Dir1/missing.txt",
			Name:      "missing.txt",
			Size:      123,
			MimeType:  "text/plain",
			Checksum:  "remote-md5",
			CreatedAt: time.Unix(10, 0),
			UpdatedAt: time.Unix(20, 0),
		},
	}

	runner := NewObservationRunner(123, translator, records, remote, ModeDownload)
	runner.Now = fixedNow

	state, err := runner.ObserveAndReconcile(ctx, localPath)
	if err != nil {
		t.Fatalf("ObserveAndReconcile() error = %v", err)
	}

	if state.Observation.LocalEntry != nil {
		t.Fatalf("LocalEntry = %#v, want nil", state.Observation.LocalEntry)
	}
	if state.Observation.RemoteEntry == nil {
		t.Fatal("RemoteEntry = nil, want remote entry")
	}
	if state.Observation.RemoteEntry.RemoteFileID == nil || *state.Observation.RemoteEntry.RemoteFileID != 77 {
		t.Fatalf("RemoteFileID = %d, want 77", state.Observation.RemoteEntry.RemoteFileID)
	}
	assertDecision(t, state.Decision, ActionDownload, true)
}

func TestObservationRunnerBothWithRecord(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "Dir1", "file.txt")
	writeTestFile(t, localPath, "hello")

	translator := mustTranslator(t, projectRoot)
	remoteID := int64(77)
	records := fakeRecordStore{
		records: map[string]filedb.FileRecord{
			"/Dir1/file.txt": {
				Path:         "/Dir1/file.txt",
				Dir:          "/Dir1",
				Name:         "file.txt",
				LocalSize:    5,
				RemoteFileID: &remoteID,
			},
		},
	}
	remote := &fakeRemoteGetter{
		file: &mcmodel.File{
			ID:        77,
			Path:      "/Dir1/file.txt",
			Name:      "file.txt",
			Size:      5,
			MimeType:  "text/plain",
			Checksum:  "remote-md5",
			CreatedAt: time.Unix(10, 0),
			UpdatedAt: time.Unix(20, 0),
		},
	}

	runner := NewObservationRunner(123, translator, records, remote, ModeStatus)
	runner.Now = fixedNow

	state, err := runner.ObserveAndReconcile(ctx, localPath)
	if err != nil {
		t.Fatalf("ObserveAndReconcile() error = %v", err)
	}

	if state.Observation.FileRecord == nil {
		t.Fatal("FileRecord = nil, want record")
	}
	if state.Observation.LocalEntry == nil {
		t.Fatal("LocalEntry = nil, want local entry")
	}
	if state.Observation.RemoteEntry == nil {
		t.Fatal("RemoteEntry = nil, want remote entry")
	}
	if state.Observation.Name != "file.txt" {
		t.Fatalf("Name = %q, want file.txt", state.Observation.Name)
	}
}

func TestObservationRunnerRemoteNotFoundIsNotFatal(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "file.txt")
	writeTestFile(t, localPath, "hello")

	translator := mustTranslator(t, projectRoot)
	records := fakeRecordStore{}
	remote := &fakeRemoteGetter{
		err: &mcapi.APIError{StatusCode: http.StatusNotFound, Status: "404 Not Found"},
	}

	runner := NewObservationRunner(123, translator, records, remote, ModeStatus)
	runner.Now = fixedNow

	state, err := runner.ObserveAndReconcile(ctx, localPath)
	if err != nil {
		t.Fatalf("ObserveAndReconcile() error = %v", err)
	}

	if state.Observation.RemoteEntry != nil {
		t.Fatalf("RemoteEntry = %#v, want nil for 404", state.Observation.RemoteEntry)
	}
}

func TestObservationRunnerRemoteErrorIsFatal(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "file.txt")
	writeTestFile(t, localPath, "hello")

	translator := mustTranslator(t, projectRoot)
	records := fakeRecordStore{}
	remote := &fakeRemoteGetter{
		err: &mcapi.APIError{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"},
	}

	runner := NewObservationRunner(123, translator, records, remote, ModeStatus)
	runner.Now = fixedNow

	_, err := runner.ObserveAndReconcile(ctx, localPath)
	if err == nil {
		t.Fatal("ObserveAndReconcile() error = nil, want error")
	}
}

func TestObservationRunnerRecordErrorIsFatal(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "file.txt")
	writeTestFile(t, localPath, "hello")

	translator := mustTranslator(t, projectRoot)
	recordErr := fmt.Errorf("database failed")
	records := fakeRecordStore{err: recordErr}
	remote := &fakeRemoteGetter{
		err: &mcapi.APIError{StatusCode: http.StatusNotFound, Status: "404 Not Found"},
	}

	runner := NewObservationRunner(123, translator, records, remote, ModeStatus)
	runner.Now = fixedNow

	_, err := runner.ObserveAndReconcile(ctx, localPath)
	if !errors.Is(err, recordErr) {
		t.Fatalf("ObserveAndReconcile() error = %v, want recordErr", err)
	}
}

func TestRemoteEntryFromMCFileDirectory(t *testing.T) {
	entry, err := RemoteEntryFromMCFile(&mcmodel.File{
		ID:        10,
		Path:      "/Dir1",
		Name:      "Dir1",
		Size:      0,
		MimeType:  "directory",
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	})
	if err != nil {
		t.Fatalf("RemoteEntryFromMCFile() error = %v", err)
	}

	if entry == nil {
		t.Fatal("RemoteEntryFromMCFile() = nil, want entry")
	}
	if entry.Kind != KindDir {
		t.Fatalf("Kind = %q, want %q", entry.Kind, KindDir)
	}
	if entry.Dir != "/" {
		t.Fatalf("Dir = %q, want /", entry.Dir)
	}
	if entry.RemoteFileID == nil || *entry.RemoteFileID != 10 {
		t.Fatalf("RemoteFileID = %d, want 10", entry.RemoteFileID)
	}
}

func TestRemoteEntryFromMCFileNil(t *testing.T) {
	got, err := RemoteEntryFromMCFile(nil)
	if err != nil {
		t.Fatalf("RemoteEntryFromMCFile(nil) error = %v", err)
	}
	if got != nil {
		t.Fatalf("RemoteEntryFromMCFile(nil) = %#v, want nil", got)
	}
}

func TestObservationRunnerEmptyLocalPathReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()

	runner := NewObservationRunner(
		123,
		mustTranslator(t, projectRoot),
		fakeRecordStore{},
		&fakeRemoteGetter{},
		ModeStatus,
	)

	_, err := runner.ObserveAndReconcile(ctx, "")
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("ObserveAndReconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestObservationRunnerInvalidProjectIDReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "file.txt")
	writeTestFile(t, localPath, "hello")

	runner := NewObservationRunner(
		0,
		mustTranslator(t, projectRoot),
		fakeRecordStore{},
		&fakeRemoteGetter{},
		ModeStatus,
	)

	_, err := runner.ObserveAndReconcile(ctx, localPath)
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("ObserveAndReconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestObservationRunnerUnconfiguredTranslatorReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()

	runner := NewObservationRunner(
		123,
		projectpath.Translator{},
		fakeRecordStore{},
		&fakeRemoteGetter{},
		ModeStatus,
	)

	_, err := runner.ObserveAndReconcile(ctx, "/tmp/file.txt")
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("ObserveAndReconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestObservationRunnerRemoteReturnedDifferentPathReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	localPath := filepath.Join(projectRoot, "Dir1", "file.txt")
	writeTestFile(t, localPath, "hello")

	runner := NewObservationRunner(
		123,
		mustTranslator(t, projectRoot),
		fakeRecordStore{},
		&fakeRemoteGetter{
			file: &mcmodel.File{
				ID:        77,
				Path:      "/Other/file.txt",
				Name:      "file.txt",
				Size:      5,
				MimeType:  "text/plain",
				Checksum:  "remote-md5",
				CreatedAt: time.Unix(10, 0),
				UpdatedAt: time.Unix(20, 0),
			},
		},
		ModeStatus,
	)
	runner.Now = fixedNow

	_, err := runner.ObserveAndReconcile(ctx, localPath)
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("ObserveAndReconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestRemoteEntryFromMCFileEmptyPathReturnsInvalidObservation(t *testing.T) {
	_, err := RemoteEntryFromMCFile(&mcmodel.File{
		ID:   10,
		Name: "file.txt",
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("RemoteEntryFromMCFile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestRemoteEntryFromMCFileRelativePathReturnsInvalidObservation(t *testing.T) {
	_, err := RemoteEntryFromMCFile(&mcmodel.File{
		ID:   10,
		Path: "Dir1/file.txt",
		Name: "file.txt",
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("RemoteEntryFromMCFile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestRemoteEntryFromMCFileDerivesMissingNameFromPath(t *testing.T) {
	entry, err := RemoteEntryFromMCFile(&mcmodel.File{
		ID:        10,
		Path:      "/Dir1/file.txt",
		Name:      "",
		Size:      5,
		MimeType:  "text/plain",
		Checksum:  "remote-md5",
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	})
	if err != nil {
		t.Fatalf("RemoteEntryFromMCFile() error = %v", err)
	}

	if entry.Name != "file.txt" {
		t.Fatalf("Name = %q, want file.txt", entry.Name)
	}
	if entry.Dir != "/Dir1" {
		t.Fatalf("Dir = %q, want /Dir1", entry.Dir)
	}
}

func TestRemoteEntryFromMCFileZeroIDKeepsRemoteFileIDNil(t *testing.T) {
	entry, err := RemoteEntryFromMCFile(&mcmodel.File{
		ID:        0,
		Path:      "/Dir1/file.txt",
		Name:      "file.txt",
		Size:      5,
		MimeType:  "text/plain",
		Checksum:  "remote-md5",
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	})
	if err != nil {
		t.Fatalf("RemoteEntryFromMCFile() error = %v", err)
	}

	if entry.RemoteFileID != nil {
		t.Fatalf("RemoteFileID = %v, want nil for zero id", entry.RemoteFileID)
	}
}

type fakeRecordStore struct {
	records map[string]filedb.FileRecord
	err     error
}

func (s fakeRecordStore) GetByPath(ctx context.Context, filePath string) (filedb.FileRecord, error) {
	if s.err != nil {
		return filedb.FileRecord{}, s.err
	}
	record, ok := s.records[filePath]
	if !ok {
		return filedb.FileRecord{}, filedb.ErrRecordNotFound
	}
	return record, nil
}

type fakeRemoteGetter struct {
	file *mcmodel.File
	err  error

	gotProjectID  int
	gotRemotePath string
}

func (g *fakeRemoteGetter) GetFileByPath(projectID int, remotePath string) (*mcmodel.File, error) {
	g.gotProjectID = projectID
	g.gotRemotePath = remotePath
	if g.err != nil {
		return nil, g.err
	}
	return g.file, nil
}

func mustTranslator(t *testing.T, projectRoot string) projectpath.Translator {
	t.Helper()

	translator, err := projectpath.New(projectRoot)
	if err != nil {
		t.Fatalf("projectpath.New() error = %v", err)
	}

	return translator
}

func writeTestFile(t *testing.T, filePath string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func fixedNow() time.Time {
	return time.Unix(100, 0)
}
