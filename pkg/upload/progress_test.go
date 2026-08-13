package upload

import (
	"fmt"
	"sync"
	"testing"
)

type fakeProgressFactory struct {
	mu    sync.Mutex
	bars  map[string]*fakeProgressBar
	waitN int
}

func newFakeProgressFactory() *fakeProgressFactory {
	return &fakeProgressFactory{
		bars: map[string]*fakeProgressBar{},
	}
}

func (f *fakeProgressFactory) AddUploadBar(total int64, name string) ProgressBar {
	f.mu.Lock()
	defer f.mu.Unlock()

	bar := &fakeProgressBar{
		name:  name,
		total: total,
	}
	f.bars[name] = bar

	return bar
}

func (f *fakeProgressFactory) Wait() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.waitN++
}

type fakeProgressBar struct {
	mu        sync.Mutex
	name      string
	current   int64
	total     int64
	completed bool
	aborted   bool
	updates   []int64
}

func (b *fakeProgressBar) SetCurrent(current int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.current = current
	b.updates = append(b.updates, current)
}

func (b *fakeProgressBar) SetTotal(total int64, complete bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.total = total
	b.completed = complete
}

func (b *fakeProgressBar) Abort(drop bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.aborted = true
}

func TestUploadProgressCreatesOneBarPerTransfer(t *testing.T) {
	factory := newFakeProgressFactory()
	progress := NewUploadProgress(factory)

	progress.ReportUploadProgress(ProgressEvent{
		TransferID: "transfer-1",
		RemotePath: "/a.txt",
		BytesSent:  5,
		TotalBytes: 10,
		Status:     ProgressUploading,
	})
	progress.ReportUploadProgress(ProgressEvent{
		TransferID: "transfer-1",
		RemotePath: "/a.txt",
		BytesSent:  10,
		TotalBytes: 10,
		Status:     ProgressComplete,
	})

	if len(progress.bars) != 1 {
		t.Fatalf("len(progress.bars) = %d, want 1", len(progress.bars))
	}

	state := progress.bars["transfer-1"]
	if state == nil {
		t.Fatal("transfer-1 bar was not created")
	}
	if state.current != 10 {
		t.Fatalf("current = %d, want 10", state.current)
	}
	if !state.done {
		t.Fatal("done = false, want true")
	}
}

func TestUploadProgressIsConcurrentSafe(t *testing.T) {
	factory := newFakeProgressFactory()
	progress := NewUploadProgress(factory)

	const uploads = 20
	const updates = 100

	var wg sync.WaitGroup
	for i := 0; i < uploads; i++ {
		transferID := fmt.Sprintf("transfer-%d", i)

		wg.Add(1)
		go func() {
			defer wg.Done()

			for sent := int64(0); sent <= updates; sent++ {
				progress.ReportUploadProgress(ProgressEvent{
					TransferID: transferID,
					RemotePath: "/" + transferID,
					BytesSent:  sent,
					TotalBytes: updates,
					Status:     ProgressUploading,
				})
			}

			progress.ReportUploadProgress(ProgressEvent{
				TransferID: transferID,
				RemotePath: "/" + transferID,
				BytesSent:  updates,
				TotalBytes: updates,
				Status:     ProgressComplete,
			})
		}()
	}

	wg.Wait()

	if len(progress.bars) != uploads {
		t.Fatalf("len(progress.bars) = %d, want %d", len(progress.bars), uploads)
	}

	for transferID, state := range progress.bars {
		if state.current != updates {
			t.Fatalf("%s current = %d, want %d", transferID, state.current, updates)
		}
		if !state.done {
			t.Fatalf("%s done = false, want true", transferID)
		}
	}
}

func TestUploadProgressDoesNotMoveBackward(t *testing.T) {
	factory := newFakeProgressFactory()
	progress := NewUploadProgress(factory)

	progress.ReportUploadProgress(ProgressEvent{
		TransferID: "transfer-1",
		RemotePath: "/a.txt",
		BytesSent:  8,
		TotalBytes: 10,
		Status:     ProgressUploading,
	})
	progress.ReportUploadProgress(ProgressEvent{
		TransferID: "transfer-1",
		RemotePath: "/a.txt",
		BytesSent:  4,
		TotalBytes: 10,
		Status:     ProgressUploading,
	})

	state := progress.bars["transfer-1"]
	if state == nil {
		t.Fatal("transfer-1 bar was not created")
	}
	if state.current != 8 {
		t.Fatalf("current = %d, want 8", state.current)
	}
}

func TestUploadProgressAbortsFailedUpload(t *testing.T) {
	factory := newFakeProgressFactory()
	progress := NewUploadProgress(factory)

	progress.ReportUploadProgress(ProgressEvent{
		TransferID: "transfer-1",
		RemotePath: "/a.txt",
		BytesSent:  4,
		TotalBytes: 10,
		Status:     ProgressFailed,
	})

	state := progress.bars["transfer-1"]
	if state == nil {
		t.Fatal("transfer-1 bar was not created")
	}
	if !state.done {
		t.Fatal("done = false, want true")
	}

	bar, ok := state.bar.(*fakeProgressBar)
	if !ok {
		t.Fatalf("bar type = %T, want *fakeProgressBar", state.bar)
	}
	if !bar.aborted {
		t.Fatal("aborted = false, want true")
	}
}
