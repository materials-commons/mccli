package upload

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

var (
	// ErrAlreadyUploaded indicates the server rejected the transfer because the
	// file content is already present.
	ErrAlreadyUploaded = errors.New("file already uploaded")

	// ErrTransferRejected indicates the server rejected the transfer.
	ErrTransferRejected = errors.New("transfer rejected")

	// ErrUnexpectedResponse indicates that the server sent a protocol message
	// that was not valid for the uploader's current state.
	ErrUnexpectedResponse = errors.New("unexpected upload protocol response")

	// ErrUploadCancelled indicates that the upload was cancelled by the caller.
	ErrUploadCancelled = errors.New("upload cancelled")

	// ErrQueueClosed indicates that an outbound or DB-write queue was closed.
	ErrQueueClosed = errors.New("queue closed")

	// ErrInvalidUploadRequest indicates that the upload request is incomplete or
	// internally inconsistent.
	ErrInvalidUploadRequest = errors.New("invalid upload request")
)

const (
	defaultChunkSize           int64 = 1024 * 1024
	defaultWindowSize                = 10
	defaultAcceptanceTimeout         = 30 * time.Second
	defaultACKTimeout                = 30 * time.Second
	defaultFinalizationTimeout       = 30 * time.Second
)

// ProgressStatus describes the current state of an upload.
type ProgressStatus string

const (
	ProgressStarting        ProgressStatus = "starting"
	ProgressUploading       ProgressStatus = "uploading"
	ProgressComplete        ProgressStatus = "complete"
	ProgressAlreadyUploaded ProgressStatus = "already uploaded"
	ProgressFailed          ProgressStatus = "failed"
)

// ProgressEvent describes upload progress for one transfer.
type ProgressEvent struct {
	TransferID string
	LocalPath  string
	RemotePath string
	BytesSent  int64
	TotalBytes int64
	Status     ProgressStatus
	Err        error
}

// ProgressReporter receives upload progress events.
//
// Implementations must be safe for concurrent calls from multiple uploader
// goroutines.
type ProgressReporter interface {
	ReportUploadProgress(event ProgressEvent)
}

// ProgressReporterFunc adapts a function to ProgressReporter.
type ProgressReporterFunc func(event ProgressEvent)

func (f ProgressReporterFunc) ReportUploadProgress(event ProgressEvent) {
	f(event)
}

// FileRecordStore persists uploaded file state.
type FileRecordStore interface {
	Upsert(ctx context.Context, record filedb.FileRecord) error
}

// UploaderConfig configures one Uploader.
type UploaderConfig struct {
	SendQueue *wsclient.Queue[wsclient.OutboundMessage]
	Store     FileRecordStore

	Request  Request
	ClientID string

	ChunkSize  int64
	WindowSize int

	AcceptanceTimeout   time.Duration
	ACKTimeout          time.Duration
	FinalizationTimeout time.Duration

	Progress ProgressReporter
}

// Uploader uploads one file over the websocket protocol.
type Uploader struct {
	SendQueue *wsclient.Queue[wsclient.OutboundMessage]
	Store     FileRecordStore

	Request  Request
	ClientID string

	TransferID string

	ChunkSize  int64
	WindowSize int

	AcceptanceTimeout   time.Duration
	ACKTimeout          time.Duration
	FinalizationTimeout time.Duration

	Progress ProgressReporter

	responseQueue *wsclient.Queue[wsclient.TextMessage]

	mu              sync.Mutex
	bytesSent       int64
	nextChunkToSend int64
	inFlightChunks  int
	lastAckedChunk  int64
	ackedChunks     map[int64]bool
	cancelled       bool
	paused          bool
	alreadyUploaded bool
}

// NewUploader creates a Uploader.
func NewUploader(cfg UploaderConfig) *Uploader {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = defaultChunkSize
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = defaultWindowSize
	}
	if cfg.AcceptanceTimeout <= 0 {
		cfg.AcceptanceTimeout = defaultAcceptanceTimeout
	}
	if cfg.ACKTimeout <= 0 {
		cfg.ACKTimeout = defaultACKTimeout
	}
	if cfg.FinalizationTimeout <= 0 {
		cfg.FinalizationTimeout = defaultFinalizationTimeout
	}

	return &Uploader{
		SendQueue:           cfg.SendQueue,
		Store:               cfg.Store,
		Request:             cfg.Request,
		ClientID:            cfg.ClientID,
		ChunkSize:           cfg.ChunkSize,
		WindowSize:          cfg.WindowSize,
		AcceptanceTimeout:   cfg.AcceptanceTimeout,
		ACKTimeout:          cfg.ACKTimeout,
		FinalizationTimeout: cfg.FinalizationTimeout,
		Progress:            cfg.Progress,
		ackedChunks:         make(map[int64]bool),
		responseQueue:       wsclient.NewQueue[wsclient.TextMessage](),
		lastAckedChunk:      -1,
	}
}

// TransferIDValue returns this uploader's transfer ID.
func (u *Uploader) TransferIDValue() string {
	return u.TransferID
}

// SetTransferID sets this uploader's transfer ID.
func (u *Uploader) SetTransferID(id string) {
	u.TransferID = id
}

// HandleResponse queues a response for this uploader.
func (u *Uploader) HandleResponse(msg wsclient.TextMessage) {
	u.responseQueue.Push(msg)
}

// Upload performs the websocket upload protocol.
func (u *Uploader) Upload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	local := u.Request.Observation.LocalEntry
	if local == nil {
		return fmt.Errorf("local entry is required")
	}

	if local.Path == "" {
		return fmt.Errorf("local path is required")
	}

	if local.Size == 0 {
		return nil
	}

	if err := u.validate(); err != nil {
		return err
	}

	u.resetTransferState()

	u.reportProgress(0, local.Size, ProgressStarting, nil)

	if err := u.sendTransferInit(ctx); err != nil {
		u.reportProgress(u.currentBytesSent(), local.Size, ProgressFailed, err)
		return err
	}

	if err := u.waitForAcceptance(ctx); err != nil {
		if errors.Is(err, ErrAlreadyUploaded) {
			u.reportProgress(local.Size, local.Size, ProgressAlreadyUploaded, nil)
			return nil
		}
		u.reportProgress(u.currentBytesSent(), local.Size, ProgressFailed, err)
		return err
	}

	if err := u.sendChunksWindowed(ctx); err != nil {
		u.reportProgress(u.currentBytesSent(), local.Size, ProgressFailed, err)
		return err
	}

	if err := u.sendTransferComplete(ctx); err != nil {
		u.reportProgress(u.currentBytesSent(), local.Size, ProgressFailed, err)
		return err
	}

	if err := u.waitForFinalization(ctx); err != nil {
		u.reportProgress(u.currentBytesSent(), local.Size, ProgressFailed, err)
		return err
	}

	fmt.Println("Progress Complete")
	u.reportProgress(local.Size, local.Size, ProgressComplete, nil)

	return nil
}

func (u *Uploader) validate() error {
	if u.SendQueue == nil {
		return fmt.Errorf("send queue is required")
	}
	if u.Store == nil {
		return fmt.Errorf("file record store is required")
	}
	if u.ClientID == "" {
		return fmt.Errorf("client id is required")
	}
	if u.TransferID == "" {
		return fmt.Errorf("transfer id is required")
	}
	if u.Request.ProjectID <= 0 {
		return fmt.Errorf("project id must be positive")
	}
	if u.Request.Observation.RemotePath == "" {
		return fmt.Errorf("remote path is required")
	}
	if u.Request.Observation.LocalEntry == nil {
		return fmt.Errorf("local entry is required")
	}
	if u.Request.Observation.LocalEntry.Path == "" {
		return fmt.Errorf("local path is required")
	}
	if u.Request.Observation.LocalEntry.Size > 0 {
		if u.Request.UpdatedRecord.LocalChecksum == nil || *u.Request.UpdatedRecord.LocalChecksum == "" {
			return fmt.Errorf("%w: local checksum is required for non-empty upload %q",
				ErrInvalidUploadRequest, u.Request.Observation.LocalEntry.Path)
		}
	}
	if u.ChunkSize <= 0 {
		return fmt.Errorf("chunk size must be positive")
	}
	if u.WindowSize <= 0 {
		return fmt.Errorf("window size must be positive")
	}
	return nil
}

func (u *Uploader) resetTransferState() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.bytesSent = 0
	u.nextChunkToSend = 0
	u.inFlightChunks = 0
	u.lastAckedChunk = -1
	u.ackedChunks = map[int64]bool{}
	u.alreadyUploaded = false
}

func (u *Uploader) sendTransferInit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	local := u.Request.Observation.LocalEntry
	if local == nil {
		return fmt.Errorf("local entry is required")
	}

	fmt.Println("Sending transfer init for transfer ID:", u.TransferID)
	msg := wsclient.TextMessage{
		"command":   "TRANSFER_INIT",
		"id":        u.TransferID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"client_id": u.ClientID,
		"payload": map[string]any{
			"transfer_id":  u.TransferID,
			"project_id":   u.Request.ProjectID,
			"file_path":    local.Path,
			"project_path": u.Request.Observation.RemotePath,
			"file_size":    local.Size,
			"chunk_size":   u.ChunkSize,
			"checksum":     checksumValue(u.Request.UpdatedRecord),
		},
	}

	if ok := u.SendQueue.Push(msg); !ok {
		return fmt.Errorf("%w: send transfer init", ErrQueueClosed)
	}

	return nil
}

func (u *Uploader) waitForAcceptance(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, u.AcceptanceTimeout)
	defer cancel()

	msg, ok, err := u.responseQueue.Pop(waitCtx)
	fmt.Printf("waitForAcceptance %+v, %v, %s\n", msg, ok, err)
	if err != nil {
		return fmt.Errorf("wait for transfer acceptance: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: response queue closed while waiting for acceptance", ErrQueueClosed)
	}

	command, _ := msg["command"].(string)
	payload, err := payloadMap(msg)
	if err != nil {
		return err
	}

	if err := u.validateResponseTransferID(payload); err != nil {
		return err
	}

	switch command {
	case "TRANSFER_ACCEPT":
		if chunkSize, ok := numberAsInt64(payload["chunk_size"]); ok && chunkSize > 0 {
			u.ChunkSize = chunkSize
		}
		return nil

	case "TRANSFER_ALREADY_UPLOADED":
		// The message will contain the information we need to update the database.
		return nil

	case "TRANSFER_REJECT":
		reason, _ := payload["reason"].(string)
		if reason == "file already uploaded" {
			u.alreadyUploaded = true
			return ErrAlreadyUploaded
		}
		if reason == "" {
			reason = "unknown"
		}
		return fmt.Errorf("%w: %s", ErrTransferRejected, reason)

	default:
		return fmt.Errorf("%w: got %q while waiting for TRANSFER_ACCEPT", ErrUnexpectedResponse, command)
	}
}

func (u *Uploader) sendChunksWindowed(ctx context.Context) error {
	local := u.Request.Observation.LocalEntry
	if local == nil {
		return fmt.Errorf("local entry is required")
	}

	file, err := os.Open(local.Path)
	if err != nil {
		return fmt.Errorf("open file for upload %q: %w", local.Path, err)
	}
	defer file.Close()

	totalChunks := (local.Size + u.ChunkSize - 1) / u.ChunkSize
	u.resetChunkState(0)

	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	go func() {
		errCh <- u.processACKs(uploadCtx, totalChunks)
	}()

	go func() {
		errCh <- u.sendChunks(uploadCtx, file, totalChunks)
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil && firstErr == nil {
			firstErr = err
			cancel()
		}
	}

	if firstErr != nil {
		return firstErr
	}

	return nil
}

func (u *Uploader) resetChunkState(startChunk int64) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.bytesSent = startChunk * u.ChunkSize
	u.nextChunkToSend = startChunk
	u.inFlightChunks = 0
	u.lastAckedChunk = startChunk - 1
	u.ackedChunks = make(map[int64]bool)
}

func (u *Uploader) sendChunks(ctx context.Context, file *os.File, totalChunks int64) error {
	reader := bufio.NewReader(file)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		u.mu.Lock()
		if u.cancelled {
			u.mu.Unlock()
			return ErrUploadCancelled
		}
		if u.nextChunkToSend >= totalChunks {
			u.mu.Unlock()
			return nil
		}
		if u.paused || u.inFlightChunks >= u.WindowSize {
			u.mu.Unlock()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}

			continue
		}

		sequence := u.nextChunkToSend
		u.nextChunkToSend++
		u.inFlightChunks++
		u.mu.Unlock()

		chunk := make([]byte, u.ChunkSize)
		n, err := io.ReadFull(reader, chunk)
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			chunk = chunk[:n]
		} else if err != nil {
			return fmt.Errorf("read upload chunk %d: %w", sequence, err)
		}

		if len(chunk) == 0 {
			return nil
		}

		frame := wsclient.BinaryFrame{
			Header: map[string]any{
				"transfer_id": u.TransferID,
				"sequence":    sequence,
				"size":        len(chunk),
				"is_last":     sequence == totalChunks-1,
			},
			Data: chunk,
		}

		if ok := u.SendQueue.Push(frame); !ok {
			return fmt.Errorf("%w: send chunk %d", ErrQueueClosed, sequence)
		}
	}
}

func (u *Uploader) processACKs(ctx context.Context, totalChunks int64) error {
	if totalChunks <= 0 {
		return nil
	}

	for {
		u.mu.Lock()
		ackedCount := len(u.ackedChunks)
		u.mu.Unlock()

		if int64(ackedCount) >= totalChunks {
			return nil
		}

		waitCtx, cancel := context.WithTimeout(ctx, u.ACKTimeout)
		msg, ok, err := u.responseQueue.Pop(waitCtx)
		cancel()

		if err != nil {
			return fmt.Errorf("wait for chunk ack: %w", err)
		}
		if !ok {
			return fmt.Errorf("%w: response queue closed while waiting for chunk ack", ErrQueueClosed)
		}

		command, _ := msg["command"].(string)
		payload, err := payloadMap(msg)
		if err != nil {
			return err
		}

		if err := u.validateResponseTransferID(payload); err != nil {
			return err
		}

		switch command {
		case "CHUNK_ACK":
			chunkSeq, ok := numberAsInt64(payload["chunk_sequence"])
			if !ok {
				return fmt.Errorf("%w: missing chunk_sequence", ErrUnexpectedResponse)
			}
			bytesReceived, ok := numberAsInt64(payload["bytes_received"])
			if !ok {
				return fmt.Errorf("%w: missing bytes_received", ErrUnexpectedResponse)
			}
			if chunkSeq < 0 || chunkSeq >= totalChunks {
				return fmt.Errorf("%w: invalid chunk_sequence %d", ErrUnexpectedResponse, chunkSeq)
			}

			u.mu.Lock()
			if u.ackedChunks == nil {
				u.ackedChunks = map[int64]bool{}
			}

			if !u.ackedChunks[chunkSeq] {
				u.ackedChunks[chunkSeq] = true
				if u.inFlightChunks > 0 {
					u.inFlightChunks--
				}
			}

			for u.ackedChunks[u.lastAckedChunk+1] {
				u.lastAckedChunk++
			}

			if bytesReceived > u.bytesSent {
				u.bytesSent = bytesReceived
			}
			currentBytesSent := u.bytesSent
			u.mu.Unlock()

			u.reportProgress(currentBytesSent, u.Request.Observation.LocalEntry.Size, ProgressUploading, nil)

		case "CHUNK_ERROR":
			reason, _ := payload["error"].(string)
			if reason == "" {
				reason = "unknown"
			}
			return fmt.Errorf("chunk error: %s", reason)

		default:
			return fmt.Errorf("%w: got %q while waiting for CHUNK_ACK", ErrUnexpectedResponse, command)
		}
	}
}

func (u *Uploader) sendTransferComplete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		fmt.Println("sendTransferComplete ", u.TransferID, err)
		return err
	}

	fmt.Println("Sent TRANSFER_COMPLETE", u.TransferID)
	msg := wsclient.TextMessage{
		"command":   "TRANSFER_COMPLETE",
		"id":        u.TransferID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"client_id": u.ClientID,
		"payload": map[string]any{
			"transfer_id": u.TransferID,
			"total_bytes": u.bytesSent,
		},
	}

	if ok := u.SendQueue.Push(msg); !ok {
		return fmt.Errorf("%w: send transfer complete", ErrQueueClosed)
	}

	return nil
}

func (u *Uploader) waitForFinalization(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, u.FinalizationTimeout)
	defer cancel()

	msg, ok, err := u.responseQueue.Pop(waitCtx)
	if err != nil {
		return fmt.Errorf("wait for transfer finalization: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: response queue closed while waiting for finalization", ErrQueueClosed)
	}

	command, _ := msg["command"].(string)
	payload, err := payloadMap(msg)
	if err != nil {
		return err
	}

	if err := u.validateResponseTransferID(payload); err != nil {
		return err
	}

	switch command {
	case "TRANSFER_FINALIZE":
		fmt.Println("Received TRANSFER_FINALIZE response")
		record := u.Request.UpdatedRecord
		record.LocalLastSeenTS = time.Now().Unix()

		if checksum, _ := payload["file_checksum"].(string); checksum != "" {
			record.RemoteChecksum = &checksum
		}
		if size, ok := numberAsInt64(payload["file_size"]); ok {
			record.RemoteSize = &size
		}
		if id, ok := numberAsInt64(payload["file_id"]); ok {
			record.RemoteFileID = &id
		}
		if createdNS, ok := numberAsInt64(payload["file_created_at_ns"]); ok {
			record.RemoteCTimeNS = &createdNS
		}

		fmt.Printf("  Upserting record: %v\n", record)
		if err := u.Store.Upsert(ctx, record); err != nil {
			return fmt.Errorf("upsert finalized upload record %q: %w", record.Path, err)
		}

		return nil

	case "UPLOAD_FAILED":
		reason, _ := payload["error"].(string)
		if reason == "" {
			reason = "unknown"
		}
		return fmt.Errorf("upload failed: %s", reason)

	default:
		return fmt.Errorf("%w: got %q while waiting for TRANSFER_FINALIZE", ErrUnexpectedResponse, command)
	}
}

// Pause pauses this upload.
func (u *Uploader) Pause() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.paused = true
}

// Resume resumes this upload.
func (u *Uploader) Resume() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.paused = false
}

// Cancel cancels this upload.
func (u *Uploader) Cancel() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.cancelled = true
}

func (u *Uploader) validateResponseTransferID(payload map[string]any) error {
	transferID, _ := payload["transfer_id"].(string)
	if transferID == "" {
		return fmt.Errorf("%w: response missing transfer_id", ErrUnexpectedResponse)
	}
	if transferID != u.TransferID {
		return fmt.Errorf("%w: response transfer_id %q does not match %q", ErrUnexpectedResponse, transferID, u.TransferID)
	}
	return nil
}

func (u *Uploader) reportProgress(bytesSent int64, totalBytes int64, status ProgressStatus, err error) {
	if u.Progress == nil {
		return
	}

	localPath := ""
	if u.Request.Observation.LocalEntry != nil {
		localPath = u.Request.Observation.LocalEntry.Path
	}

	u.Progress.ReportUploadProgress(ProgressEvent{
		TransferID: u.TransferID,
		LocalPath:  localPath,
		RemotePath: u.Request.Observation.RemotePath,
		BytesSent:  bytesSent,
		TotalBytes: totalBytes,
		Status:     status,
		Err:        err,
	})
}

func (u *Uploader) currentBytesSent() int64 {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.bytesSent
}

func payloadMap(msg wsclient.TextMessage) (map[string]any, error) {
	payload, ok := msg["payload"].(map[string]any)
	if !ok || payload == nil {
		return nil, fmt.Errorf("%w: response payload is missing or invalid", ErrUnexpectedResponse)
	}
	return payload, nil
}

func checksumValue(record filedb.FileRecord) string {
	if record.LocalChecksum == nil {
		return ""
	}
	return *record.LocalChecksum
}

func numberAsInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	default:
		return 0, false
	}
}
