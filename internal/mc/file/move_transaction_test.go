package file

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMoveDBSaveTransactionAssignsIDBeforeExistenceCheckRegression(t *testing.T) {
	db := newTestMoveDB(t)

	poisonPath := filepath.Join(db.path, ".json")
	if err := os.WriteFile(poisonPath, []byte(`{"id":"poison"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(.json) error = %v", err)
	}

	txn := &MoveTransaction{
		CommandWorkingDir: t.TempDir(),
		CommandArgs:       []string{"mv", "src.txt", "dest.txt"},
		Source:            "src.txt",
		Dest:              "dest.txt",
		SourceFullPath:    "/tmp/src.txt",
		DestFullPath:      "/tmp/dest.txt",
		CreatedAt:         time.Unix(10, 0),
		UpdatedAt:         time.Unix(10, 0),
	}

	saved, err := db.SaveTransaction(txn)
	if err != nil {
		t.Fatalf("SaveTransaction() error = %v, want nil even when .json exists", err)
	}
	if saved.ID == "" {
		t.Fatal("SaveTransaction() left ID empty")
	}
	if saved.ID == "poison" {
		t.Fatal("SaveTransaction() reused unrelated .json contents as transaction ID")
	}

	wantPath := filepath.Join(db.path, saved.ID+".json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("saved transaction file stat error = %v, want file at %s", err, wantPath)
	}
}

func TestMoveDBGetTransactionsFindsUUIDJSONFilesRegression(t *testing.T) {
	db := newTestMoveDB(t)

	first := mustSaveMoveTxn(t, db, &MoveTransaction{
		ID:                "11111111-1111-1111-1111-111111111111",
		CommandWorkingDir: "/work",
		CommandArgs:       []string{"mv", "a", "b"},
		Source:            "a",
		Dest:              "b",
		CreatedAt:         time.Unix(1, 0),
		UpdatedAt:         time.Unix(1, 0),
	})
	second := mustSaveMoveTxn(t, db, &MoveTransaction{
		ID:                "22222222-2222-2222-2222-222222222222",
		CommandWorkingDir: "/work",
		CommandArgs:       []string{"mv", "c", "d"},
		Source:            "c",
		Dest:              "d",
		CreatedAt:         time.Unix(2, 0),
		UpdatedAt:         time.Unix(2, 0),
	})

	if err := os.WriteFile(filepath.Join(db.path, "not-json.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("WriteFile(not-json.txt) error = %v", err)
	}

	txns, err := db.GetTransactions()
	if err != nil {
		t.Fatalf("GetTransactions() error = %v", err)
	}

	gotIDs := map[string]bool{}
	for _, txn := range txns {
		gotIDs[txn.ID] = true
	}

	if len(txns) != 2 {
		t.Fatalf("GetTransactions() returned %d transactions, want 2: %#v", len(txns), txns)
	}
	if !gotIDs[first.ID] || !gotIDs[second.ID] {
		t.Fatalf("GetTransactions() IDs = %#v, want both saved transactions", gotIDs)
	}
}

func TestMoveDBGetTransactionsReturnsCorruptJSONErrorRegression(t *testing.T) {
	db := newTestMoveDB(t)

	mustSaveMoveTxn(t, db, &MoveTransaction{
		ID:     "11111111-1111-1111-1111-111111111111",
		Source: "good",
		Dest:   "dest",
	})

	corruptPath := filepath.Join(db.path, "22222222-2222-2222-2222-222222222222.json")
	if err := os.WriteFile(corruptPath, []byte(`{"id":`), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt json) error = %v", err)
	}

	txns, err := db.GetTransactions()
	if err != nil {
		t.Fatalf("GetTransactions() error = %v", err)
	}
	// Should skip a corrupt JSON file. We don't know what the error is as the user could have saved other JSON
	// files in the db directory.
	if len(txns) != 1 {
		t.Fatalf("GetTransactions() error = nil, txns = %#v; want corrupt JSON error", txns)
	}
}

func TestMoveDBClearAllTransactionsDeletesOnlyTransactionJSONFiles(t *testing.T) {
	db := newTestMoveDB(t)

	mustSaveMoveTxn(t, db, &MoveTransaction{
		ID:     "11111111-1111-1111-1111-111111111111",
		Source: "a",
		Dest:   "b",
	})
	mustSaveMoveTxn(t, db, &MoveTransaction{
		ID:     "22222222-2222-2222-2222-222222222222",
		Source: "c",
		Dest:   "d",
	})

	keepPath := filepath.Join(db.path, "keep.txt")
	if err := os.WriteFile(keepPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.txt) error = %v", err)
	}

	if err := db.ClearAllTransactions(); err != nil {
		t.Fatalf("ClearAllTransactions() error = %v", err)
	}

	txns, err := db.GetTransactions()
	if err != nil {
		t.Fatalf("GetTransactions() after clear error = %v", err)
	}
	if len(txns) != 0 {
		t.Fatalf("GetTransactions() after clear returned %d transactions, want 0", len(txns))
	}

	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("non-transaction file stat error = %v, want preserved", err)
	}
}

func TestMoveDBRejectsDuplicateTransactionID(t *testing.T) {
	db := newTestMoveDB(t)

	txn := &MoveTransaction{
		ID:     "11111111-1111-1111-1111-111111111111",
		Source: "src",
		Dest:   "dest",
	}
	mustSaveMoveTxn(t, db, txn)

	_, err := db.SaveTransaction(&MoveTransaction{
		ID:     txn.ID,
		Source: "other-src",
		Dest:   "other-dest",
	})
	if err == nil {
		t.Fatal("SaveTransaction() duplicate error = nil, want error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("SaveTransaction() duplicate error = %v, want already exists", err)
	}
}

func TestMoveDBUpdateTransactionPersistsChanges(t *testing.T) {
	db := newTestMoveDB(t)

	saved := mustSaveMoveTxn(t, db, &MoveTransaction{
		ID:        "11111111-1111-1111-1111-111111111111",
		Source:    "src",
		Dest:      "dest",
		LastError: "old",
	})

	saved.LastError = "new error"
	saved.UpdatedAt = time.Unix(50, 0)

	if err := db.UpdateTransaction(saved); err != nil {
		t.Fatalf("UpdateTransaction() error = %v", err)
	}

	got, err := db.GetTransaction(saved.ID)
	if err != nil {
		t.Fatalf("GetTransaction() error = %v", err)
	}
	if got.LastError != "new error" {
		t.Fatalf("GetTransaction().LastError = %q, want %q", got.LastError, "new error")
	}
	if !got.UpdatedAt.Equal(time.Unix(50, 0)) {
		t.Fatalf("GetTransaction().UpdatedAt = %v, want %v", got.UpdatedAt, time.Unix(50, 0))
	}
}

func TestMoveDBDeleteTransactionRemovesFile(t *testing.T) {
	db := newTestMoveDB(t)

	saved := mustSaveMoveTxn(t, db, &MoveTransaction{
		ID:     "11111111-1111-1111-1111-111111111111",
		Source: "src",
		Dest:   "dest",
	})

	if err := db.DeleteTransaction(saved.ID); err != nil {
		t.Fatalf("DeleteTransaction() error = %v", err)
	}

	_, err := db.GetTransaction(saved.ID)
	if err == nil {
		t.Fatal("GetTransaction() after delete error = nil, want missing file error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GetTransaction() after delete error = %v, want os.ErrNotExist wrapper", err)
	}
}

func TestMoveDBConcurrentSaveTransactionAssignsUniqueIDsAndDoesNotLoseWrites(t *testing.T) {
	db := newTestMoveDB(t)

	const workers = 64

	start := make(chan struct{})
	errCh := make(chan error, workers)
	idCh := make(chan string, workers)

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()

			<-start

			txn, err := db.SaveTransaction(&MoveTransaction{
				CommandWorkingDir: "/work",
				CommandArgs:       []string{"mv", fmt.Sprintf("src-%d", i), fmt.Sprintf("dest-%d", i)},
				Source:            fmt.Sprintf("src-%d", i),
				Dest:              fmt.Sprintf("dest-%d", i),
				SourceFullPath:    fmt.Sprintf("/work/src-%d", i),
				DestFullPath:      fmt.Sprintf("/work/dest-%d", i),
				CreatedAt:         time.Unix(int64(i), 0),
				UpdatedAt:         time.Unix(int64(i), 0),
			})
			if err != nil {
				errCh <- err
				return
			}
			if txn.ID == "" {
				errCh <- fmt.Errorf("empty transaction ID")
				return
			}

			idCh <- txn.ID
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)
	close(idCh)

	for err := range errCh {
		t.Fatalf("concurrent SaveTransaction() error = %v", err)
	}

	seenIDs := map[string]bool{}
	for id := range idCh {
		if seenIDs[id] {
			t.Fatalf("duplicate generated transaction ID %q", id)
		}
		seenIDs[id] = true
	}

	if len(seenIDs) != workers {
		t.Fatalf("saved IDs = %d, want %d", len(seenIDs), workers)
	}

	txns, err := db.GetTransactions()
	if err != nil {
		t.Fatalf("GetTransactions() after concurrent saves error = %v", err)
	}
	if len(txns) != workers {
		t.Fatalf("GetTransactions() after concurrent saves returned %d transactions, want %d", len(txns), workers)
	}

	for _, txn := range txns {
		if !seenIDs[txn.ID] {
			t.Fatalf("GetTransactions() returned unexpected transaction ID %q", txn.ID)
		}
	}
}

func TestMoveDBConcurrentSaveUpdateGetAndDeleteDoesNotCorruptJSON(t *testing.T) {
	db := newTestMoveDB(t)

	const workers = 32

	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)

		go func() {
			defer wg.Done()

			txn, err := db.SaveTransaction(&MoveTransaction{
				ID:             uuid.New().String(),
				Source:         fmt.Sprintf("src-%02d", i),
				Dest:           fmt.Sprintf("dest-%02d", i),
				SourceFullPath: fmt.Sprintf("/work/src-%02d", i),
				DestFullPath:   fmt.Sprintf("/work/dest-%02d", i),
				CreatedAt:      time.Unix(int64(i), 0),
				UpdatedAt:      time.Unix(int64(i), 0),
			})
			if err != nil {
				errCh <- fmt.Errorf("save %d: %w", i, err)
				return
			}

			for revision := 0; revision < 10; revision++ {
				txn.LastError = fmt.Sprintf("worker=%02d revision=%02d", i, revision)
				txn.UpdatedAt = time.Unix(int64(i*100+revision), 0)

				if err := db.UpdateTransaction(txn); err != nil {
					errCh <- fmt.Errorf("update %d revision %d: %w", i, revision, err)
					return
				}

				got, err := db.GetTransaction(txn.ID)
				if err != nil {
					errCh <- fmt.Errorf("get %d revision %d: %w", i, revision, err)
					return
				}
				if got.ID != txn.ID {
					errCh <- fmt.Errorf("get %d revision %d ID = %q, want %q", i, revision, got.ID, txn.ID)
					return
				}
			}

			if i%2 == 0 {
				if err := db.DeleteTransaction(txn.ID); err != nil {
					errCh <- fmt.Errorf("delete %d: %w", i, err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent operation error = %v", err)
	}

	files, err := os.ReadDir(db.path)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		b, err := os.ReadFile(filepath.Join(db.path, file.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", file.Name(), err)
		}

		var txn MoveTransaction
		if err := json.Unmarshal(b, &txn); err != nil {
			t.Fatalf("transaction file %s contains corrupt JSON %q: %v", file.Name(), string(b), err)
		}
		if txn.ID == "" {
			t.Fatalf("transaction file %s decoded with empty ID", file.Name())
		}
	}

	txns, err := db.GetTransactions()
	if err != nil {
		t.Fatalf("GetTransactions() after mixed concurrent operations error = %v", err)
	}

	if len(txns) != workers/2 {
		t.Fatalf("GetTransactions() after mixed concurrent operations returned %d transactions, want %d", len(txns), workers/2)
	}
	for _, txn := range txns {
		if strings.HasPrefix(txn.ID, "txn-") {
			var index int
			if _, err := fmt.Sscanf(txn.ID, "txn-%02d", &index); err != nil {
				t.Fatalf("unexpected transaction ID %q", txn.ID)
			}
			if index%2 == 0 {
				t.Fatalf("deleted even-index transaction %q is still present", txn.ID)
			}
		}
	}
}

func TestNewMoveDBRejectsInvalidPaths(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		db, err := NewMoveDB("")
		if err == nil {
			t.Fatalf("NewMoveDB(empty) = %#v, nil error; want error", db)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		db, err := NewMoveDB(missing)
		if err == nil {
			t.Fatalf("NewMoveDB(missing) = %#v, nil error; want error", db)
		}
	})

	t.Run("file path", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "file")
		if err := os.WriteFile(filePath, []byte("not a dir"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		db, err := NewMoveDB(filePath)
		if err == nil {
			t.Fatalf("NewMoveDB(file path) = %#v, nil error; want error", db)
		}
	})
}

func TestMoveDBTransactionIDPathTraversalIsRejectedRegression(t *testing.T) {
	db := newTestMoveDB(t)

	_, err := db.SaveTransaction(&MoveTransaction{
		ID:     "../escape",
		Source: "src",
		Dest:   "dest",
	})
	if err == nil {
		t.Fatal("SaveTransaction() with path traversal ID error = nil, want error")
	}
}

func newTestMoveDB(t *testing.T) *MoveDB {
	t.Helper()

	db, err := NewMoveDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewMoveDB() error = %v", err)
	}

	return db
}

func mustSaveMoveTxn(t *testing.T, db *MoveDB, txn *MoveTransaction) *MoveTransaction {
	t.Helper()

	saved, err := db.SaveTransaction(txn)
	if err != nil {
		t.Fatalf("SaveTransaction(%+v) error = %v", txn, err)
	}

	return saved
}
