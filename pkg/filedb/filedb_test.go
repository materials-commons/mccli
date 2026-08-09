package filedb

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
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
	record.RemoteFileID = int64Ptr(123)
	record.RemoteChecksum = stringPtr("remote-checksum")

	if err := store.Upsert(ctx, record); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := store.GetByPath(ctx, "/Dir1/file.txt")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}

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
	first.LocalChecksum = stringPtr("local-checksum-1")
	first.RemoteFileID = int64Ptr(123)
	first.RemoteChecksum = stringPtr("remote-checksum-1")
	first.Status = stringPtr("uploaded")

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

func stringPtr(value string) *string {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
