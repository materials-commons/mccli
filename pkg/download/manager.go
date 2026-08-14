package download

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/materials-commons/mccli/pkg/transfer"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

type downloaderRunner interface {
	Download(ctx context.Context) error
	TransferIDValue() string
	SetTransferID(id string)
	Pause()
	Resume()
	Cancel()
	BytesReceived() int64
	ExpectedSize() int64
	LocalPath() (string, error)
}

// Factory creates downloaders. It is injectable for tests.
type Factory func(req Request) downloaderRunner

// Result describes the outcome of one download.
type Result struct {
	TransferID string
	Success    bool
	Err        error
}

// ActiveDownload describes one active download.
type ActiveDownload struct {
	TransferID    string
	FileName      string
	BytesReceived int64
	FileSize      int64
	ProgressPct   float64
}

// Config configures a Manager.
type Config struct {
	// SendQueue is optional. If set, individual downloads may emit completion
	// messages to it.
	SendQueue     *wsclient.Queue[wsclient.OutboundMessage]
	Store         FileRecordStore
	ClientID      string
	MaxConcurrent int
	Factory       Factory
	Progress      transfer.Reporter
}

// Manager manages queued concurrent downloads.
type Manager struct {
	sendQueue *wsclient.Queue[wsclient.OutboundMessage]
	store     FileRecordStore

	clientID      string
	maxConcurrent int

	downloadQueue *wsclient.Queue[downloaderRunner]

	mu              sync.Mutex
	activeDownloads map[string]downloaderRunner
	results         map[string]Result

	started bool
	cancel  context.CancelFunc
	done    chan struct{}
	wg      sync.WaitGroup

	factory Factory
}

// NewManager creates a download manager.
func NewManager(cfg Config) (*Manager, error) {
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
		sendQueue:       cfg.SendQueue,
		store:           cfg.Store,
		clientID:        cfg.ClientID,
		maxConcurrent:   cfg.MaxConcurrent,
		downloadQueue:   wsclient.NewQueue[downloaderRunner](),
		activeDownloads: map[string]downloaderRunner{},
		results:         map[string]Result{},
		factory:         cfg.Factory,
	}

	if m.factory == nil {
		m.factory = func(req Request) downloaderRunner {
			return NewDownloader(DownloaderConfig{
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

// StartWorkers starts background download workers.
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
		go m.worker(workerCtx)
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

// QueueDownload enqueues one download and returns its transfer ID.
func (m *Manager) QueueDownload(req Request) (string, error) {
	if req.ClientID == "" {
		req.ClientID = m.clientID
	}

	downloader := m.factory(req)
	if downloader == nil {
		return "", fmt.Errorf("downloader factory returned nil")
	}

	if downloader.TransferIDValue() == "" {
		downloader.SetTransferID(uuid.NewString())
	}

	if ok := m.downloadQueue.Push(downloader); !ok {
		return "", fmt.Errorf("download queue is closed")
	}

	return downloader.TransferIDValue(), nil
}

// Result returns the result for a transfer.
func (m *Manager) Result(transferID string) (Result, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, ok := m.results[transferID]
	return result, ok
}

// Success returns whether a transfer completed successfully.
func (m *Manager) Success(transferID string) (bool, bool) {
	result, ok := m.Result(transferID)
	if !ok {
		return false, false
	}
	return result.Success, true
}

// ActiveCount returns the number of currently active downloads.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.activeDownloads)
}

// ActiveDownloads returns currently active downloads.
func (m *Manager) ActiveDownloads() []ActiveDownload {
	m.mu.Lock()
	defer m.mu.Unlock()

	active := make([]ActiveDownload, 0, len(m.activeDownloads))
	for transferID, downloader := range m.activeDownloads {
		localPath, _ := downloader.LocalPath()
		total := downloader.ExpectedSize()
		done := downloader.BytesReceived()

		progressPct := 0.0
		if total > 0 {
			progressPct = float64(done) / float64(total) * 100
		}

		active = append(active, ActiveDownload{
			TransferID:    transferID,
			FileName:      filepath.Base(localPath),
			BytesReceived: done,
			FileSize:      total,
			ProgressPct:   progressPct,
		})
	}

	return active
}

// PauseDownload pauses an active download.
func (m *Manager) PauseDownload(transferID string) bool {
	m.mu.Lock()
	downloader := m.activeDownloads[transferID]
	m.mu.Unlock()

	if downloader == nil {
		return false
	}

	downloader.Pause()
	return true
}

// ResumeDownload resumes an active download.
func (m *Manager) ResumeDownload(transferID string) bool {
	m.mu.Lock()
	downloader := m.activeDownloads[transferID]
	m.mu.Unlock()

	if downloader == nil {
		return false
	}

	downloader.Resume()
	return true
}

// CancelDownload cancels an active download.
func (m *Manager) CancelDownload(transferID string) bool {
	m.mu.Lock()
	downloader := m.activeDownloads[transferID]
	m.mu.Unlock()

	if downloader == nil {
		return false
	}

	downloader.Cancel()
	return true
}

func (m *Manager) worker(ctx context.Context) {
	defer m.wg.Done()

	for {
		downloader, ok, err := m.downloadQueue.Pop(ctx)
		if err != nil || !ok {
			return
		}

		m.runDownloader(ctx, downloader)
	}
}

func (m *Manager) runDownloader(ctx context.Context, downloader downloaderRunner) {
	transferID := downloader.TransferIDValue()

	m.mu.Lock()
	m.activeDownloads[transferID] = downloader
	m.mu.Unlock()

	var downloadErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				downloadErr = fmt.Errorf("download panic: %v", r)
			}
		}()

		downloadErr = downloader.Download(ctx)
	}()

	m.mu.Lock()
	delete(m.activeDownloads, transferID)
	m.results[transferID] = Result{
		TransferID: transferID,
		Success:    downloadErr == nil,
		Err:        downloadErr,
	}
	m.mu.Unlock()
}
