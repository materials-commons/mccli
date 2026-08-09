package checksum

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMD5File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := MD5File(context.Background(), path)
	if err != nil {
		t.Fatalf("MD5File() error = %v", err)
	}

	const want = "5d41402abc4b2a76b9719d911017c592"
	if got != want {
		t.Fatalf("MD5File() = %q, want %q", got, want)
	}
}

func TestMD5FileEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := MD5File(context.Background(), path)
	if err != nil {
		t.Fatalf("MD5File() error = %v", err)
	}

	const want = "d41d8cd98f00b204e9800998ecf8427e"
	if got != want {
		t.Fatalf("MD5File() = %q, want %q", got, want)
	}
}

func TestMD5FileWithProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var calls int
	var lastBytesRead int64

	_, err := MD5FileWithProgress(context.Background(), path, 2, func(bytesRead int64) {
		calls++
		lastBytesRead = bytesRead
	})
	if err != nil {
		t.Fatalf("MD5FileWithProgress() error = %v", err)
	}

	if calls == 0 {
		t.Fatal("progress callback was not called")
	}
	if lastBytesRead != 6 {
		t.Fatalf("lastBytesRead = %d, want 6", lastBytesRead)
	}
}

func TestMD5FileCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := MD5File(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("MD5File() error = %v, want context.Canceled", err)
	}
}
