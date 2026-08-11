package upload

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/materials-commons/mccli/pkg/wsclient"
)

func TestNewManagerValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing send queue",
			cfg: Config{
				DBWriteQueue: wsclient.NewQueue[DBWriteRequest](),
				ClientID:     "client-1",
			},
		},
		{
			name: "missing db queue",
			cfg: Config{
				SendQueue: wsclient.NewQueue[wsclient.OutboundMessage](),
				ClientID:  "client-1",
			},
		},
		{
			name: "missing client id",
			cfg: Config{
				SendQueue:    wsclient.NewQueue[wsclient.OutboundMessage](),
				DBWriteQueue: wsclient.NewQueue[DBWriteRequest](),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewManager(tt.cfg)
			if err == nil {
				t.Fatal("NewManager() error = nil, want error")
			}
		})
	}
}

func TestNewManagerDefaultsMaxConcurrent(t *testing.T) {
	manager := newTestManager(t, Config{})

	if manager.maxConcurrent != 3 {
		t.Fatalf("maxConcurrent = %d, want 3", manager.maxConcurrent)
	}
}

func TestQueueUploadUsesFactoryAndReturnsTransferID(t *testing.T) {
	var gotReq Request

	manager := newTestManager(t, Config{
		Factory: func(req Request) uploaderRunner {
			gotReq = req
			return &fakeUploader{transferID: "transfer-1"}
		},
	})

	transferID, err := manager.QueueUpload(Request{ProjectID: 123})
	if err != nil {
		t.Fatalf("QueueUpload() error = %v", err)
	}

	if transferID != "transfer-1" {
		t.Fatalf("transferID = %q, want transfer-1", transferID)
	}
	if gotReq.ProjectID != 123 {
		t.Fatalf("factory ProjectID = %d, want 123", gotReq.ProjectID)
	}
	if manager.uploadQueue.Len() != 1 {
		t.Fatalf("uploadQueue.Len() = %d, want 1", manager.uploadQueue.Len())
	}
}

func TestQueueUploadAssignsTransferIDWhenMissing(t *testing.T) {
	manager := newTestManager(t, Config{
		Factory: func(req Request) uploaderRunner {
			return &fakeUploader{}
		},
	})

	transferID, err := manager.QueueUpload(Request{})
	if err != nil {
		t.Fatalf("QueueUpload() error = %v", err)
	}

	if transferID == "" {
		t.Fatal("transferID is empty, want generated id")
	}
}

func TestQueueUploadFailsWhenQueueClosed(t *testing.T) {
	manager := newTestManager(t, Config{
		Factory: func(req Request) uploaderRunner {
			return &fakeUploader{transferID: "transfer-1"}
		},
	})
	manager.uploadQueue.Close()

	_, err := manager.QueueUpload(Request{})
	if err == nil {
		t.Fatal("QueueUpload() error = nil, want error")
	}
}

func TestManagerWorkerRecordsSuccessfulResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	manager := newTestManager(t, Config{
		MaxConcurrent: 1,
		Factory: func(req Request) uploaderRunner {
			return &fakeUploader{
				transferID: "transfer-1",
				result:     true,
			}
		},
	})

	manager.StartWorkers(ctx)

	transferID, err := manager.QueueUpload(Request{})
	if err != nil {
		t.Fatalf("QueueUpload() error = %v", err)
	}

	waitFor(t, time.Second, func() bool {
		_, ok := manager.Result(transferID)
		return ok
	})

	result, ok := manager.Result(transferID)
	if !ok {
		t.Fatal("Result() ok = false, want true")
	}
	if !result.Success {
		t.Fatalf("Result() error = %v, wanted Success == true", result.Err)
	}
}

func TestManagerWorkerRecordsFailedResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	manager := newTestManager(t, Config{
		MaxConcurrent: 1,
		Factory: func(req Request) uploaderRunner {
			return &fakeUploader{
				transferID: "transfer-1",
				result:     false,
			}
		},
	})

	manager.StartWorkers(ctx)

	transferID, err := manager.QueueUpload(Request{})
	if err != nil {
		t.Fatalf("QueueUpload() error = %v", err)
	}

	waitFor(t, time.Second, func() bool {
		_, ok := manager.Result(transferID)
		return ok
	})

	result, ok := manager.Result(transferID)
	if !ok {
		t.Fatal("Result() ok = false, want true")
	}
	if result.Success {
		t.Fatal("result.Success = true, wanted false")
	}
}

func TestManagerHonorsMaxConcurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	started := make(chan string, 10)
	release := make(chan struct{})

	manager := newTestManager(t, Config{
		MaxConcurrent: 2,
		Factory: func(req Request) uploaderRunner {
			id := fmt.Sprintf("transfer-%d", req.ProjectID)
			return &fakeUploader{
				transferID: id,
				result:     true,
				onUpload: func() {
					started <- id
					<-release
				},
			}
		},
	})

	manager.StartWorkers(ctx)

	for i := 0; i < 5; i++ {
		if _, err := manager.QueueUpload(Request{ProjectID: i}); err != nil {
			t.Fatalf("QueueUpload(%d) error = %v", i, err)
		}
	}

	first := <-started
	second := <-started

	select {
	case third := <-started:
		t.Fatalf("third upload started before release: first=%s second=%s third=%s", first, second, third)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
}

func TestManagerHandleMessageRoutesToActiveUploader(t *testing.T) {
	manager := newTestManager(t, Config{})

	uploader := &fakeUploader{transferID: "transfer-1"}

	manager.mu.Lock()
	manager.activeUploads["transfer-1"] = uploader
	manager.mu.Unlock()

	msg := wsclient.TextMessage{
		"command": "TRANSFER_ACCEPT",
		"payload": map[string]any{
			"transfer_id": "transfer-1",
		},
	}

	manager.HandleMessage(msg)

	if len(uploader.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(uploader.messages))
	}
	if uploader.messages[0]["command"] != "TRANSFER_ACCEPT" {
		t.Fatalf("command = %v, want TRANSFER_ACCEPT", uploader.messages[0]["command"])
	}
}

func TestManagerHandleMessageIgnoresUnknownTransfer(t *testing.T) {
	manager := newTestManager(t, Config{})

	msg := wsclient.TextMessage{
		"command": "TRANSFER_ACCEPT",
		"payload": map[string]any{
			"transfer_id": "missing",
		},
	}

	manager.HandleMessage(msg)
}

func TestManagerStartWorkersIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	started := make(chan struct{}, 10)
	release := make(chan struct{})

	manager := newTestManager(t, Config{
		MaxConcurrent: 1,
		Factory: func(req Request) uploaderRunner {
			return &fakeUploader{
				transferID: fmt.Sprintf("transfer-%d", req.ProjectID),
				result:     true,
				onUpload: func() {
					started <- struct{}{}
					<-release
				},
			}
		},
	})

	manager.StartWorkers(ctx)
	manager.StartWorkers(ctx)
	manager.StartWorkers(ctx)

	for i := 0; i < 3; i++ {
		if _, err := manager.QueueUpload(Request{ProjectID: i}); err != nil {
			t.Fatalf("QueueUpload(%d) error = %v", i, err)
		}
	}

	<-started

	select {
	case <-started:
		t.Fatal("second upload started; StartWorkers likely started duplicate workers")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	manager.StopWorkers()
}

func TestManagerStopWorkersStopsWorkers(t *testing.T) {
	ctx := context.Background()

	manager := newTestManager(t, Config{
		MaxConcurrent: 1,
		Factory: func(req Request) uploaderRunner {
			return &fakeUploader{
				transferID: "transfer-1",
				result:     true,
			}
		},
	})

	manager.StartWorkers(ctx)
	if !manager.Running() {
		t.Fatal("Running() = false, want true")
	}

	manager.StopWorkers()

	if manager.Running() {
		t.Fatal("Running() = true after StopWorkers, want false")
	}
}

func TestManagerWorkerStoresUploadError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	uploadErr := fmt.Errorf("network exploded")

	manager := newTestManager(t, Config{
		MaxConcurrent: 1,
		Factory: func(req Request) uploaderRunner {
			return &fakeUploader{
				transferID: "transfer-1",
				err:        uploadErr,
			}
		},
	})

	manager.StartWorkers(ctx)

	transferID, err := manager.QueueUpload(Request{})
	if err != nil {
		t.Fatalf("QueueUpload() error = %v", err)
	}

	waitFor(t, time.Second, func() bool {
		_, ok := manager.Result(transferID)
		return ok
	})

	result, ok := manager.Result(transferID)
	if !ok {
		t.Fatal("Result() ok = false, want true")
	}
	if result.Success {
		t.Fatal("Result.Success = true, want false")
	}
	if result.Err == nil || result.Err.Error() != "network exploded" {
		t.Fatalf("Result.Err = %v, want network exploded", result.Err)
	}
}

func TestManagerWorkerRecoversPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	manager := newTestManager(t, Config{
		MaxConcurrent: 1,
		Factory: func(req Request) uploaderRunner {
			return &fakeUploader{
				transferID: "transfer-1",
				onUpload: func() {
					panic("boom")
				},
			}
		},
	})

	manager.StartWorkers(ctx)

	transferID, err := manager.QueueUpload(Request{})
	if err != nil {
		t.Fatalf("QueueUpload() error = %v", err)
	}

	waitFor(t, time.Second, func() bool {
		_, ok := manager.Result(transferID)
		return ok
	})

	result, ok := manager.Result(transferID)
	if !ok {
		t.Fatal("Result() ok = false, want true")
	}
	if result.Success {
		t.Fatal("Result.Success = true, want false")
	}
	if result.Err == nil || result.Err.Error() != "upload panic: boom" {
		t.Fatalf("Result.Err = %v, want upload panic: boom", result.Err)
	}
}

func TestManagerHandleMessageIgnoresNonUploadCommand(t *testing.T) {
	manager := newTestManager(t, Config{})

	uploader := &fakeUploader{transferID: "transfer-1"}

	manager.mu.Lock()
	manager.activeUploads["transfer-1"] = uploader
	manager.mu.Unlock()

	manager.HandleMessage(wsclient.TextMessage{
		"command": "SOME_OTHER_COMMAND",
		"payload": map[string]any{
			"transfer_id": "transfer-1",
		},
	})

	if len(uploader.messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0", len(uploader.messages))
	}
}

func TestManagerPauseResumeCancelActiveUpload(t *testing.T) {
	manager := newTestManager(t, Config{})

	uploader := &fakeUploader{transferID: "transfer-1"}

	manager.mu.Lock()
	manager.activeUploads["transfer-1"] = uploader
	manager.mu.Unlock()

	if !manager.PauseUpload("transfer-1") {
		t.Fatal("PauseUpload() = false, want true")
	}
	if !manager.ResumeUpload("transfer-1") {
		t.Fatal("ResumeUpload() = false, want true")
	}
	if !manager.CancelUpload("transfer-1") {
		t.Fatal("CancelUpload() = false, want true")
	}

	uploader.mu.Lock()
	defer uploader.mu.Unlock()

	if !uploader.paused {
		t.Fatal("paused = false, want true")
	}
	if !uploader.resumed {
		t.Fatal("resumed = false, want true")
	}
	if !uploader.cancelled {
		t.Fatal("cancelled = false, want true")
	}
}

func TestManagerPauseResumeCancelMissingUpload(t *testing.T) {
	manager := newTestManager(t, Config{})

	if manager.PauseUpload("missing") {
		t.Fatal("PauseUpload(missing) = true, want false")
	}
	if manager.ResumeUpload("missing") {
		t.Fatal("ResumeUpload(missing) = true, want false")
	}
	if manager.CancelUpload("missing") {
		t.Fatal("CancelUpload(missing) = true, want false")
	}
}

func TestManagerQueueUploadRejectsNilUploader(t *testing.T) {
	manager := newTestManager(t, Config{
		Factory: func(req Request) uploaderRunner {
			return nil
		},
	})

	_, err := manager.QueueUpload(Request{})
	if err == nil {
		t.Fatal("QueueUpload() error = nil, want error")
	}
}

func newTestManager(t *testing.T, cfg Config) *Manager {
	t.Helper()

	if cfg.SendQueue == nil {
		cfg.SendQueue = wsclient.NewQueue[wsclient.OutboundMessage]()
	}
	if cfg.DBWriteQueue == nil {
		cfg.DBWriteQueue = wsclient.NewQueue[DBWriteRequest]()
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "client-1"
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	return manager
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("condition not met before timeout")
}

type fakeUploader struct {
	mu         sync.Mutex
	transferID string
	result     bool
	err        error
	onUpload   func()
	messages   []wsclient.TextMessage
	paused     bool
	resumed    bool
	cancelled  bool
}

func (f *fakeUploader) Upload(ctx context.Context) error {
	if f.onUpload != nil {
		f.onUpload()
	}
	if f.err != nil {
		return f.err
	}
	if f.result {
		return nil
	}
	return fmt.Errorf("upload failed")
}

func (f *fakeUploader) HandleResponse(msg wsclient.TextMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.messages = append(f.messages, msg)
}

func (f *fakeUploader) TransferIDValue() string {
	return f.transferID
}

func (f *fakeUploader) SetTransferID(id string) {
	f.transferID = id
}

func (f *fakeUploader) Pause() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = true
}

func (f *fakeUploader) Resume() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumed = true
}

func (f *fakeUploader) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = true
}
