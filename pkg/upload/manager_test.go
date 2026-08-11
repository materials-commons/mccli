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

	got, ok := manager.Result(transferID)
	if !ok {
		t.Fatal("Result() ok = false, want true")
	}
	if !got {
		t.Fatal("Result() = false, want true")
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

	got, ok := manager.Result(transferID)
	if !ok {
		t.Fatal("Result() ok = false, want true")
	}
	if got {
		t.Fatal("Result() = true, want false")
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

type fakeUploader struct {
	mu         sync.Mutex
	transferID string
	result     bool
	onUpload   func()
	messages   []wsclient.TextMessage
}

func (f *fakeUploader) Upload(ctx context.Context) error {
	if f.onUpload != nil {
		f.onUpload()
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
