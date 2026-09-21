// Package transfer contains shared transfer progress types used by upload and download.
package transfer

import "fmt"

// Direction identifies the transfer direction.
type Direction string

const (
	DirectionUpload   Direction = "upload"
	DirectionDownload Direction = "download"
)

// Status describes the current state of a transfer.
type Status string

const (
	StatusStarting        Status = "starting"
	StatusUploading       Status = "uploading"
	StatusDownloading     Status = "downloading"
	StatusComplete        Status = "complete"
	StatusAlreadyUploaded Status = "already uploaded"
	StatusFailed          Status = "failed"
	StatusCancelled       Status = "cancelled"
)

// Event describes progress for one transfer.
type Event struct {
	TransferID string
	Direction  Direction

	LocalPath  string
	RemotePath string

	BytesDone  int64
	TotalBytes int64

	Status Status
	Err    error
}

// Reporter receives transfer progress events.
//
// Implementations must be safe for concurrent calls from multiple transfer
// goroutines.
type Reporter interface {
	ReportTransferProgress(event Event)
}

// ReporterFunc adapts a function to Reporter.
type ReporterFunc func(event Event)

// ReportTransferProgress reports one transfer progress event.
func (f ReporterFunc) ReportTransferProgress(event Event) {
	f(event)
}

// Percent returns the transfer percentage clamped to [0, 100].
func (e Event) Percent() float64 {
	if e.TotalBytes <= 0 {
		if e.BytesDone > 0 {
			return 100
		}
		return 0
	}

	percent := float64(e.BytesDone) / float64(e.TotalBytes) * 100
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

// DisplayStatus returns a stable human readable status string.
func (e Event) DisplayStatus() string {
	if e.Err == nil {
		return string(e.Status)
	}
	return fmt.Sprintf("%s: %v", e.Status, e.Err)
}
