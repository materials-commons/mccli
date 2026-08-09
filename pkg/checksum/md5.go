// Package checksum provides file checksum utilities used by upload, download,
// scan, and reconciliation code.
package checksum

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const defaultChunkSize = 1024 * 1024

// ProgressFunc is called after each chunk is read.
//
// bytesRead is the cumulative number of bytes read so far.
type ProgressFunc func(bytesRead int64)

// MD5File returns the hex-encoded MD5 checksum for path.
//
// The file is streamed in chunks so large files are not loaded into memory.
func MD5File(ctx context.Context, path string) (string, error) {
	return MD5FileWithProgress(ctx, path, defaultChunkSize, nil)
}

// MD5FileWithProgress returns the hex-encoded MD5 checksum for path and reports
// cumulative progress through progress.
//
// If chunkSize is less than or equal to zero, a default 1 MiB chunk size is
// used.
func MD5FileWithProgress(ctx context.Context, path string, chunkSize int, progress ProgressFunc) (string, error) {
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for checksum %q: %w", path, err)
	}
	defer file.Close()

	hash := md5.New()
	buffer := make([]byte, chunkSize)

	var bytesRead int64
	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("checksum %q cancelled: %w", path, err)
		}

		n, readErr := file.Read(buffer)
		if n > 0 {
			if _, err := hash.Write(buffer[:n]); err != nil {
				return "", fmt.Errorf("hash file %q: %w", path, err)
			}

			bytesRead += int64(n)
			if progress != nil {
				progress(bytesRead)
			}
		}

		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			break
		}

		return "", fmt.Errorf("read file for checksum %q: %w", path, readErr)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
