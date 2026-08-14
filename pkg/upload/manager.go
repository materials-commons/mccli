package upload

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/materials-commons/mccli/pkg/transfer"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

// uploaderRunner is the behavior Manager needs from an uploader.
//
// Keeping this as a small interface makes Manager unit-testable without running
// the file upload protocol.
type uploaderRunner interface {
	Upload(ctx context.Context) error
	HandleResponse(msg wsclient.TextMessage)
	TransferIDValue() string
	SetTransferID(id string)
	Pause()
	Resume()
	Cancel()
}

// UploaderFactory creates uploaders. It is injectable for tests.
type UploaderFactory func(req Request) uploaderRunner

// Result describes the outcome of one transfer.
type Result struct {
	TransferID string
	Success    bool
	Err        error
}

// Manager manages queued concurrent uploads.
type Manager struct {
	sendQueue *wsclient.Queue[wsclient.OutboundMessage]
	store     FileRecordStore

	clientID      string
	maxConcurrent int

	uploadQueue *wsclient.Queue[uploaderRunner]

	mu            sync.Mutex
	activeUploads map[string]uploaderRunner
	results       map[string]Result

	started bool
	cancel  context.CancelFunc
	done    chan struct{}
	wg      sync.WaitGroup

	factory UploaderFactory
}

// Config configures a Manager.
type Config struct {
	SendQueue     *wsclient.Queue[wsclient.OutboundMessage]
	Store         FileRecordStore
	ClientID      string
	MaxConcurrent int
	Factory       UploaderFactory
	Progress      transfer.Reporter
}

// NewManager creates an upload manager.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.SendQueue == nil {
		return nil, fmt.Errorf("send queue is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("file record store is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("client id is required")
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}

	m := &Manager{
		sendQueue:     cfg.SendQueue,
		store:         cfg.Store,
		clientID:      cfg.ClientID,
		maxConcurrent: cfg.MaxConcurrent,
		uploadQueue:   wsclient.NewQueue[uploaderRunner](),
		activeUploads: map[string]uploaderRunner{},
		results:       map[string]Result{},
		factory:       cfg.Factory,
	}

	if m.factory == nil {
		m.factory = func(req Request) uploaderRunner {
			return NewUploader(UploaderConfig{
				SendQueue: m.sendQueue,
				Store:     m.store,
				Request:   req,
				ClientID:  m.clientID,
				Progress:  cfg.Progress,
			})
		}
	}

	return m, nil
}

// StartWorkers starts background upload workers.
//
// It is safe to call StartWorkers more than once; only the first call starts
// workers.
func (m *Manager) StartWorkers(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return
	}

	workerCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})
	m.started = true

	for i := 0; i < m.maxConcurrent; i++ {
		m.wg.Add(1)
		go m.worker(workerCtx, i)
	}

	go func() {
		m.wg.Wait()
		close(m.done)

		m.mu.Lock()
		m.started = false
		m.cancel = nil
		m.mu.Unlock()
	}()
}

// StopWorkers stops workers and waits for them to exit.
func (m *Manager) StopWorkers() {
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if done != nil {
		<-done
	}
}

// Running reports whether workers are running.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.started
}

// QueueUpload enqueues one upload and returns its transfer ID.
//
// This method should remain fast and should not wait for websocket I/O. That is
// the key behavior needed by mc2 up while walking/reconciling/checksumming.
func (m *Manager) QueueUpload(req Request) (string, error) {
	if req.ClientID == "" {
		req.ClientID = m.clientID
	}

	uploader := m.factory(req)
	if uploader == nil {
		return "", fmt.Errorf("uploader factory returned nil")
	}

	if uploader.TransferIDValue() == "" {
		uploader.SetTransferID(uuid.NewString())
	}

	if ok := m.uploadQueue.Push(uploader); !ok {
		return "", fmt.Errorf("upload queue is closed")
	}

	return uploader.TransferIDValue(), nil
}

// HandleMessage routes an incoming websocket message to its active uploader.
func (m *Manager) HandleMessage(msg wsclient.TextMessage) {
	command, _ := msg["command"].(string)
	if !isUploadResponseCommand(command) {
		return
	}

	payload, _ := msg["payload"].(map[string]any)
	if payload == nil {
		return
	}

	transferID, _ := payload["transfer_id"].(string)
	if transferID == "" {
		return
	}

	m.mu.Lock()
	uploader := m.activeUploads[transferID]
	m.mu.Unlock()

	if uploader != nil {
		uploader.HandleResponse(msg)
	} else {
		//fmt.Println("No active uploader found for transfer ID:", transferID)
	}
}

// Result returns the result for a transfer.
func (m *Manager) Result(transferID string) (Result, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, ok := m.results[transferID]
	return result, ok
}

// Success returns whether a transfer completed successfully.
//
// This is a convenience wrapper for older call sites/tests that only care about
// success.
func (m *Manager) Success(transferID string) (bool, bool) {
	result, ok := m.Result(transferID)
	if !ok {
		return false, false
	}
	return result.Success, true
}

// ActiveCount returns the number of currently active uploads.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.activeUploads)
}

// PauseUpload pauses an active upload.
func (m *Manager) PauseUpload(transferID string) bool {
	m.mu.Lock()
	uploader := m.activeUploads[transferID]
	m.mu.Unlock()

	if uploader == nil {
		return false
	}

	uploader.Pause()
	return true
}

// ResumeUpload resumes an active upload.
func (m *Manager) ResumeUpload(transferID string) bool {
	m.mu.Lock()
	uploader := m.activeUploads[transferID]
	m.mu.Unlock()

	if uploader == nil {
		return false
	}

	uploader.Resume()
	return true
}

// CancelUpload cancels an active upload.
func (m *Manager) CancelUpload(transferID string) bool {
	m.mu.Lock()
	uploader := m.activeUploads[transferID]
	m.mu.Unlock()

	if uploader == nil {
		return false
	}

	uploader.Cancel()
	return true
}

func (m *Manager) worker(ctx context.Context, workerID int) {
	defer m.wg.Done()

	for {
		uploader, ok, err := m.uploadQueue.Pop(ctx)
		if err != nil || !ok {
			return
		}

		m.runUploader(ctx, uploader)
	}
}

func (m *Manager) runUploader(ctx context.Context, uploader uploaderRunner) {
	transferID := uploader.TransferIDValue()

	m.mu.Lock()
	m.activeUploads[transferID] = uploader
	m.mu.Unlock()

	var uploadErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				uploadErr = fmt.Errorf("upload panic: %v", r)
			}
		}()

		uploadErr = uploader.Upload(ctx)
	}()

	m.mu.Lock()
	delete(m.activeUploads, transferID)
	m.results[transferID] = Result{
		TransferID: transferID,
		Success:    uploadErr == nil,
		Err:        uploadErr,
	}
	m.mu.Unlock()
}

func isUploadResponseCommand(command string) bool {
	switch command {
	case "TRANSFER_ACCEPT",
		"TRANSFER_REJECT",
		"CHUNK_ACK",
		"CHUNK_ERROR",
		"TRANSFER_FINALIZE",
		"UPLOAD_FAILED",
		"TRANSFER_ALREADY_UPLOADED",
		"TRANSFER_RESUME_RESPONSE":
		return true
	default:
		return false
	}
}
