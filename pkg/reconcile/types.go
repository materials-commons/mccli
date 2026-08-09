// Package reconcile determines what action mc2 should take for local and
// remote project entries.
package reconcile

import "github.com/materials-commons/mccli/pkg/filedb"

// Kind describes whether an observed entry is a regular file or directory.
type Kind string

const (
	KindUnknown Kind = "unknown"
	KindFile    Kind = "file"
	KindDir     Kind = "directory"
)

// Action is the action selected by the reconciler.
type Action string

const (
	ActionSkip     Action = "skip"
	ActionUpload   Action = "upload"
	ActionDownload Action = "download"
	ActionConflict Action = "conflict"
	ActionDBUpdate Action = "db_update"
)

// Mode controls reconciliation policy.
type Mode string

const (
	ModeUpload   Mode = "upload"
	ModeDownload Mode = "download"
	ModeStatus   Mode = "status"
	ModeSync     Mode = "sync"
)

// ObservationState classifies where an entry exists.
type ObservationState string

const (
	ObservationNeither    ObservationState = "neither"
	ObservationLocalOnly  ObservationState = "local_only"
	ObservationRemoteOnly ObservationState = "remote_only"
	ObservationBoth       ObservationState = "both"
)

// LocalEntry is the local filesystem observation for a project path.
type LocalEntry struct {
	Path       string
	RemotePath string
	Name       string
	Dir        string
	Kind       Kind
	IsSymlink  bool

	Size       int64
	MTimeNS    int64
	CTimeNS    int64
	LastSeenTS int64
}

// RemoteEntry is the remote Materials Commons observation for a project path.
//
// This is intentionally independent of gomcapi's concrete model so the
// reconciler can be unit-tested without network calls.
type RemoteEntry struct {
	Path string
	Name string
	Dir  string
	Kind Kind

	RemoteFileID int64
	Size         int64
	CTimeNS      int64
	MTimeNS      int64
	Checksum     string
}

// Observation combines local state, remote state, and the existing file
// database record for one project path.
type Observation struct {
	RemotePath string
	Name       string
	Dir        string

	FileRecord  *filedb.FileRecord
	LocalEntry  *LocalEntry
	RemoteEntry *RemoteEntry
}

// Decision is the reconciler result for one project path.
type Decision struct {
	Action        Action
	Reason        string
	Updated       bool
	UpdatedRecord filedb.FileRecord
}
