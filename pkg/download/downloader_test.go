package download

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/materials-commons/mccli/pkg/conv"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/reconcile"
	"github.com/materials-commons/mccli/pkg/transfer"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

func TestDownloaderDownloadsFileAndReportsProgress(t *testing.T) {
	ctx := context.Background()
	body := []byte("hello range download")
	checksum := md5Hex(body)

	store := &fakeStore{}
	progress := &fakeProgress{}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q, want Bearer token-1", got)
		}
		if got := req.Header.Get("Range"); got != "" {
			t.Fatalf("Range = %q, want empty for fresh download", got)
		}

		return response(http.StatusOK, body), nil
	})

	localPath := filepath.Join(t.TempDir(), "Dir1", "file.txt")
	sendQueue := wsclient.NewQueue[wsclient.OutboundMessage]()

	d := NewDownloader(DownloaderConfig{
		SendQueue:  sendQueue,
		Store:      store,
		ClientID:   "client-1",
		TransferID: "transfer-1",
		Request: Request{
			ProjectID: 1,
			BaseURL:   "https://example.test/api",
			APIToken:  "token-1",
			LocalPath: localPath,
			Observation: reconcile.Observation{
				RemotePath:  "/Dir1/file.txt",
				Name:        "file.txt",
				Dir:         "/Dir1",
				RemoteEntry: remoteEntry(10, int64(len(body)), checksum),
			},
			UpdatedRecord: filedb.FileRecord{
				Path: "/Dir1/file.txt",
				Dir:  "/Dir1",
				Name: "file.txt",
			},
		},
		ChunkSize:  4,
		HTTPClient: client,
		Progress:   progress,
		Now:        func() time.Time { return time.Unix(123, 0) },
	})

	if err := d.Download(ctx); err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("downloaded body = %q, want %q", got, body)
	}

	if store.record.Path != "/Dir1/file.txt" {
		t.Fatalf("record.Path = %q, want /Dir1/file.txt", store.record.Path)
	}
	if store.record.RemoteChecksum == nil || *store.record.RemoteChecksum != checksum {
		t.Fatalf("RemoteChecksum = %v, want %s", store.record.RemoteChecksum, checksum)
	}
	if store.record.LocalChecksum == nil || *store.record.LocalChecksum != checksum {
		t.Fatalf("LocalChecksum = %v, want %s", store.record.LocalChecksum, checksum)
	}
	if store.record.RemoteFileID == nil || *store.record.RemoteFileID != 10 {
		t.Fatalf("RemoteFileID = %v, want 10", store.record.RemoteFileID)
	}

	if len(progress.events) == 0 {
		t.Fatal("progress events = 0, want at least one")
	}
	last := progress.events[len(progress.events)-1]
	if last.Status != transfer.StatusComplete {
		t.Fatalf("last progress status = %q, want complete", last.Status)
	}
	if last.BytesDone != int64(len(body)) {
		t.Fatalf("last BytesDone = %d, want %d", last.BytesDone, len(body))
	}

	msg, ok, err := sendQueue.Pop(ctx)
	if err != nil || !ok {
		t.Fatalf("Pop completion message ok=%v err=%v", ok, err)
	}
	text, ok := msg.(wsclient.TextMessage)
	if !ok {
		t.Fatalf("completion message type = %T, want TextMessage", msg)
	}
	if text["command"] != "DOWNLOAD_COMPLETE" {
		t.Fatalf("command = %v, want DOWNLOAD_COMPLETE", text["command"])
	}
}

func TestDownloaderResumesWithRangeRequest(t *testing.T) {
	ctx := context.Background()
	full := []byte("hello resumed download")
	prefix := []byte("hello ")
	suffix := full[len(prefix):]
	checksum := md5Hex(full)

	tmp := t.TempDir()
	localPath := filepath.Join(tmp, "file.txt")
	partPath := localPath + ".part"
	metaPath := localPath + ".meta.json"

	if err := os.WriteFile(partPath, prefix, 0o644); err != nil {
		t.Fatalf("WriteFile(part) error = %v", err)
	}
	if err := os.WriteFile(metaPath, []byte(`{
  "transfer_id": "transfer-1",
  "file_id": 10,
  "file_path": "`+filepath.ToSlash(localPath)+`",
  "download_url": "https://example.test/api/projects/1/files/10/download",
  "bytes_downloaded": 6,
  "total_size": 22,
  "expected_checksum": "`+checksum+`",
  "timestamp": "2026-08-14T00:00:00Z"
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(meta) error = %v", err)
	}

	var sawRange string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawRange = req.Header.Get("Range")
		return response(http.StatusPartialContent, suffix), nil
	})

	d := NewDownloader(DownloaderConfig{
		Store:      &fakeStore{},
		ClientID:   "client-1",
		TransferID: "transfer-1",
		Request: Request{
			ProjectID: 1,
			BaseURL:   "https://example.test/api",
			APIToken:  "token-1",
			LocalPath: localPath,
			Observation: reconcile.Observation{
				RemotePath:  "/file.txt",
				Name:        "file.txt",
				Dir:         "/",
				RemoteEntry: remoteEntry(10, int64(len(full)), checksum),
			},
			UpdatedRecord: filedb.FileRecord{
				Path: "/file.txt",
				Dir:  "/",
				Name: "file.txt",
			},
		},
		ChunkSize:  3,
		HTTPClient: client,
	})

	if err := d.Download(ctx); err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	if sawRange != "bytes=6-" {
		t.Fatalf("Range = %q, want bytes=6-", sawRange)
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, full) {
		t.Fatalf("downloaded body = %q, want %q", got, full)
	}

	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Fatalf("meta file exists after successful download, err=%v", err)
	}
}

func TestDownloaderChecksumMismatchReturnsError(t *testing.T) {
	ctx := context.Background()
	body := []byte("bad checksum body")

	d := NewDownloader(DownloaderConfig{
		Store:      &fakeStore{},
		ClientID:   "client-1",
		TransferID: "transfer-1",
		Request: Request{
			ProjectID: 1,
			BaseURL:   "https://example.test/api",
			APIToken:  "token-1",
			LocalPath: filepath.Join(t.TempDir(), "file.txt"),
			Observation: reconcile.Observation{
				RemotePath:  "/file.txt",
				Name:        "file.txt",
				Dir:         "/",
				RemoteEntry: remoteEntry(10, int64(len(body)), "not-the-md5"),
			},
			UpdatedRecord: filedb.FileRecord{
				Path: "/file.txt",
				Dir:  "/",
				Name: "file.txt",
			},
		},
		HTTPClient: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		}),
	})

	err := d.Download(ctx)
	if err == nil {
		t.Fatal("Download() error = nil, want checksum error")
	}
}

type fakeStore struct {
	record filedb.FileRecord
}

func (s *fakeStore) Upsert(ctx context.Context, record filedb.FileRecord) error {
	s.record = record
	return nil
}

type fakeProgress struct {
	events []transfer.Event
}

func (p *fakeProgress) ReportTransferProgress(event transfer.Event) {
	p.events = append(p.events, event)
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func response(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Header:        make(http.Header),
	}
}

func remoteEntry(id int64, size int64, checksum string) *reconcile.RemoteEntry {
	return &reconcile.RemoteEntry{
		Path:         "/file.txt",
		Name:         "file.txt",
		Dir:          "/",
		Kind:         reconcile.KindFile,
		RemoteFileID: conv.Int64Ptr(id),
		Size:         size,
		Checksum:     checksum,
	}
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}
