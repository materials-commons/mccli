package filedb

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/materials-commons/mccli/pkg/conv"
)

func TestOpenCreatesDatabase(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()

	store, err := Open(ctx, projectRoot)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	if store.Path() != DBPath(projectRoot) {
		t.Fatalf("Path() = %q, want %q", store.Path(), DBPath(projectRoot))
	}
}

func TestUpsertAndGetByPath(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	record := testRecord("/Dir1/file.txt")
	record.RemoteFileID = conv.Int64Ptr(123)
	record.RemoteChecksum = conv.StringPtr("remote-checksum")

	// Insert new record
	if err := store.Upsert(ctx, record); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	// Check that we can get it back again.
	got, err := store.GetByPath(ctx, "/Dir1/file.txt")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}

	// Validate the record.
	if got.Path != record.Path {
		t.Fatalf("Path = %q, want %q", got.Path, record.Path)
	}
	if got.RemoteFileID == nil || *got.RemoteFileID != 123 {
		t.Fatalf("RemoteFileID = %v, want 123", got.RemoteFileID)
	}
	if got.RemoteChecksum == nil || *got.RemoteChecksum != "remote-checksum" {
		t.Fatalf("RemoteChecksum = %v, want remote-checksum", got.RemoteChecksum)
	}
}

func TestUpsertPreservesExistingNullableValues(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	first := testRecord("/Dir1/file.txt")
	first.LocalChecksum = conv.StringPtr("local-checksum-1")
	first.RemoteFileID = conv.Int64Ptr(123)
	first.RemoteChecksum = conv.StringPtr("remote-checksum-1")
	first.Status = conv.StringPtr("uploaded")

	if err := store.Upsert(ctx, first); err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}

	second := testRecord("/Dir1/file.txt")
	second.LocalChecksum = nil
	second.RemoteFileID = nil
	second.RemoteChecksum = nil
	second.Status = nil
	second.LocalSize = 999

	if err := store.Upsert(ctx, second); err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}

	got, err := store.GetByPath(ctx, "/Dir1/file.txt")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}

	if got.LocalSize != 999 {
		t.Fatalf("LocalSize = %d, want 999", got.LocalSize)
	}
	if got.LocalChecksum == nil || *got.LocalChecksum != "local-checksum-1" {
		t.Fatalf("LocalChecksum = %v, want local-checksum-1", got.LocalChecksum)
	}
	if got.RemoteFileID == nil || *got.RemoteFileID != 123 {
		t.Fatalf("RemoteFileID = %v, want 123", got.RemoteFileID)
	}
	if got.RemoteChecksum == nil || *got.RemoteChecksum != "remote-checksum-1" {
		t.Fatalf("RemoteChecksum = %v, want remote-checksum-1", got.RemoteChecksum)
	}
	if got.Status == nil || *got.Status != "uploaded" {
		t.Fatalf("Status = %v, want uploaded", got.Status)
	}
}

func TestUpsertManyAndListByDir(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	records := []FileRecord{
		testRecord("/Dir1/a.txt"),
		testRecord("/Dir1/b.txt"),
		testRecord("/Dir2/c.txt"),
	}

	if err := store.UpsertMany(ctx, records); err != nil {
		t.Fatalf("UpsertMany() error = %v", err)
	}

	got, err := store.ListByDir(ctx, "/Dir1")
	if err != nil {
		t.Fatalf("ListByDir() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(ListByDir()) = %d, want 2", len(got))
	}
	if got[0].Name != "a.txt" {
		t.Fatalf("first name = %q, want a.txt", got[0].Name)
	}
	if got[1].Name != "b.txt" {
		t.Fatalf("second name = %q, want b.txt", got[1].Name)
	}
}

func TestGetByPathNotFound(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	_, err := store.GetByPath(ctx, "/missing.txt")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("GetByPath() error = %v, want ErrRecordNotFound", err)
	}
}

func TestDeleteByPath(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	if err := store.Upsert(ctx, testRecord("/Dir1/file.txt")); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if err := store.DeleteByPath(ctx, "/Dir1/file.txt"); err != nil {
		t.Fatalf("DeleteByPath() error = %v", err)
	}

	_, err := store.GetByPath(ctx, "/Dir1/file.txt")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("GetByPath() error = %v, want ErrRecordNotFound", err)
	}
}

func TestConcurrentUpserts(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	const workers = 10
	const perWorker = 20

	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)

	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < perWorker; i++ {
				path := filepath.ToSlash(filepath.Join("/Dir", "worker", string(rune('a'+worker)), string(rune('a'+i))+".txt"))
				record := testRecord(path)
				if err := store.Upsert(ctx, record); err != nil {
					errs <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Upsert() error = %v", err)
		}
	}
}

func TestUpsertRejectsRelativePath(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	record := testRecord("Dir1/file.txt")

	err := store.Upsert(ctx, record)
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Upsert() error = %v, want ErrInvalidRecord", err)
	}
}

func TestUpsertRejectsMismatchedDir(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	record := testRecord("/Dir1/file.txt")
	record.Dir = "/Wrong"

	err := store.Upsert(ctx, record)
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Upsert() error = %v, want ErrInvalidRecord", err)
	}
}

func TestUpsertRejectsMismatchedName(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	record := testRecord("/Dir1/file.txt")
	record.Name = "wrong.txt"

	err := store.Upsert(ctx, record)
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Upsert() error = %v, want ErrInvalidRecord", err)
	}
}

func TestUpsertRejectsEmptyLocalChecksum(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	record := testRecord("/Dir1/file.txt")
	record.LocalChecksum = conv.StringPtr("")

	err := store.Upsert(ctx, record)
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Upsert() error = %v, want ErrInvalidRecord", err)
	}
}

func TestUpsertRejectsEmptyRemoteChecksum(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	record := testRecord("/Dir1/file.txt")
	record.RemoteChecksum = conv.StringPtr("")

	err := store.Upsert(ctx, record)
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Upsert() error = %v, want ErrInvalidRecord", err)
	}
}

func TestGetByPathRejectsInvalidPath(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	_, err := store.GetByPath(ctx, "Dir1/file.txt")
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("GetByPath() error = %v, want ErrInvalidRecord", err)
	}
}

func TestListByDirRejectsInvalidDir(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	_, err := store.ListByDir(ctx, "Dir1")
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListByDir() error = %v, want ErrInvalidRecord", err)
	}
}

func TestMarkTransferRejectsMissingRecord(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	err := store.MarkTransfer(ctx, "/missing.txt", "uploading", "upload", "transfer-1")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("MarkTransfer() error = %v, want ErrRecordNotFound", err)
	}
}

func TestMarkTransferRejectsEmptyStatus(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	if err := store.Upsert(ctx, testRecord("/Dir1/file.txt")); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	err := store.MarkTransfer(ctx, "/Dir1/file.txt", "", "upload", "transfer-1")
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("MarkTransfer() error = %v, want ErrInvalidRecord", err)
	}
}

func TestMarkTransferRejectsEmptyOrigin(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	if err := store.Upsert(ctx, testRecord("/Dir1/file.txt")); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	err := store.MarkTransfer(ctx, "/Dir1/file.txt", "uploading", "", "transfer-1")
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("MarkTransfer() error = %v, want ErrInvalidRecord", err)
	}
}

func TestMarkTransferRejectsEmptyTransferID(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	if err := store.Upsert(ctx, testRecord("/Dir1/file.txt")); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	err := store.MarkTransfer(ctx, "/Dir1/file.txt", "uploading", "upload", "")
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("MarkTransfer() error = %v, want ErrInvalidRecord", err)
	}
}

func TestMarkTransferUpdatesExistingRecord(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	if err := store.Upsert(ctx, testRecord("/Dir1/file.txt")); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if err := store.MarkTransfer(ctx, "/Dir1/file.txt", "uploading", "upload", "transfer-1"); err != nil {
		t.Fatalf("MarkTransfer() error = %v", err)
	}

	got, err := store.GetByPath(ctx, "/Dir1/file.txt")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}

	if got.Status == nil || *got.Status != "uploading" {
		t.Fatalf("Status = %v, want uploading", got.Status)
	}
	if got.Origin == nil || *got.Origin != "upload" {
		t.Fatalf("Origin = %v, want upload", got.Origin)
	}
	if got.TransferID == nil || *got.TransferID != "transfer-1" {
		t.Fatalf("TransferID = %v, want transfer-1", got.TransferID)
	}
}

func TestTouchLocalSeenRejectsMissingRecord(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	err := store.TouchLocalSeen(ctx, "/missing.txt", time.Unix(123, 0))
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("TouchLocalSeen() error = %v, want ErrRecordNotFound", err)
	}
}

func TestTouchLocalSeenRejectsZeroTime(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	if err := store.Upsert(ctx, testRecord("/Dir1/file.txt")); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	err := store.TouchLocalSeen(ctx, "/Dir1/file.txt", time.Time{})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("TouchLocalSeen() error = %v, want ErrInvalidRecord", err)
	}
}

func TestTouchLocalSeenUpdatesExistingRecord(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	if err := store.Upsert(ctx, testRecord("/Dir1/file.txt")); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if err := store.TouchLocalSeen(ctx, "/Dir1/file.txt", time.Unix(123, 0)); err != nil {
		t.Fatalf("TouchLocalSeen() error = %v", err)
	}

	got, err := store.GetByPath(ctx, "/Dir1/file.txt")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}

	if got.LocalLastSeenTS != 123 {
		t.Fatalf("LocalLastSeenTS = %d, want 123", got.LocalLastSeenTS)
	}
}

func TestClearRemoteByPathClearsRemoteFields(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	record := testRecord("/Dir1/file.txt")
	record.RemoteFileID = conv.Int64Ptr(123)
	record.RemoteSize = conv.Int64Ptr(456)
	record.RemoteCTimeNS = conv.Int64Ptr(789)
	record.RemoteChecksum = conv.StringPtr("remote-md5")
	record.RemoteLastSeenTS = conv.Int64Ptr(111)
	record.Status = conv.StringPtr("uploaded")
	record.Origin = conv.StringPtr("upload")
	record.TransferID = conv.StringPtr("transfer-1")

	if err := store.Upsert(ctx, record); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if err := store.ClearRemoteByPath(ctx, "/Dir1/file.txt"); err != nil {
		t.Fatalf("ClearRemoteByPath() error = %v", err)
	}

	got, err := store.GetByPath(ctx, "/Dir1/file.txt")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}

	if got.RemoteFileID != nil {
		t.Fatalf("RemoteFileID = %v, want nil", got.RemoteFileID)
	}
	if got.RemoteSize != nil {
		t.Fatalf("RemoteSize = %v, want nil", got.RemoteSize)
	}
	if got.RemoteCTimeNS != nil {
		t.Fatalf("RemoteCTimeNS = %v, want nil", got.RemoteCTimeNS)
	}
	if got.RemoteChecksum != nil {
		t.Fatalf("RemoteChecksum = %v, want nil", got.RemoteChecksum)
	}
	if got.RemoteLastSeenTS != nil {
		t.Fatalf("RemoteLastSeenTS = %v, want nil", got.RemoteLastSeenTS)
	}
	if got.Status != nil {
		t.Fatalf("Status = %v, want nil", got.Status)
	}
	if got.Origin != nil {
		t.Fatalf("Origin = %v, want nil", got.Origin)
	}
	if got.TransferID != nil {
		t.Fatalf("TransferID = %v, want nil", got.TransferID)
	}
}

func TestClearRemoteByPathRejectsMissingRecord(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	err := store.ClearRemoteByPath(ctx, "/missing.txt")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("ClearRemoteByPath() error = %v, want ErrRecordNotFound", err)
	}
}

func TestUpsertNilRemoteFieldsPreservesExistingRemoteFields(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	first := testRecord("/Dir1/file.txt")
	first.RemoteFileID = conv.Int64Ptr(123)
	first.RemoteChecksum = conv.StringPtr("remote-md5")

	if err := store.Upsert(ctx, first); err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}

	second := testRecord("/Dir1/file.txt")
	second.RemoteFileID = nil
	second.RemoteChecksum = nil
	second.LocalSize = 999

	if err := store.Upsert(ctx, second); err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}

	got, err := store.GetByPath(ctx, "/Dir1/file.txt")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}

	if got.LocalSize != 999 {
		t.Fatalf("LocalSize = %d, want 999", got.LocalSize)
	}
	if got.RemoteFileID == nil || *got.RemoteFileID != 123 {
		t.Fatalf("RemoteFileID = %v, want 123", got.RemoteFileID)
	}
	if got.RemoteChecksum == nil || *got.RemoteChecksum != "remote-md5" {
		t.Fatalf("RemoteChecksum = %v, want remote-md5", got.RemoteChecksum)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()

	store, err := Open(ctx, projectRoot)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Close(ctx); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	if err := store.Close(ctx); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
}

func openTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	projectRoot := t.TempDir()
	store, err := Open(ctx, projectRoot)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	return store
}

func testRecord(recordPath string) FileRecord {
	dir := filepath.Dir(recordPath)
	name := filepath.Base(recordPath)

	if dir == "." {
		dir = "/"
	}

	return FileRecord{
		Path:             recordPath,
		Dir:              dir,
		Name:             name,
		IsCleanLocalCopy: false,
		LocalSize:        100,
		LocalMTimeNS:     200,
		LocalCTimeNS:     300,
		LocalLastSeenTS:  400,
	}
}
