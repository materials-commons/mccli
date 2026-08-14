package services

import (
	"context"
	"errors"
	"testing"

	"github.com/materials-commons/mccli/pkg/download"
	"github.com/materials-commons/mccli/pkg/upload"
)

func TestWaitForUploadsReturnsImmediatelyForEmptyTransfers(t *testing.T) {
	if err := WaitForUploads(context.Background(), fakeWaitUploadManager{}, nil); err != nil {
		t.Fatalf("WaitForUploads() error = %v", err)
	}
}

func TestWaitForUploadsReturnsResultError(t *testing.T) {
	wantErr := errors.New("upload failed")

	err := WaitForUploads(context.Background(), fakeWaitUploadManager{
		result: upload.Result{
			Success: false,
			Err:     wantErr,
		},
		ok: true,
	}, []string{"upload-1"})

	if !errors.Is(err, wantErr) {
		t.Fatalf("WaitForUploads() error = %v, want %v", err, wantErr)
	}
}

func TestWaitForDownloadsReturnsImmediatelyForEmptyTransfers(t *testing.T) {
	if err := WaitForDownloads(context.Background(), fakeWaitDownloadManager{}, nil); err != nil {
		t.Fatalf("WaitForDownloads() error = %v", err)
	}
}

func TestWaitForDownloadsReturnsResultError(t *testing.T) {
	wantErr := errors.New("download failed")

	err := WaitForDownloads(context.Background(), fakeWaitDownloadManager{
		result: download.Result{
			Success: false,
			Err:     wantErr,
		},
		ok: true,
	}, []string{"download-1"})

	if !errors.Is(err, wantErr) {
		t.Fatalf("WaitForDownloads() error = %v, want %v", err, wantErr)
	}
}

type fakeWaitUploadManager struct {
	fakeUploadManager
	result upload.Result
	ok     bool
}

func (m fakeWaitUploadManager) Result(transferID string) (upload.Result, bool) {
	return m.result, m.ok
}

type fakeWaitDownloadManager struct {
	fakeDownloadManager
	result download.Result
	ok     bool
}

func (m fakeWaitDownloadManager) Result(transferID string) (download.Result, bool) {
	return m.result, m.ok
}
