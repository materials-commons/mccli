// Package download implements queued HTTP Range file downloads.
package download

import (
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/reconcile"
)

// Request describes one file download request.
type Request struct {
	ProjectID int
	ClientID  string

	// BaseURL is the Materials Commons API base URL.
	BaseURL string

	// APIToken is the Materials Commons API token.
	APIToken string

	// ProjectRoot is used to resolve remote project paths when LocalPath is empty.
	ProjectRoot string

	// LocalPath overrides the destination path. If empty, RemotePath is resolved
	// under ProjectRoot.
	LocalPath string

	Observation   reconcile.Observation
	UpdatedRecord filedb.FileRecord
}
