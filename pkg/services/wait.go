package services

import (
	"context"
	"fmt"
	"time"

	"github.com/materials-commons/mccli/pkg/di"
)

// WaitForUploads waits for queued upload transfers to complete.
func WaitForUploads(ctx context.Context, manager di.UploadManager, transferIDs []string) error {
	pending := map[string]bool{}
	for _, id := range transferIDs {
		pending[id] = true
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		for id := range pending {
			result, ok := manager.Result(id)
			if !ok {
				continue
			}
			if !result.Success {
				if result.Err != nil {
					return result.Err
				}
				return fmt.Errorf("upload %s failed", id)
			}
			delete(pending, id)
		}

		if len(pending) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}

	return nil
}

// WaitForDownloads waits for queued download transfers to complete.
func WaitForDownloads(ctx context.Context, manager di.DownloadManager, transferIDs []string) error {
	pending := map[string]bool{}
	for _, id := range transferIDs {
		pending[id] = true
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		for id := range pending {
			result, ok := manager.Result(id)
			if !ok {
				continue
			}
			if !result.Success {
				if result.Err != nil {
					return result.Err
				}
				return fmt.Errorf("download %s failed", id)
			}
			delete(pending, id)
		}

		if len(pending) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}

	return nil
}
