// Package upload implements queued websocket file uploads.
package upload

import (
	"github.com/materials-commons/mccli/pkg/filedb"
	"github.com/materials-commons/mccli/pkg/reconcile"
)

// Request describes one file upload request.
//
// This is intentionally independent of CLI command code so the uploader can be
// tested with fake websocket queues.
type Request struct {
	ProjectID int

	// ClientID is the Materials Commons client UUID.
	ClientID string

	// Observation contains local path, remote path, and local metadata.
	Observation reconcile.Observation

	// UpdatedRecord is the reconciler's file record with local checksum filled
	// in by upload-mode reconciliation.
	UpdatedRecord filedb.FileRecord
}

// DBWriteRequest is emitted after finalization so DB persistence can be handled
// separately from websocket upload mechanics.
type DBWriteRequest struct {
	Record filedb.FileRecord
}
