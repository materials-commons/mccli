package upload

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/reconcile"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

func TestUploaderSendTransferInit(t *testing.T) {
	uploader, sendQueue, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	if err := uploader.sendTransferInit(context.Background()); err != nil {
		t.Fatalf("sendTransferInit() error = %v, want nil", err)
	}

	msg := popOutbound[wsclient.TextMessage](t, sendQueue)

	if msg["command"] != "TRANSFER_INIT" {
		t.Fatalf("command = %v, want TRANSFER_INIT", msg["command"])
	}

	payload := msg["payload"].(map[string]any)
	if payload["transfer_id"] != "transfer-1" {
		t.Fatalf("transfer_id = %v, want transfer-1", payload["transfer_id"])
	}
	if payload["project_id"] != 123 {
		t.Fatalf("project_id = %v, want 123", payload["project_id"])
	}
	if payload["project_path"] != "/example.txt" {
		t.Fatalf("project_path = %v, want /example.txt", payload["project_path"])
	}
	if payload["checksum"] != "local-md5" {
		t.Fatalf("checksum = %v, want local-md5", payload["checksum"])
	}
}

func TestUploaderWaitForAcceptanceAcceptAdjustsChunkSize(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "TRANSFER_ACCEPT",
		"payload": map[string]any{
			"transfer_id": "transfer-1",
			"chunk_size":  7,
		},
	})

	if err := uploader.waitForAcceptance(context.Background()); err != nil {
		t.Fatalf("waitForAcceptance() error = %v, want nil", err)
	}

	if uploader.ChunkSize != 7 {
		t.Fatalf("ChunkSize = %d, want 7", uploader.ChunkSize)
	}
}

func TestUploaderWaitForAcceptanceRejectAlreadyUploaded(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "TRANSFER_REJECT",
		"payload": map[string]any{
			"transfer_id": "transfer-1",
			"reason":      "file already uploaded",
		},
	})

	err := uploader.waitForAcceptance(context.Background())
	if !errors.Is(err, ErrAlreadyUploaded) {
		t.Fatalf("waitForAcceptance() error = %v, want ErrAlreadyUploaded", err)
	}

	if !uploader.alreadyUploaded {
		t.Fatal("alreadyUploaded = false, want true")
	}
}

func TestUploaderSendChunksWindowedSendsBinaryFrames(t *testing.T) {
	uploader, sendQueue, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	go func() {
		uploader.HandleResponse(chunkACK("transfer-1", 0, 5))
		uploader.HandleResponse(chunkACK("transfer-1", 1, 10))
		uploader.HandleResponse(chunkACK("transfer-1", 2, 11))
	}()

	if err := uploader.sendChunksWindowed(context.Background()); err != nil {
		t.Fatalf("sendChunksWindowed() error = %v, want nil", err)
	}

	frames := sendQueue.Drain()
	if len(frames) != 3 {
		t.Fatalf("len(frames) = %d, want 3", len(frames))
	}

	wantData := [][]byte{
		[]byte("hello"),
		[]byte(" worl"),
		[]byte("d"),
	}

	for i, item := range frames {
		frame, ok := item.(wsclient.BinaryFrame)
		if !ok {
			t.Fatalf("frame %d type = %T, want BinaryFrame", i, item)
		}

		if frame.Header["transfer_id"] != "transfer-1" {
			t.Fatalf("frame %d transfer_id = %v", i, frame.Header["transfer_id"])
		}
		if frame.Header["sequence"] != int64(i) {
			t.Fatalf("frame %d sequence = %v, want %d", i, frame.Header["sequence"], i)
		}
		if string(frame.Data) != string(wantData[i]) {
			t.Fatalf("frame %d data = %q, want %q", i, string(frame.Data), string(wantData[i]))
		}
	}
}

func TestUploaderProcessACKsReportsProgress(t *testing.T) {
	var progress []int64

	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, func(sent int64, total int64) {
		progress = append(progress, sent)
	})
	uploader.TransferID = "transfer-1"

	uploader.nextChunkToSend = 3
	uploader.inFlightChunks = 3

	uploader.HandleResponse(chunkACK("transfer-1", 0, 5))
	uploader.HandleResponse(chunkACK("transfer-1", 1, 10))
	uploader.HandleResponse(chunkACK("transfer-1", 2, 11))

	if err := uploader.processACKs(context.Background(), 3); err != nil {
		t.Fatalf("processACKs() error = %v", err)
	}

	if len(progress) != 3 {
		t.Fatalf("len(progress) = %d, want 3", len(progress))
	}
	if progress[2] != 11 {
		t.Fatalf("last progress = %d, want 11", progress[2])
	}
	if uploader.bytesSent != 11 {
		t.Fatalf("bytesSent = %d, want 11", uploader.bytesSent)
	}
}

func TestUploaderSendTransferComplete(t *testing.T) {
	uploader, sendQueue, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	uploader.bytesSent = 11

	if err := uploader.sendTransferComplete(context.Background()); err != nil {
		t.Fatalf("sendTransferComplete() error = %v, want nil", err)
	}

	msg := popOutbound[wsclient.TextMessage](t, sendQueue)

	if msg["command"] != "TRANSFER_COMPLETE" {
		t.Fatalf("command = %v, want TRANSFER_COMPLETE", msg["command"])
	}

	payload := msg["payload"].(map[string]any)
	if payload["total_bytes"] != int64(11) {
		t.Fatalf("total_bytes = %v, want 11", payload["total_bytes"])
	}
}

func TestUploaderWaitForFinalizationQueuesDBWrite(t *testing.T) {
	uploader, _, dbQueue := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "TRANSFER_FINALIZE",
		"payload": map[string]any{
			"transfer_id":        "transfer-1",
			"file_checksum":      "remote-md5",
			"file_size":          11,
			"file_id":            456,
			"file_created_at_ns": 789,
		},
	})

	if err := uploader.waitForFinalization(context.Background()); err != nil {
		t.Fatalf("waitForFinalization() error = %v, want nil", err)
	}

	reqs := dbQueue.Drain()
	if len(reqs) != 1 {
		t.Fatalf("len(db writes) = %d, want 1", len(reqs))
	}

	record := reqs[0].Record
	if record.RemoteChecksum == nil || *record.RemoteChecksum != "remote-md5" {
		t.Fatalf("RemoteChecksum = %v, want remote-md5", record.RemoteChecksum)
	}
	if record.RemoteSize == nil || *record.RemoteSize != 11 {
		t.Fatalf("RemoteSize = %v, want 11", record.RemoteSize)
	}
	if record.RemoteFileID == nil || *record.RemoteFileID != 456 {
		t.Fatalf("RemoteFileID = %v, want 456", record.RemoteFileID)
	}
	if record.RemoteCTimeNS == nil || *record.RemoteCTimeNS != 789 {
		t.Fatalf("RemoteCTimeNS = %v, want 789", record.RemoteCTimeNS)
	}
}

func TestUploaderUploadZeroSizeSkips(t *testing.T) {
	uploader, sendQueue, dbQueue := makeUploader(t, nil, 5, 10, nil)

	if err := uploader.Upload(context.Background()); err != nil {
		t.Fatalf("Upload() error = %v, want nil for empty file", err)
	}

	if sendQueue.Len() != 0 {
		t.Fatalf("sendQueue.Len() = %d, want 0", sendQueue.Len())
	}
	if dbQueue.Len() != 0 {
		t.Fatalf("dbQueue.Len() = %d, want 0", dbQueue.Len())
	}
}

func TestUploaderWaitForAcceptanceRejectsWrongTransferID(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "TRANSFER_ACCEPT",
		"payload": map[string]any{
			"transfer_id": "other-transfer",
		},
	})

	err := uploader.waitForAcceptance(context.Background())
	if !errors.Is(err, ErrUnexpectedResponse) {
		t.Fatalf("waitForAcceptance() error = %v, want ErrUnexpectedResponse", err)
	}
}

func TestUploaderWaitForAcceptanceTimesOut(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	uploader.AcceptanceTimeout = 10 * time.Millisecond

	err := uploader.waitForAcceptance(context.Background())
	if err == nil {
		t.Fatal("waitForAcceptance() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("waitForAcceptance() error = %v, want deadline exceeded", err)
	}
}

func TestUploaderProcessACKsRejectsWrongTransferID(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	uploader.nextChunkToSend = 1
	uploader.inFlightChunks = 1

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "CHUNK_ACK",
		"payload": map[string]any{
			"transfer_id":    "other-transfer",
			"chunk_sequence": int64(0),
			"bytes_received": int64(5),
		},
	})

	err := uploader.processACKs(context.Background(), 1)
	if !errors.Is(err, ErrUnexpectedResponse) {
		t.Fatalf("processACKs() error = %v, want ErrUnexpectedResponse", err)
	}
}

func TestUploaderProcessACKsRejectsMissingPayload(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	uploader.nextChunkToSend = 1
	uploader.inFlightChunks = 1

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "CHUNK_ACK",
	})

	err := uploader.processACKs(context.Background(), 1)
	if !errors.Is(err, ErrUnexpectedResponse) {
		t.Fatalf("processACKs() error = %v, want ErrUnexpectedResponse", err)
	}
}

func TestUploaderProcessACKsReturnsChunkError(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	uploader.nextChunkToSend = 1
	uploader.inFlightChunks = 1

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "CHUNK_ERROR",
		"payload": map[string]any{
			"transfer_id": "transfer-1",
			"error":       "bad chunk",
		},
	})

	err := uploader.processACKs(context.Background(), 1)
	if err == nil {
		t.Fatal("processACKs() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "bad chunk") {
		t.Fatalf("processACKs() error = %v, want bad chunk", err)
	}
}

func TestUploaderSendChunksWindowedReturnsWhenACKProcessorFails(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "CHUNK_ERROR",
		"payload": map[string]any{
			"transfer_id": "transfer-1",
			"error":       "bad chunk",
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := uploader.sendChunksWindowed(ctx)
	if err == nil {
		t.Fatal("sendChunksWindowed() error = nil, want error")
	}
}

func TestUploaderSendTransferInitFailsWhenSendQueueClosed(t *testing.T) {
	uploader, sendQueue, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	sendQueue.Close()

	err := uploader.sendTransferInit(context.Background())
	if !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("sendTransferInit() error = %v, want ErrQueueClosed", err)
	}
}

func TestUploaderWaitForFinalizationFailsWhenDBQueueClosed(t *testing.T) {
	uploader, _, dbQueue := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	dbQueue.Close()

	uploader.HandleResponse(wsclient.TextMessage{
		"command": "TRANSFER_FINALIZE",
		"payload": map[string]any{
			"transfer_id": "transfer-1",
			"file_id":     456,
		},
	})

	err := uploader.waitForFinalization(context.Background())
	if !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("waitForFinalization() error = %v, want ErrQueueClosed", err)
	}
}

func TestUploaderUploadZeroSizeDoesNotRequireTransferID(t *testing.T) {
	uploader, sendQueue, dbQueue := makeUploader(t, nil, 5, 10, nil)
	uploader.TransferID = ""

	if err := uploader.Upload(context.Background()); err != nil {
		t.Fatalf("Upload() error = %v, want nil for empty file", err)
	}

	if sendQueue.Len() != 0 {
		t.Fatalf("sendQueue.Len() = %d, want 0", sendQueue.Len())
	}
	if dbQueue.Len() != 0 {
		t.Fatalf("dbQueue.Len() = %d, want 0", dbQueue.Len())
	}
}

func TestUploaderUploadRejectsBlankChecksumForNonEmptyFile(t *testing.T) {
	uploader, sendQueue, dbQueue := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	blank := ""
	uploader.Request.UpdatedRecord.LocalChecksum = &blank

	err := uploader.Upload(context.Background())
	if !errors.Is(err, ErrInvalidUploadRequest) {
		t.Fatalf("Upload() error = %v, want ErrInvalidUploadRequest", err)
	}

	if sendQueue.Len() != 0 {
		t.Fatalf("sendQueue.Len() = %d, want 0", sendQueue.Len())
	}
	if dbQueue.Len() != 0 {
		t.Fatalf("dbQueue.Len() = %d, want 0", dbQueue.Len())
	}
}

func TestUploaderUploadRejectsMissingChecksumForNonEmptyFile(t *testing.T) {
	uploader, sendQueue, dbQueue := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	uploader.Request.UpdatedRecord.LocalChecksum = nil

	err := uploader.Upload(context.Background())
	if !errors.Is(err, ErrInvalidUploadRequest) {
		t.Fatalf("Upload() error = %v, want ErrInvalidUploadRequest", err)
	}

	if sendQueue.Len() != 0 {
		t.Fatalf("sendQueue.Len() = %d, want 0", sendQueue.Len())
	}
	if dbQueue.Len() != 0 {
		t.Fatalf("dbQueue.Len() = %d, want 0", dbQueue.Len())
	}
}

func TestUploaderProcessACKsHandlesOutOfOrderACKs(t *testing.T) {
	var progress []int64

	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, func(sent int64, total int64) {
		progress = append(progress, sent)
	})
	uploader.TransferID = "transfer-1"
	uploader.ackedChunks = map[int64]bool{}
	uploader.nextChunkToSend = 3
	uploader.inFlightChunks = 3

	uploader.HandleResponse(chunkACK("transfer-1", 2, 11))
	uploader.HandleResponse(chunkACK("transfer-1", 0, 5))
	uploader.HandleResponse(chunkACK("transfer-1", 1, 10))

	if err := uploader.processACKs(context.Background(), 3); err != nil {
		t.Fatalf("processACKs() error = %v", err)
	}

	if len(uploader.ackedChunks) != 3 {
		t.Fatalf("len(ackedChunks) = %d, want 3", len(uploader.ackedChunks))
	}
	if uploader.lastAckedChunk != 2 {
		t.Fatalf("lastAckedChunk = %d, want 2", uploader.lastAckedChunk)
	}
	if uploader.bytesSent != 11 {
		t.Fatalf("bytesSent = %d, want 11", uploader.bytesSent)
	}
	if uploader.inFlightChunks != 0 {
		t.Fatalf("inFlightChunks = %d, want 0", uploader.inFlightChunks)
	}
	if len(progress) != 3 {
		t.Fatalf("len(progress) = %d, want 3", len(progress))
	}
}

func TestUploaderProcessACKsDoesNotCompleteOnHighestACKOnly(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	uploader.ACKTimeout = 10 * time.Millisecond
	uploader.ackedChunks = map[int64]bool{}
	uploader.nextChunkToSend = 3
	uploader.inFlightChunks = 3

	uploader.HandleResponse(chunkACK("transfer-1", 2, 11))

	err := uploader.processACKs(context.Background(), 3)
	if err == nil {
		t.Fatal("processACKs() error = nil, want timeout waiting for missing ACKs")
	}

	if len(uploader.ackedChunks) != 1 {
		t.Fatalf("len(ackedChunks) = %d, want 1", len(uploader.ackedChunks))
	}
	if uploader.lastAckedChunk != -1 {
		t.Fatalf("lastAckedChunk = %d, want -1 because chunks 0 and 1 were not ACKed", uploader.lastAckedChunk)
	}
}

func TestUploaderProcessACKsIgnoresDuplicateACKForCompletion(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"
	uploader.ackedChunks = map[int64]bool{}
	uploader.nextChunkToSend = 3
	uploader.inFlightChunks = 3

	uploader.HandleResponse(chunkACK("transfer-1", 0, 5))
	uploader.HandleResponse(chunkACK("transfer-1", 0, 5))
	uploader.HandleResponse(chunkACK("transfer-1", 1, 10))
	uploader.HandleResponse(chunkACK("transfer-1", 2, 11))

	if err := uploader.processACKs(context.Background(), 3); err != nil {
		t.Fatalf("processACKs() error = %v", err)
	}

	if len(uploader.ackedChunks) != 3 {
		t.Fatalf("len(ackedChunks) = %d, want 3", len(uploader.ackedChunks))
	}
	if uploader.inFlightChunks != 0 {
		t.Fatalf("inFlightChunks = %d, want 0", uploader.inFlightChunks)
	}
}

func TestUploaderSendChunksWindowedResetsStaleACKState(t *testing.T) {
	uploader, _, _ := makeUploader(t, []byte("hello world"), 5, 10, nil)
	uploader.TransferID = "transfer-1"

	uploader.ackedChunks[0] = true
	uploader.ackedChunks[1] = true
	uploader.ackedChunks[2] = true
	uploader.lastAckedChunk = 2
	uploader.nextChunkToSend = 3
	uploader.inFlightChunks = 0

	uploader.HandleResponse(chunkACK("transfer-1", 0, 5))
	uploader.HandleResponse(chunkACK("transfer-1", 1, 10))
	uploader.HandleResponse(chunkACK("transfer-1", 2, 11))

	if err := uploader.sendChunksWindowed(context.Background()); err != nil {
		t.Fatalf("sendChunksWindowed() error = %v, want nil", err)
	}

	if len(uploader.ackedChunks) != 3 {
		t.Fatalf("len(ackedChunks) = %d, want 3 fresh ACKs", len(uploader.ackedChunks))
	}
	if uploader.lastAckedChunk != 2 {
		t.Fatalf("lastAckedChunk = %d, want 2", uploader.lastAckedChunk)
	}
}

func makeUploader(t *testing.T, fileBytes []byte, chunkSize int64, windowSize int, progress ProgressFunc) (*Uploader, *wsclient.Queue[wsclient.OutboundMessage], *wsclient.Queue[DBWriteRequest]) {
	t.Helper()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "example.txt")
	if err := os.WriteFile(filePath, fileBytes, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	localChecksum := "local-md5"

	sendQueue := wsclient.NewQueue[wsclient.OutboundMessage]()
	dbQueue := wsclient.NewQueue[DBWriteRequest]()

	record := filedb.FileRecord{
		Path:             "/example.txt",
		Dir:              "/",
		Name:             "example.txt",
		LocalSize:        int64(len(fileBytes)),
		LocalMTimeNS:     time.Unix(100, 0).UnixNano(),
		LocalCTimeNS:     time.Unix(100, 0).UnixNano(),
		LocalLastSeenTS:  100,
		LocalChecksum:    &localChecksum,
		IsCleanLocalCopy: false,
	}

	uploader := NewUploader(UploaderConfig{
		SendQueue:    sendQueue,
		DBWriteQueue: dbQueue,
		Request: Request{
			ProjectID: 123,
			ClientID:  "client-123",
			Observation: reconcile.Observation{
				RemotePath: "/example.txt",
				Name:       "example.txt",
				Dir:        "/",
				LocalEntry: &reconcile.LocalEntry{
					Path:       filePath,
					RemotePath: "/example.txt",
					Name:       "example.txt",
					Dir:        "/",
					Kind:       reconcile.KindFile,
					Size:       int64(len(fileBytes)),
					MTimeNS:    time.Unix(100, 0).UnixNano(),
					CTimeNS:    time.Unix(100, 0).UnixNano(),
				},
			},
			UpdatedRecord: record,
		},
		ClientID:   "client-123",
		ChunkSize:  chunkSize,
		WindowSize: windowSize,
		Progress:   progress,
	})

	return uploader, sendQueue, dbQueue
}

func popOutbound[T wsclient.OutboundMessage](t *testing.T, q *wsclient.Queue[wsclient.OutboundMessage]) T {
	t.Helper()

	item, ok, err := q.Pop(context.Background())
	if err != nil {
		t.Fatalf("Pop() error = %v", err)
	}
	if !ok {
		t.Fatal("Pop() ok = false, want true")
	}

	got, ok := item.(T)
	if !ok {
		t.Fatalf("item type = %T, want requested type", item)
	}

	return got
}

func chunkACK(transferID string, sequence int64, bytesReceived int64) wsclient.TextMessage {
	return wsclient.TextMessage{
		"command": "CHUNK_ACK",
		"payload": map[string]any{
			"transfer_id":    transferID,
			"chunk_sequence": sequence,
			"bytes_received": bytesReceived,
		},
	}
}
