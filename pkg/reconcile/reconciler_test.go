package reconcile

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/materials-commons/mccli/pkg/conv"
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

func TestReconcileNoEntryObservedSkips(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/missing.txt",
		Name:       "missing.txt",
		Dir:        "/",
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionSkip, false)
	if decision.Reason != "no local or remote entry observed" {
		t.Fatalf("Reason = %q, want no local or remote entry observed", decision.Reason)
	}
}

func TestReconcileUnknownEntryKindConflicts(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/unknown",
		Name:       "unknown",
		Dir:        "/",
		LocalEntry: &LocalEntry{
			Path:       "/tmp/project/unknown",
			RemotePath: "/unknown",
			Name:       "unknown",
			Dir:        "/",
			Kind:       KindUnknown,
		},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionConflict, false)
	if decision.Reason != "entry kind is unknown" {
		t.Fatalf("Reason = %q, want entry kind is unknown", decision.Reason)
	}
}

func TestReconcileLocalOnlyDirectorySkips(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1",
		Name:       "Dir1",
		Dir:        "/",
		LocalEntry: localDirEntry("/tmp/project/Dir1", "/Dir1"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionSkip, false)
	if decision.Reason != "local directory exists only locally" {
		t.Fatalf("Reason = %q, want local directory exists only locally", decision.Reason)
	}
	if decision.UpdatedRecord.LocalSize != 0 {
		t.Fatalf("LocalSize = %d, want 0 for directory", decision.UpdatedRecord.LocalSize)
	}
}

func TestReconcileRemoteOnlyDirectorySkips(t *testing.T) {
	ctx := context.Background()
	r := New(ModeDownload)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1",
		Name:        "Dir1",
		Dir:         "/",
		RemoteEntry: remoteDirEntry("/Dir1", 44),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionSkip, false)
	if decision.Reason != "remote directory exists only remotely" {
		t.Fatalf("Reason = %q, want remote directory exists only remotely", decision.Reason)
	}
}

func TestReconcileDirectoryBothNoRecordSkips(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1",
		Name:        "Dir1",
		Dir:         "/",
		LocalEntry:  localDirEntry("/tmp/project/Dir1", "/Dir1"),
		RemoteEntry: remoteDirEntry("/Dir1", 44),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionSkip, false)
	if decision.Reason != "directory exists locally and remotely" {
		t.Fatalf("Reason = %q, want directory exists locally and remotely", decision.Reason)
	}
}

func TestReconcileDirectoryBothWithStaleRecordUpdatesDatabase(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	remoteID := int64(44)
	record := filedb.FileRecord{
		Path:         "/Dir1",
		Dir:          "/",
		Name:         "Dir1",
		LocalSize:    99,
		LocalMTimeNS: 98,
		LocalCTimeNS: 97,
		RemoteFileID: &remoteID,
	}

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1",
		Name:        "Dir1",
		Dir:         "/",
		FileRecord:  &record,
		LocalEntry:  localDirEntry("/tmp/project/Dir1", "/Dir1"),
		RemoteEntry: remoteDirEntry("/Dir1", 44),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDBUpdate, true)
	if decision.Reason != "directory metadata changed" {
		t.Fatalf("Reason = %q, want directory metadata changed", decision.Reason)
	}
}

func TestReconcileLocalOnlyStatusNoRecordUploadsWithoutChecksum(t *testing.T) {
	ctx := context.Background()
	checksumCalls := 0
	r := New(ModeStatus).WithChecksumFunc(func(ctx context.Context, localPath string) (string, error) {
		checksumCalls++
		return "should-not-be-used", nil
	})

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionUpload, true)
	if checksumCalls != 0 {
		t.Fatalf("checksumCalls = %d, want 0 in status mode", checksumCalls)
	}
	if decision.UpdatedRecord.LocalChecksum != nil {
		t.Fatalf("LocalChecksum = %v, want nil in status mode", decision.UpdatedRecord.LocalChecksum)
	}
}

func TestReconcileLocalOnlyUploadExistingMatchingRecordWithChecksumDoesNotRecompute(t *testing.T) {
	ctx := context.Background()
	existingChecksum := "existing-md5"
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalChecksum = &existingChecksum

	checksumCalls := 0
	r := New(ModeUpload).WithChecksumFunc(func(ctx context.Context, localPath string) (string, error) {
		checksumCalls++
		return "new-md5", nil
	})

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		FileRecord: &record,
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionUpload, true)
	if checksumCalls != 0 {
		t.Fatalf("checksumCalls = %d, want 0 when matching record already has checksum", checksumCalls)
	}
	if decision.UpdatedRecord.LocalChecksum == nil || *decision.UpdatedRecord.LocalChecksum != existingChecksum {
		t.Fatalf("LocalChecksum = %v, want existing checksum", decision.UpdatedRecord.LocalChecksum)
	}
}

func TestReconcileLocalOnlyDownloadMatchingRecordSkipsAndClearsRemote(t *testing.T) {
	ctx := context.Background()
	remoteID := int64(88)
	remoteChecksum := "remote-md5"
	record := matchingLocalRecord("/Dir1/file.txt")
	record.RemoteFileID = &remoteID
	record.RemoteChecksum = &remoteChecksum

	r := New(ModeDownload).WithChecksumFunc(fakeChecksum("should-not-be-used"))

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		FileRecord: &record,
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionSkip, false)
	if decision.UpdatedRecord.RemoteFileID != nil {
		t.Fatalf("RemoteFileID = %v, want nil after clearRemote", decision.UpdatedRecord.RemoteFileID)
	}
	if decision.UpdatedRecord.RemoteChecksum != nil {
		t.Fatalf("RemoteChecksum = %v, want nil after clearRemote", decision.UpdatedRecord.RemoteChecksum)
	}
}

func TestReconcileLocalOnlyDownloadChangedRecordUpdatesDatabaseWithChecksum(t *testing.T) {
	ctx := context.Background()
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalSize = 1

	r := New(ModeDownload).WithChecksumFunc(fakeChecksum("changed-md5"))

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		FileRecord: &record,
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDBUpdate, true)
	if decision.UpdatedRecord.LocalChecksum == nil || *decision.UpdatedRecord.LocalChecksum != "changed-md5" {
		t.Fatalf("LocalChecksum = %v, want changed-md5", decision.UpdatedRecord.LocalChecksum)
	}
}

func TestReconcileRemoteOnlyStatusDownloads(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDownload, true)
}

func TestReconcileBothStatusNoRecordSkipsUnknown(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionSkip, false)
	if decision.Reason != "status unknown; run scan to reconcile with checksum" {
		t.Fatalf("Reason = %q, want status unknown; run scan to reconcile with checksum", decision.Reason)
	}
}

func TestReconcileBothStatusMatchingLocalDifferentRemoteDownloads(t *testing.T) {
	ctx := context.Background()
	remoteID := int64(9)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.RemoteFileID = &remoteID

	r := New(ModeStatus)

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

	assertDecision(t, decision, ActionDownload, true)
}

func TestReconcileBothStatusChangedLocalSameRemoteUploads(t *testing.T) {
	ctx := context.Background()
	remoteID := int64(10)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalSize = 1
	record.RemoteFileID = &remoteID

	r := New(ModeStatus)

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

	assertDecision(t, decision, ActionUpload, true)
}

func TestReconcileBothStatusChangedLocalDifferentRemoteSkipsUnknown(t *testing.T) {
	ctx := context.Background()
	remoteID := int64(9)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalSize = 1
	record.RemoteFileID = &remoteID

	r := New(ModeStatus)

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

	assertDecision(t, decision, ActionSkip, false)
}

func TestReconcileBothUploadNoRecordChecksumMatchesRemoteUpdatesDatabase(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload).WithChecksumFunc(fakeChecksum("same-md5"))

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "same-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDBUpdate, true)
	if decision.UpdatedRecord.LocalChecksum == nil || *decision.UpdatedRecord.LocalChecksum != "same-md5" {
		t.Fatalf("LocalChecksum = %v, want same-md5", decision.UpdatedRecord.LocalChecksum)
	}
}

func TestReconcileBothUploadNoRecordChecksumDiffersUploads(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload).WithChecksumFunc(fakeChecksum("local-md5"))

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionUpload, true)
}

func TestReconcileBothDownloadNoRecordChecksumMatchesRemoteUpdatesDatabase(t *testing.T) {
	ctx := context.Background()
	r := New(ModeDownload).WithChecksumFunc(fakeChecksum("same-md5"))

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "same-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDBUpdate, true)
}

func TestReconcileBothDownloadNoRecordChecksumDiffersConflicts(t *testing.T) {
	ctx := context.Background()
	r := New(ModeDownload).WithChecksumFunc(fakeChecksum("local-md5"))

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionConflict, false)
}

func TestReconcileBothDownloadMatchingRecordDifferentRemoteDownloads(t *testing.T) {
	ctx := context.Background()
	remoteID := int64(9)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.RemoteFileID = &remoteID

	r := New(ModeDownload)

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

	assertDecision(t, decision, ActionDownload, true)
}

func TestReconcileBothDownloadChangedLocalChecksumMatchesRecordDownloads(t *testing.T) {
	ctx := context.Background()
	localChecksum := "known-local-md5"
	remoteID := int64(9)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalSize = 1
	record.LocalChecksum = &localChecksum
	record.RemoteFileID = &remoteID

	r := New(ModeDownload).WithChecksumFunc(fakeChecksum("known-local-md5"))

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

	assertDecision(t, decision, ActionDownload, true)
}

func TestReconcileBothDownloadChangedLocalChecksumDiffersConflicts(t *testing.T) {
	ctx := context.Background()
	localChecksum := "known-local-md5"
	remoteID := int64(9)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalSize = 1
	record.LocalChecksum = &localChecksum
	record.RemoteFileID = &remoteID

	r := New(ModeDownload).WithChecksumFunc(fakeChecksum("new-local-md5"))

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

	assertDecision(t, decision, ActionConflict, false)
}

func TestReconcileInvalidModeReturnsError(t *testing.T) {
	ctx := context.Background()
	r := New(Mode("invalid"))

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidMode", err)
	}
}

func TestReconcileChecksumErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	checksumErr := fmt.Errorf("checksum failed")
	r := New(ModeUpload).WithChecksumFunc(func(ctx context.Context, localPath string) (string, error) {
		return "", checksumErr
	})

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, checksumErr) {
		t.Fatalf("Reconcile() error = %v, want checksumErr", err)
	}
}

func TestReconcileContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := New(ModeUpload)

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Reconcile() error = %v, want context.Canceled", err)
	}
}

func TestReconcileInvalidObservationWithoutRemotePathReturnsError(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	_, err := r.Reconcile(ctx, Observation{})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

// ... existing code ...

func TestReconcileRecordPathMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	record := matchingLocalRecord("/Other/file.txt")

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		FileRecord: &record,
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileLocalEntryRemotePathMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	entry := localEntry("/tmp/project/Dir1/file.txt", "/Other/file.txt")

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: entry,
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileRemoteEntryPathMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	entry := remoteEntry("/Other/file.txt", 10, "remote-md5")

	_, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		RemoteEntry: entry,
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileObservationNameMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "wrong.txt",
		Dir:        "/Dir1",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileObservationDirMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Wrong",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileLocalEntryNameMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	entry := localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt")
	entry.Name = "wrong.txt"

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: entry,
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileRemoteEntryNameMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	entry := remoteEntry("/Dir1/file.txt", 10, "remote-md5")
	entry.Name = "wrong.txt"

	_, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		RemoteEntry: entry,
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileLocalEntryDirMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	entry := localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt")
	entry.Dir = "/Wrong"

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: entry,
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileRemoteEntryDirMismatchReturnsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	entry := remoteEntry("/Dir1/file.txt", 10, "remote-md5")
	entry.Dir = "/Wrong"

	_, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		RemoteEntry: entry,
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestReconcileLocalUnknownRemoteFileConflicts(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	local := localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt")
	local.Kind = KindUnknown

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  local,
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionConflict, false)
	if decision.Reason != "local and remote entry kinds differ" {
		t.Fatalf("Reason = %q, want local and remote entry kinds differ", decision.Reason)
	}
}

func TestReconcileLocalFileRemoteUnknownConflicts(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	remote := remoteEntry("/Dir1/file.txt", 10, "remote-md5")
	remote.Kind = KindUnknown

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remote,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionConflict, false)
	if decision.Reason != "local and remote entry kinds differ" {
		t.Fatalf("Reason = %q, want local and remote entry kinds differ", decision.Reason)
	}
}

func TestReconcileBothUnknownKindsConflictsAsUnknown(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	local := localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt")
	local.Kind = KindUnknown

	remote := remoteEntry("/Dir1/file.txt", 10, "remote-md5")
	remote.Kind = KindUnknown

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  local,
		RemoteEntry: remote,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionConflict, false)
	if decision.Reason != "entry kind is unknown" {
		t.Fatalf("Reason = %q, want entry kind is unknown", decision.Reason)
	}
}

func TestReconcileRootRemoteOnlyDirectorySkips(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	remote := &RemoteEntry{
		Path:         "/",
		Name:         "/",
		Dir:          "/",
		Kind:         KindDir,
		RemoteFileID: conv.Int64Ptr(1),
	}

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/",
		Name:        "/",
		Dir:         "/",
		RemoteEntry: remote,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionSkip, false)
	if decision.UpdatedRecord.Path != "/" {
		t.Fatalf("UpdatedRecord.Path = %q, want /", decision.UpdatedRecord.Path)
	}
	if decision.UpdatedRecord.Name != "/" {
		t.Fatalf("UpdatedRecord.Name = %q, want /", decision.UpdatedRecord.Name)
	}
}

func TestReconcileRemoteEmptyChecksumDoesNotSetRecordChecksum(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload)

	remote := remoteEntry("/Dir1/file.txt", 10, "")
	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		RemoteEntry: remote,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDBUpdate, true)
	if decision.UpdatedRecord.RemoteChecksum != nil {
		t.Fatalf("RemoteChecksum = %v, want nil when remote checksum is empty", decision.UpdatedRecord.RemoteChecksum)
	}
}

func TestReconcileChecksumFunctionReturnsEmptyStringIsInvalid(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload).WithChecksumFunc(fakeChecksum(""))

	_, err := r.Reconcile(ctx, Observation{
		RemotePath: "/Dir1/file.txt",
		Name:       "file.txt",
		Dir:        "/Dir1",
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidChecksum", err)
	}
}

func TestReconcileBothUploadEmptyChecksumIsInvalid(t *testing.T) {
	ctx := context.Background()
	remoteID := int64(10)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalSize = 1
	record.RemoteFileID = &remoteID

	r := New(ModeUpload).WithChecksumFunc(fakeChecksum(""))

	_, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		FileRecord:  &record,
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidChecksum", err)
	}
}

func TestReconcileBothDownloadEmptyChecksumIsInvalid(t *testing.T) {
	ctx := context.Background()
	localChecksum := "known-local-md5"
	remoteID := int64(9)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalSize = 1
	record.LocalChecksum = &localChecksum
	record.RemoteFileID = &remoteID

	r := New(ModeDownload).WithChecksumFunc(fakeChecksum(""))

	_, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		FileRecord:  &record,
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidChecksum", err)
	}
}

func TestReconcileInvalidExistingRecordWithoutPathReturnsError(t *testing.T) {
	ctx := context.Background()
	r := New(ModeStatus)

	record := filedb.FileRecord{
		Name: "file.txt",
		Dir:  "/Dir1",
	}

	_, err := r.Reconcile(ctx, Observation{
		FileRecord: &record,
		LocalEntry: localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
	})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidObservation", err)
	}
}

func TestRemoteOnlyRecordDoesNotInventRemoteFileID(t *testing.T) {
	ctx := context.Background()
	r := New(ModeUpload)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		RemoteEntry: remoteEntryWithoutID("/Dir1/file.txt", "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDBUpdate, true)
	if decision.UpdatedRecord.RemoteFileID != nil {
		t.Fatalf("RemoteFileID = %v, want nil", decision.UpdatedRecord.RemoteFileID)
	}
}

func TestBothStatusRemoteEntryWithoutIDDoesNotMatchRecord(t *testing.T) {
	ctx := context.Background()
	remoteID := int64(10)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.RemoteFileID = &remoteID

	r := New(ModeStatus)

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		FileRecord:  &record,
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntryWithoutID("/Dir1/file.txt", "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDownload, true)
}

func TestBothDownloadNoRecordPreviousVersionUploadedDownloads(t *testing.T) {
	ctx := context.Background()
	r := New(ModeDownload).
		WithChecksumFunc(fakeChecksum("local-md5")).
		WithRemoteHistory(fakeRemoteHistory{uploaded: true})

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionDownload, true)
	if decision.Reason != "local file already uploaded, remote changed" {
		t.Fatalf("Reason = %q, want local file already uploaded, remote changed", decision.Reason)
	}
}

func TestBothDownloadNoRecordPreviousVersionNotUploadedConflicts(t *testing.T) {
	ctx := context.Background()
	r := New(ModeDownload).
		WithChecksumFunc(fakeChecksum("local-md5")).
		WithRemoteHistory(fakeRemoteHistory{uploaded: false})

	decision, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertDecision(t, decision, ActionConflict, false)
	if decision.Reason != "local file changed, remote changed, local file never uploaded" {
		t.Fatalf("Reason = %q, want local file changed, remote changed, local file never uploaded", decision.Reason)
	}
}

func TestBothDownloadChangedLocalPreviousVersionUploadedDownloads(t *testing.T) {
	ctx := context.Background()
	localChecksum := "old-md5"
	remoteID := int64(9)
	record := matchingLocalRecord("/Dir1/file.txt")
	record.LocalSize = 1
	record.LocalChecksum = &localChecksum
	record.RemoteFileID = &remoteID

	r := New(ModeDownload).
		WithChecksumFunc(fakeChecksum("new-md5")).
		WithRemoteHistory(fakeRemoteHistory{uploaded: true})

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

	assertDecision(t, decision, ActionDownload, true)
}

func TestBothDownloadPreviousVersionLookupErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	historyErr := fmt.Errorf("history failed")

	r := New(ModeDownload).
		WithChecksumFunc(fakeChecksum("local-md5")).
		WithRemoteHistory(fakeRemoteHistory{err: historyErr})

	_, err := r.Reconcile(ctx, Observation{
		RemotePath:  "/Dir1/file.txt",
		Name:        "file.txt",
		Dir:         "/Dir1",
		LocalEntry:  localEntry("/tmp/project/Dir1/file.txt", "/Dir1/file.txt"),
		RemoteEntry: remoteEntry("/Dir1/file.txt", 10, "remote-md5"),
	})
	if !errors.Is(err, historyErr) {
		t.Fatalf("Reconcile() error = %v, want historyErr", err)
	}
}

type fakeRemoteHistory struct {
	uploaded bool
	err      error
}

func (h fakeRemoteHistory) HasVersionWithChecksum(ctx context.Context, obs Observation, checksum string) (bool, error) {
	if h.err != nil {
		return false, h.err
	}
	return h.uploaded, nil
}

func assertDecision(t *testing.T, decision Decision, wantAction Action, wantUpdated bool) {
	t.Helper()

	if decision.Action != wantAction {
		t.Fatalf("Action = %q, want %q; reason=%q", decision.Action, wantAction, decision.Reason)
	}
	if decision.Updated != wantUpdated {
		t.Fatalf("Updated = %v, want %v; action=%q reason=%q", decision.Updated, wantUpdated, decision.Action, decision.Reason)
	}
}

func matchingLocalRecord(recordPath string) filedb.FileRecord {
	return filedb.FileRecord{
		Path:             recordPath,
		Dir:              "/Dir1",
		Name:             "file.txt",
		IsCleanLocalCopy: false,
		LocalSize:        100,
		LocalMTimeNS:     200,
		LocalCTimeNS:     300,
		LocalLastSeenTS:  400,
	}
}

func localDirEntry(localPath string, remotePath string) *LocalEntry {
	return &LocalEntry{
		Path:       localPath,
		RemotePath: remotePath,
		Name:       "Dir1",
		Dir:        "/",
		Kind:       KindDir,
		Size:       0,
		MTimeNS:    200,
		CTimeNS:    300,
		LastSeenTS: 400,
	}
}

func remoteDirEntry(remotePath string, id int64) *RemoteEntry {
	return &RemoteEntry{
		Path:         remotePath,
		Name:         "Dir1",
		Dir:          "/",
		Kind:         KindDir,
		RemoteFileID: &id,
		Size:         0,
		CTimeNS:      500,
		MTimeNS:      600,
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
		RemoteFileID: &id,
		Size:         100,
		CTimeNS:      500,
		MTimeNS:      600,
		Checksum:     checksum,
	}
}

func remoteEntryWithoutID(remotePath string, checksum string) *RemoteEntry {
	return &RemoteEntry{
		Path:         remotePath,
		Name:         "file.txt",
		Dir:          "/Dir1",
		Kind:         KindFile,
		RemoteFileID: nil,
		Size:         100,
		CTimeNS:      500,
		MTimeNS:      600,
		Checksum:     checksum,
	}
}
