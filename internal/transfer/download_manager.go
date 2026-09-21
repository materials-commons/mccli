package transfer

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/materials-commons/mccli/internal/wsclient"
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
type Factory func(req DownloadRequest) downloaderRunner

// DownloadResult describes the outcome of one download.
type DownloadResult struct {
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

// DownloadConfig configures a DownloadManager.
type DownloadConfig struct {
	// SendQueue is optional. If set, individual downloads may emit completion
	// messages to it.
	SendQueue     *wsclient.Queue[wsclient.OutboundMessage]
	Store         FileRecordStore
	ClientID      string
	MaxConcurrent int
	Factory       Factory
	Progress      Reporter
}

// DownloadManager manages queued concurrent downloads.
type DownloadManager struct {
	sendQueue *wsclient.Queue[wsclient.OutboundMessage]
	store     FileRecordStore

	clientID      string
	maxConcurrent int

	downloadQueue *wsclient.Queue[downloaderRunner]

	mu              sync.Mutex
	activeDownloads map[string]downloaderRunner
	results         map[string]DownloadResult

	started bool
	cancel  context.CancelFunc
	done    chan struct{}
	wg      sync.WaitGroup

	factory Factory
}

// NewDownloadManager creates a download manager.
func NewDownloadManager(cfg DownloadConfig) (*DownloadManager, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("file record store is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("client id is required")
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}

	m := &DownloadManager{
		sendQueue:       cfg.SendQueue,
		store:           cfg.Store,
		clientID:        cfg.ClientID,
		maxConcurrent:   cfg.MaxConcurrent,
		downloadQueue:   wsclient.NewQueue[downloaderRunner](),
		activeDownloads: map[string]downloaderRunner{},
		results:         map[string]DownloadResult{},
		factory:         cfg.Factory,
	}

	if m.factory == nil {
		m.factory = func(req DownloadRequest) downloaderRunner {
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
func (m *DownloadManager) StartWorkers(ctx context.Context) {
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
func (m *DownloadManager) StopWorkers() {
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
func (m *DownloadManager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.started
}

// QueueDownload enqueues one download and returns its transfer ID.
func (m *DownloadManager) QueueDownload(req DownloadRequest) (string, error) {
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

// DownloadResult returns the result for a transfer.
func (m *DownloadManager) Result(transferID string) (DownloadResult, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, ok := m.results[transferID]
	return result, ok
}

// Success returns whether a transfer completed successfully.
func (m *DownloadManager) Success(transferID string) (bool, bool) {
	result, ok := m.Result(transferID)
	if !ok {
		return false, false
	}
	return result.Success, true
}

// ActiveCount returns the number of currently active downloads.
func (m *DownloadManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.activeDownloads)
}

// ActiveDownloads returns currently active downloads.
func (m *DownloadManager) ActiveDownloads() []ActiveDownload {
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
func (m *DownloadManager) PauseDownload(transferID string) bool {
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
func (m *DownloadManager) ResumeDownload(transferID string) bool {
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
func (m *DownloadManager) CancelDownload(transferID string) bool {
	m.mu.Lock()
	downloader := m.activeDownloads[transferID]
	m.mu.Unlock()

	if downloader == nil {
		return false
	}

	downloader.Cancel()
	return true
}

func (m *DownloadManager) worker(ctx context.Context) {
	defer m.wg.Done()

	for {
		downloader, ok, err := m.downloadQueue.Pop(ctx)
		if err != nil || !ok {
			return
		}

		m.runDownloader(ctx, downloader)
	}
}

func (m *DownloadManager) runDownloader(ctx context.Context, downloader downloaderRunner) {
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
	m.results[transferID] = DownloadResult{
		TransferID: transferID,
		Success:    downloadErr == nil,
		Err:        downloadErr,
	}
	m.mu.Unlock()
}
