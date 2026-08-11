package upload

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
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
}

// UploaderFactory creates uploaders. It is injectable for tests.
type UploaderFactory func(req Request) uploaderRunner

// Manager manages queued concurrent uploads.
type Manager struct {
	sendQueue *wsclient.Queue[wsclient.OutboundMessage]
	dbQueue   *wsclient.Queue[DBWriteRequest]

	clientID      string
	maxConcurrent int

	uploadQueue *wsclient.Queue[uploaderRunner]

	mu            sync.Mutex
	activeUploads map[string]uploaderRunner
	results       map[string]bool

	factory UploaderFactory
}

// Config configures a Manager.
type Config struct {
	SendQueue     *wsclient.Queue[wsclient.OutboundMessage]
	DBWriteQueue  *wsclient.Queue[DBWriteRequest]
	ClientID      string
	MaxConcurrent int
	Factory       UploaderFactory
}

// NewManager creates an upload manager.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.SendQueue == nil {
		return nil, fmt.Errorf("send queue is required")
	}
	if cfg.DBWriteQueue == nil {
		return nil, fmt.Errorf("db write queue is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("client id is required")
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}

	m := &Manager{
		sendQueue:     cfg.SendQueue,
		dbQueue:       cfg.DBWriteQueue,
		clientID:      cfg.ClientID,
		maxConcurrent: cfg.MaxConcurrent,
		uploadQueue:   wsclient.NewQueue[uploaderRunner](),
		activeUploads: map[string]uploaderRunner{},
		results:       map[string]bool{},
		factory:       cfg.Factory,
	}

	if m.factory == nil {
		m.factory = func(req Request) uploaderRunner {
			return NewUploader(UploaderConfig{
				SendQueue:    m.sendQueue,
				DBWriteQueue: m.dbQueue,
				Request:      req,
				ClientID:     m.clientID,
			})
		}
	}

	return m, nil
}

// StartWorkers starts background upload workers.
func (m *Manager) StartWorkers(ctx context.Context) {
	for i := 0; i < m.maxConcurrent; i++ {
		go m.worker(ctx, i)
	}
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
	}
}

// Result returns whether a transfer completed successfully.
func (m *Manager) Result(transferID string) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, ok := m.results[transferID]
	return result, ok
}

func (m *Manager) worker(ctx context.Context, workerID int) {
	for {
		uploader, ok, err := m.uploadQueue.Pop(ctx)
		if err != nil || !ok {
			return
		}

		transferID := uploader.TransferIDValue()

		m.mu.Lock()
		m.activeUploads[transferID] = uploader
		m.mu.Unlock()

		uploadErr := uploader.Upload(ctx)

		m.mu.Lock()
		delete(m.activeUploads, transferID)
		m.results[transferID] = uploadErr == nil
		m.mu.Unlock()
	}
}
