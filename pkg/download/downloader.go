package download

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"github.com/materials-commons/mccli/pkg/transfer"
	"github.com/materials-commons/mccli/pkg/wsclient"
)

var (
	ErrInvalidDownloadRequest = errors.New("invalid download request")
	ErrDownloadCancelled      = errors.New("download cancelled")
	ErrChecksumMismatch       = errors.New("download checksum mismatch")
)

const defaultDownloadChunkSize int64 = 1024 * 1024

// FileRecordStore persists downloaded file state.
type FileRecordStore interface {
	Upsert(ctx context.Context, record filedb.FileRecord) error
}

// HTTPClient is the subset of http.Client used by Downloader.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config configures one Downloader.
type DownloaderConfig struct {
	SendQueue *wsclient.Queue[wsclient.OutboundMessage]
	Store     FileRecordStore
	ClientID  string

	Request Request

	TransferID string
	ChunkSize  int64
	HTTPClient HTTPClient
	Progress   transfer.Reporter

	Now func() time.Time
}

// Downloader downloads one file via HTTP Range requests.
type Downloader struct {
	SendQueue *wsclient.Queue[wsclient.OutboundMessage]
	Store     FileRecordStore
	ClientID  string

	Request    Request
	TransferID string
	ChunkSize  int64
	HTTPClient HTTPClient
	Progress   transfer.Reporter
	Now        func() time.Time

	mu            sync.Mutex
	bytesReceived int64
	expectedSize  int64
	paused        bool
	cancelled     bool
}

// NewDownloader creates a Downloader.
func NewDownloader(cfg DownloaderConfig) *Downloader {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = defaultDownloadChunkSize
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	return &Downloader{
		SendQueue:  cfg.SendQueue,
		Store:      cfg.Store,
		ClientID:   cfg.ClientID,
		Request:    cfg.Request,
		TransferID: cfg.TransferID,
		ChunkSize:  cfg.ChunkSize,
		HTTPClient: cfg.HTTPClient,
		Progress:   cfg.Progress,
		Now:        cfg.Now,
	}
}

// TransferIDValue returns this downloader's transfer ID.
func (d *Downloader) TransferIDValue() string {
	return d.TransferID
}

// SetTransferID sets this downloader's transfer ID.
func (d *Downloader) SetTransferID(id string) {
	d.TransferID = id
}

// BytesReceived returns the current received byte count.
func (d *Downloader) BytesReceived() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.bytesReceived
}

// ExpectedSize returns the current expected total byte count.
func (d *Downloader) ExpectedSize() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.expectedSize
}

// LocalPath returns the resolved destination path.
func (d *Downloader) LocalPath() (string, error) {
	if d.Request.LocalPath != "" {
		return filepath.Abs(d.Request.LocalPath)
	}

	if d.Request.ProjectRoot == "" {
		return "", fmt.Errorf("%w: project root is required when local path is empty", ErrInvalidDownloadRequest)
	}
	if d.Request.Observation.RemotePath == "" {
		return "", fmt.Errorf("%w: remote path is required", ErrInvalidDownloadRequest)
	}

	return projectpath.RemoteToLocal(d.Request.ProjectRoot, d.Request.Observation.RemotePath)
}

// Download downloads the file and updates the file record on success.
func (d *Downloader) Download(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.validate(); err != nil {
		return err
	}

	localPath, err := d.LocalPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("create download directory %q: %w", filepath.Dir(localPath), err)
	}

	partPath := localPath + ".part"
	metaPath := localPath + ".meta.json"

	resumeFrom, err := d.resumeOffset(partPath, metaPath)
	if err != nil {
		return err
	}

	d.setBytesReceived(resumeFrom)
	d.reportProgress(transfer.StatusStarting, nil)

	if err := d.downloadWithRange(ctx, localPath, partPath, metaPath, resumeFrom); err != nil {
		_ = d.saveMetadata(metaPath, localPath)
		d.reportProgress(transfer.StatusFailed, err)
		_ = d.sendCompletion(false, err)
		return err
	}

	if checksum := d.expectedChecksum(); checksum != "" {
		if err := verifyMD5(partPath, checksum); err != nil {
			_ = d.saveMetadata(metaPath, localPath)
			d.reportProgress(transfer.StatusFailed, err)
			_ = d.sendCompletion(false, err)
			return err
		}
	}

	if err := os.Rename(partPath, localPath); err != nil {
		return fmt.Errorf("finalize download %q: %w", localPath, err)
	}

	_ = os.Remove(metaPath)

	if err := d.upsertRecord(ctx, localPath); err != nil {
		return err
	}

	d.reportProgress(transfer.StatusComplete, nil)
	return d.sendCompletion(true, nil)
}

func (d *Downloader) validate() error {
	if d.Store == nil {
		return fmt.Errorf("%w: file record store is required", ErrInvalidDownloadRequest)
	}
	if d.HTTPClient == nil {
		return fmt.Errorf("%w: http client is required", ErrInvalidDownloadRequest)
	}
	if d.ClientID == "" {
		return fmt.Errorf("%w: client id is required", ErrInvalidDownloadRequest)
	}
	if d.TransferID == "" {
		return fmt.Errorf("%w: transfer id is required", ErrInvalidDownloadRequest)
	}
	if d.Request.ProjectID <= 0 {
		return fmt.Errorf("%w: project id must be positive", ErrInvalidDownloadRequest)
	}
	if d.Request.BaseURL == "" {
		return fmt.Errorf("%w: base url is required", ErrInvalidDownloadRequest)
	}
	if d.Request.APIToken == "" {
		return fmt.Errorf("%w: api token is required", ErrInvalidDownloadRequest)
	}
	if d.remoteFileID() <= 0 {
		return fmt.Errorf("%w: remote file id is required", ErrInvalidDownloadRequest)
	}
	if d.ChunkSize <= 0 {
		return fmt.Errorf("%w: chunk size must be positive", ErrInvalidDownloadRequest)
	}
	return nil
}

func (d *Downloader) downloadWithRange(ctx context.Context, localPath, partPath, metaPath string, resumeFrom int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.downloadURL(), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+d.Request.APIToken)
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resumeFrom > 0 && resp.StatusCode != http.StatusPartialContent {
		resumeFrom = 0
		d.setBytesReceived(0)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("download failed with HTTP status %d", resp.StatusCode)
	}

	if resp.ContentLength >= 0 {
		total := resp.ContentLength
		if resumeFrom > 0 {
			total += resumeFrom
		}
		d.setExpectedSize(total)
	} else if size := d.expectedRemoteSize(); size > 0 {
		d.setExpectedSize(size)
	}

	mode := os.O_CREATE | os.O_WRONLY
	if resumeFrom > 0 && resp.StatusCode == http.StatusPartialContent {
		mode |= os.O_APPEND
	} else {
		mode |= os.O_TRUNC
	}

	file, err := os.OpenFile(partPath, mode, 0o644)
	if err != nil {
		return fmt.Errorf("open partial download %q: %w", partPath, err)
	}
	defer file.Close()

	d.reportProgress(transfer.StatusDownloading, nil)

	buf := make([]byte, d.ChunkSize)
	lastSave := d.BytesReceived()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		for d.isPaused() && !d.isCancelled() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}

		if d.isCancelled() {
			return ErrDownloadCancelled
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := file.Write(buf[:n]); err != nil {
				return fmt.Errorf("write partial download %q: %w", partPath, err)
			}

			current := d.addBytesReceived(int64(n))
			d.reportProgress(transfer.StatusDownloading, nil)

			if current-lastSave >= d.ChunkSize*10 {
				if err := d.saveMetadata(metaPath, localPath); err != nil {
					return err
				}
				lastSave = current
			}
		}

		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read download response: %w", readErr)
		}
	}
}

type resumeMetadata struct {
	TransferID       string `json:"transfer_id"`
	FileID           int64  `json:"file_id"`
	FilePath         string `json:"file_path"`
	DownloadURL      string `json:"download_url"`
	BytesDownloaded  int64  `json:"bytes_downloaded"`
	TotalSize        int64  `json:"total_size"`
	ExpectedChecksum string `json:"expected_checksum"`
	Timestamp        string `json:"timestamp"`
}

func (d *Downloader) resumeOffset(partPath, metaPath string) (int64, error) {
	partInfo, err := os.Stat(partPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}

	data, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}

	var metadata resumeMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return 0, nil
	}

	if metadata.FileID != d.remoteFileID() {
		return 0, nil
	}
	if metadata.TransferID != d.TransferID {
		return 0, nil
	}
	if metadata.BytesDownloaded != partInfo.Size() {
		return 0, nil
	}

	d.setExpectedSize(metadata.TotalSize)
	return metadata.BytesDownloaded, nil
}

func (d *Downloader) saveMetadata(metaPath, localPath string) error {
	metadata := resumeMetadata{
		TransferID:       d.TransferID,
		FileID:           d.remoteFileID(),
		FilePath:         localPath,
		DownloadURL:      d.downloadURL(),
		BytesDownloaded:  d.BytesReceived(),
		TotalSize:        d.ExpectedSize(),
		ExpectedChecksum: d.expectedChecksum(),
		Timestamp:        d.Now().UTC().Format(time.RFC3339Nano),
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(metaPath, data, 0o644)
}

func (d *Downloader) upsertRecord(ctx context.Context, localPath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}

	now := d.Now().Unix()
	record := d.Request.UpdatedRecord
	record.RemoteChecksum = stringPtrWithNil(d.expectedChecksum())
	record.LocalChecksum = stringPtrWithNil(d.expectedChecksum())
	record.LocalSize = info.Size()
	record.LocalMTimeNS = info.ModTime().UnixNano()
	record.LocalCTimeNS = info.ModTime().UnixNano()
	record.LocalLastSeenTS = now
	record.RemoteSize = int64PtrWithZeroNil(d.expectedRemoteSize())
	record.RemoteFileID = int64PtrWithZeroNil(d.remoteFileID())
	record.RemoteLastSeenTS = &now

	if record.Path == "" {
		record.Path = d.Request.Observation.RemotePath
	}
	if record.Dir == "" {
		record.Dir = filepath.ToSlash(filepath.Dir(record.Path))
	}
	if record.Name == "" {
		record.Name = filepath.Base(record.Path)
	}

	if err := d.Store.Upsert(ctx, record); err != nil {
		return fmt.Errorf("upsert downloaded file record %q: %w", record.Path, err)
	}

	return nil
}

func (d *Downloader) sendCompletion(success bool, completionErr error) error {
	if d.SendQueue == nil {
		return nil
	}

	localPath, _ := d.LocalPath()

	payload := map[string]any{
		"transfer_id":    d.TransferID,
		"file_id":        d.remoteFileID(),
		"file_path":      localPath,
		"bytes_received": d.BytesReceived(),
		"success":        success,
	}
	if completionErr != nil {
		payload["error"] = completionErr.Error()
	}

	msg := wsclient.TextMessage{
		"command":   "DOWNLOAD_COMPLETE",
		"id":        uuid.NewString(),
		"timestamp": d.Now().UTC().Format(time.RFC3339Nano),
		"clientId":  d.ClientID,
		"payload":   payload,
	}
	if !success {
		msg["command"] = "DOWNLOAD_FAILED"
	}

	if ok := d.SendQueue.Push(msg); !ok {
		return fmt.Errorf("send completion message: queue is closed")
	}

	return nil
}

// Pause pauses this download.
func (d *Downloader) Pause() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused = true
}

// Resume resumes this download.
func (d *Downloader) Resume() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused = false
}

// Cancel cancels this download.
func (d *Downloader) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancelled = true
}

func (d *Downloader) reportProgress(status transfer.Status, err error) {
	if d.Progress == nil {
		return
	}

	localPath, _ := d.LocalPath()

	d.Progress.ReportTransferProgress(transfer.Event{
		TransferID: d.TransferID,
		Direction:  transfer.DirectionDownload,
		LocalPath:  localPath,
		RemotePath: d.Request.Observation.RemotePath,
		BytesDone:  d.BytesReceived(),
		TotalBytes: d.ExpectedSize(),
		Status:     status,
		Err:        err,
	})
}

func (d *Downloader) downloadURL() string {
	return fmt.Sprintf("%s/projects/%d/files/%d/download", d.Request.BaseURL, d.Request.ProjectID, d.remoteFileID())
}

func (d *Downloader) remoteFileID() int64 {
	if d.Request.Observation.RemoteEntry == nil || d.Request.Observation.RemoteEntry.RemoteFileID == nil {
		return 0
	}
	return *d.Request.Observation.RemoteEntry.RemoteFileID
}

func (d *Downloader) expectedRemoteSize() int64 {
	if d.Request.Observation.RemoteEntry == nil {
		return 0
	}
	return d.Request.Observation.RemoteEntry.Size
}

func (d *Downloader) expectedChecksum() string {
	if d.Request.Observation.RemoteEntry == nil {
		return ""
	}
	return d.Request.Observation.RemoteEntry.Checksum
}

func (d *Downloader) setBytesReceived(value int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bytesReceived = value
}

func (d *Downloader) addBytesReceived(value int64) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bytesReceived += value
	return d.bytesReceived
}

func (d *Downloader) setExpectedSize(value int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expectedSize = value
}

func (d *Downloader) isPaused() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.paused
}

func (d *Downloader) isCancelled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cancelled
}

func verifyMD5(filePath, expected string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}

	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("%w: expected %s got %s", ErrChecksumMismatch, expected, actual)
	}

	return nil
}

func stringPtrWithNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func int64PtrWithZeroNil(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}
