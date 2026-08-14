package download

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/materials-commons/mccli/pkg/filedb"
)

func TestManagerQueueDownloadAssignsTransferIDAndRuns(t *testing.T) {
	ctx := context.Background()

	var runner *fakeDownloaderRunner
	m, err := NewManager(Config{
		Store:         &fakeStore{},
		ClientID:      "client-1",
		MaxConcurrent: 1,
		Factory: func(req Request) downloaderRunner {
			runner = &fakeDownloaderRunner{
				localPath: filepath.Join(t.TempDir(), "file.txt"),
				total:     100,
			}
			return runner
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	m.StartWorkers(ctx)
	defer m.StopWorkers()

	transferID, err := m.QueueDownload(Request{})
	if err != nil {
		t.Fatalf("QueueDownload() error = %v", err)
	}
	if transferID == "" {
		t.Fatal("transferID is empty")
	}
	if runner.TransferIDValue() != transferID {
		t.Fatalf("runner transfer ID = %q, want %q", runner.TransferIDValue(), transferID)
	}

	waitForResult(t, m, transferID)

	result, ok := m.Result(transferID)
	if !ok {
		t.Fatalf("Result(%q) not found", transferID)
	}
	if !result.Success {
		t.Fatalf("Success = false, err=%v", result.Err)
	}
}

func TestManagerRecordsDownloadFailure(t *testing.T) {
	ctx := context.Background()
	downloadErr := fmt.Errorf("download failed")

	m, err := NewManager(Config{
		Store:         &fakeStore{},
		ClientID:      "client-1",
		MaxConcurrent: 1,
		Factory: func(req Request) downloaderRunner {
			return &fakeDownloaderRunner{err: downloadErr}
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	m.StartWorkers(ctx)
	defer m.StopWorkers()

	transferID, err := m.QueueDownload(Request{})
	if err != nil {
		t.Fatalf("QueueDownload() error = %v", err)
	}

	waitForResult(t, m, transferID)

	result, ok := m.Result(transferID)
	if !ok {
		t.Fatalf("Result(%q) not found", transferID)
	}
	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if result.Err != downloadErr {
		t.Fatalf("Err = %v, want %v", result.Err, downloadErr)
	}
}

func waitForResult(t *testing.T, m *Manager, transferID string) {
	t.Helper()

	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, ok := m.Result(transferID); ok {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("timeout waiting for result %q", transferID)
		case <-ticker.C:
		}
	}
}

type fakeDownloaderRunner struct {
	mu         sync.Mutex
	transferID string
	localPath  string
	done       int64
	total      int64
	err        error
	paused     bool
	cancelled  bool
}

func (r *fakeDownloaderRunner) Download(ctx context.Context) error {
	r.mu.Lock()
	r.done = r.total
	r.mu.Unlock()
	return r.err
}

func (r *fakeDownloaderRunner) TransferIDValue() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transferID
}

func (r *fakeDownloaderRunner) SetTransferID(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transferID = id
}

func (r *fakeDownloaderRunner) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = true
}

func (r *fakeDownloaderRunner) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = false
}

func (r *fakeDownloaderRunner) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelled = true
}

func (r *fakeDownloaderRunner) BytesReceived() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

func (r *fakeDownloaderRunner) ExpectedSize() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total
}

func (r *fakeDownloaderRunner) LocalPath() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.localPath, nil
}

var _ FileRecordStore = (*fakeStore)(nil)

func assertFileRecordStore(_ filedb.FileRecord) {}
