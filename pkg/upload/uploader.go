package upload

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

// ProgressFunc receives upload progress.
type ProgressFunc func(bytesSent int64, totalBytes int64)

// UploaderConfig configures one Uploader.
type UploaderConfig struct {
	SendQueue    *wsclient.Queue[wsclient.OutboundMessage]
	DBWriteQueue *wsclient.Queue[DBWriteRequest]

	Request  Request
	ClientID string

	ChunkSize  int64
	WindowSize int

	Progress ProgressFunc
}

// Uploader uploads one file over the websocket protocol.
type Uploader struct {
	SendQueue    *wsclient.Queue[wsclient.OutboundMessage]
	DBWriteQueue *wsclient.Queue[DBWriteRequest]

	Request  Request
	ClientID string

	TransferID string

	ChunkSize  int64
	WindowSize int

	Progress ProgressFunc

	responseQueue *wsclient.Queue[wsclient.TextMessage]

	mu              sync.Mutex
	bytesSent       int64
	nextChunkToSend int64
	inFlightChunks  int
	lastAckedChunk  int64
	cancelled       bool
	paused          bool
	alreadyUploaded bool
}

// NewUploader creates a Uploader.
func NewUploader(cfg UploaderConfig) *Uploader {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 1024 * 1024
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 10
	}

	return &Uploader{
		SendQueue:      cfg.SendQueue,
		DBWriteQueue:   cfg.DBWriteQueue,
		Request:        cfg.Request,
		ClientID:       cfg.ClientID,
		ChunkSize:      cfg.ChunkSize,
		WindowSize:     cfg.WindowSize,
		Progress:       cfg.Progress,
		responseQueue:  wsclient.NewQueue[wsclient.TextMessage](),
		lastAckedChunk: -1,
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
func (u *Uploader) Upload(ctx context.Context) bool {
	local := u.Request.Observation.LocalEntry
	if local == nil {
		return false
	}
	if local.Size == 0 {
		return true
	}

	if !u.sendTransferInit() {
		return false
	}
	if !u.waitForAcceptance(ctx) {
		return u.alreadyUploaded
	}
	if !u.sendChunksWindowed(ctx) {
		return false
	}
	if !u.sendTransferComplete() {
		return false
	}
	if !u.waitForFinalization(ctx) {
		return false
	}

	return true
}

func (u *Uploader) sendTransferInit() bool {
	local := u.Request.Observation.LocalEntry
	if local == nil {
		return false
	}

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

	return u.SendQueue.Push(msg)
}

func (u *Uploader) waitForAcceptance(ctx context.Context) bool {
	msg, ok, err := u.responseQueue.Pop(ctx)
	if err != nil || !ok {
		return false
	}

	command, _ := msg["command"].(string)
	payload, _ := msg["payload"].(map[string]any)

	switch command {
	case "TRANSFER_ACCEPT":
		if chunkSize, ok := numberAsInt64(payload["chunk_size"]); ok && chunkSize > 0 {
			u.ChunkSize = chunkSize
		}
		return true
	case "TRANSFER_REJECT":
		reason, _ := payload["reason"].(string)
		if reason == "file already uploaded" {
			u.alreadyUploaded = true
		}
		return false
	default:
		return false
	}
}

func (u *Uploader) sendChunksWindowed(ctx context.Context) bool {
	local := u.Request.Observation.LocalEntry
	if local == nil {
		return false
	}

	file, err := os.Open(local.Path)
	if err != nil {
		return false
	}
	defer file.Close()

	totalChunks := (local.Size + u.ChunkSize - 1) / u.ChunkSize

	errCh := make(chan error, 2)

	go func() {
		errCh <- u.processACKs(ctx, totalChunks)
	}()

	go func() {
		errCh <- u.sendChunks(ctx, file, totalChunks)
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			return false
		}
	}

	return true
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
			return fmt.Errorf("upload cancelled")
		}
		if u.nextChunkToSend >= totalChunks {
			u.mu.Unlock()
			return nil
		}
		if u.paused || u.inFlightChunks >= u.WindowSize {
			u.mu.Unlock()
			time.Sleep(10 * time.Millisecond)
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
			return err
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
			return fmt.Errorf("send queue closed")
		}
	}
}

func (u *Uploader) processACKs(ctx context.Context, totalChunks int64) error {
	for {
		u.mu.Lock()
		if u.lastAckedChunk >= totalChunks-1 {
			u.mu.Unlock()
			return nil
		}
		u.mu.Unlock()

		msg, ok, err := u.responseQueue.Pop(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("response queue closed")
		}

		command, _ := msg["command"].(string)
		payload, _ := msg["payload"].(map[string]any)

		switch command {
		case "CHUNK_ACK":
			chunkSeq, _ := numberAsInt64(payload["chunk_sequence"])
			bytesReceived, _ := numberAsInt64(payload["bytes_received"])

			u.mu.Lock()
			if chunkSeq > u.lastAckedChunk {
				u.lastAckedChunk = chunkSeq
				u.inFlightChunks = int((u.nextChunkToSend - 1) - u.lastAckedChunk)
			}
			u.bytesSent = bytesReceived
			u.mu.Unlock()

			if u.Progress != nil {
				u.Progress(bytesReceived, u.Request.Observation.LocalEntry.Size)
			}

		case "CHUNK_ERROR":
			return fmt.Errorf("chunk error")

		default:
			return fmt.Errorf("unexpected response %q while waiting for chunk ack", command)
		}
	}
}

func (u *Uploader) sendTransferComplete() bool {
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

	return u.SendQueue.Push(msg)
}

func (u *Uploader) waitForFinalization(ctx context.Context) bool {
	msg, ok, err := u.responseQueue.Pop(ctx)
	if err != nil || !ok {
		return false
	}

	command, _ := msg["command"].(string)
	payload, _ := msg["payload"].(map[string]any)

	switch command {
	case "TRANSFER_FINALIZE":
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

		return u.DBWriteQueue.Push(DBWriteRequest{Record: record})

	case "UPLOAD_FAILED":
		return false

	default:
		return false
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
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}
