package reconcile

import (
	"context"
	"errors"
	"testing"

	"github.com/materials-commons/mccli/pkg/filedb"
)

func TestReconcileLocalOnlyUpload(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload).WithChecksumFunc(fakeChecksum("abc123"))

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if decision.Action != ActionUpload {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionUpload)
	}
	if decision.UpdatedRecord.LocalChecksum == nil || *decision.UpdatedRecord.LocalChecksum != "abc123" {
		t.Fatalf("LocalChecksum = %v, want abc123", decision.UpdatedRecord.LocalChecksum)
	}
}

func TestReconcileRemoteOnlyDownloadModeDownloads(t *testing.T) {
	ctx := context.Background()
	r := New(ModeDownload)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if decision.Action != ActionDownload {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionDownload)
	}
	if decision.UpdatedRecord.RemoteFileID == nil || *decision.UpdatedRecord.RemoteFileID != 10 {
		t.Fatalf("RemoteFileID = %v, want 10", decision.UpdatedRecord.RemoteFileID)
	}
}

func TestReconcileRemoteOnlyUploadModeUpdatesDatabase(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if decision.Action != ActionDBUpdate {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionDBUpdate)
	}
}

func TestReconcileBothUploadLocalMatchesRecordSkips(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload)

	remoteID := int64(10)
	record := filedb.FileRecord{
		Path:         "/Dir1/file.txt",
		Dir:          "/Dir1",
		Name:         "file.txt",
		LocalSize:    100,
		LocalMTimeNS: 200,
		LocalCTimeNS: 300,
		RemoteFileID: &remoteID,
	}

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		FileRecord:  &record,
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if decision.Action != ActionSkip {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionSkip)
	}
}

func TestReconcileBothUploadLocalChangedUploads(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload).WithChecksumFunc(fakeChecksum("new-md5"))

	remoteID := int64(10)
	record := filedb.FileRecord{
		Path:         "/Dir1/file.txt",
		Dir:          "/Dir1",
		Name:         "file.txt",
		LocalSize:    1,
		LocalMTimeNS: 2,
		LocalCTimeNS: 3,
		RemoteFileID: &remoteID,
	}

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		FileRecord:  &record,
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if decision.Action != ActionUpload {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionUpload)
	}
	if decision.UpdatedRecord.LocalChecksum == nil || *decision.UpdatedRecord.LocalChecksum != "new-md5" {
		t.Fatalf("LocalChecksum = %v, want new-md5", decision.UpdatedRecord.LocalChecksum)
	}
}

func TestReconcileKindConflict(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: &LocalEntry{
			Path:       "/tmp/project/Dir1/file.txt",
			RemotePath: "/Dir1/file.txt",
			Name:       "file.txt",
			Dir:        "/Dir1",
			Kind:       KindDir,
		},
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if decision.Action != ActionConflict {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionConflict)
	}
}

func TestReconcileSymlinkConflict(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload)

	entry := localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt")
	entry.IsSymlink = true

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: entry,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if decision.Action != ActionConflict {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionConflict)
	}
}

func TestReconcileSyncUnsupported(t *testing.T) {
	ctx := context.Background()
	r := New(ModeSync)

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, ErrSyncUnsupported) {
		t.Fatalf("Reconcile() error = %v, want ErrSyncUnsupported", err)
	}
}

func fakeChecksum(value string) ChecksumFunc {
	return func(ctx context.Context, localPath string) (string, error) {
		return value, nil
	}
}

func localEntry(localPath string, remotePath string) *LocalEntry {
	return &LocalEntry{
		Path:       localPath,
		RemotePath: remotePath,
		Name:       "file.txt",
		Dir:        "/Dir1",
		Kind:       KindFile,
		Size:       100,
		MTimeNS:    200,
		CTimeNS:    300,
		LastSeenTS: 400,
	}
}

func remoteEntry(remotePath string, id int64, checksum string) *RemoteEntry {
	return &RemoteEntry{
		Path:         remotePath,
		Name:         "file.txt",
		Dir:          "/Dir1",
		Kind:         KindFile,
		RemoteFileID: id,
		Size:         100,
		CTimeNS:      500,
		MTimeNS:      600,
		Checksum:     checksum,
	}
}
