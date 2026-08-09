package reconcile

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/materials-commons/mccli/pkg/checksum"
	"github.com/materials-commons/mccli/pkg/filedb"
)

var (
	// ErrInvalidMode indicates that the reconciler mode is unsupported.
	ErrInvalidMode = errors.New("invalid reconcile mode")

	// ErrSyncUnsupported indicates that sync mode has not been implemented yet.
	ErrSyncUnsupported = errors.New("sync reconcile mode is not supported")
)

// ChecksumFunc computes a checksum for a local path.
type ChecksumFunc func(ctx context.Context, localPath string) (string, error)

// Reconciler reconciles one observed project path.
type Reconciler struct {
	mode     Mode
	checksum ChecksumFunc
}

// New creates a Reconciler.
func New(mode Mode) *Reconciler {
	return &Reconciler{
		mode: mode,
		checksum: func(ctx context.Context, localPath string) (string, error) {
			return checksum.MD5File(ctx, localPath)
		},
	}
}

// WithChecksumFunc sets the checksum function used by the reconciler.
//
// This is primarily useful for tests and for future progress-reporting wrappers.
func (r *Reconciler) WithChecksumFunc(fn ChecksumFunc) *Reconciler {
	if fn != nil {
		r.checksum = fn
	}
	return r
}

// Reconcile reconciles one observation.
func (r *Reconciler) Reconcile(ctx context.Context, obs Observation) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}

	state := classify(obs)
	record := recordFromObservation(obs)

	if state == ObservationNeither {
		return skip(record, "no local or remote entry observed"), nil
	}

	if obs.LocalEntry != nil && obs.LocalEntry.IsSymlink {
		return conflict(record, "local path is a symlink"), nil
	}

	if hasKindConflict(obs) {
		return conflict(record, "local and remote entry kinds differ"), nil
	}

	if isDir(obs) {
		return r.reconcileDirectory(obs, record), nil
	}

	if isFile(obs) {
		return r.reconcileRegularFile(ctx, obs, record, state)
	}

	return conflict(record, "entry kind is unknown"), nil
}

func classify(obs Observation) ObservationState {
	if obs.LocalEntry != nil && obs.RemoteEntry != nil {
		return ObservationBoth
	}
	if obs.LocalEntry != nil {
		return ObservationLocalOnly
	}
	if obs.RemoteEntry != nil {
		return ObservationRemoteOnly
	}
	return ObservationNeither
}

func hasKindConflict(obs Observation) bool {
	if obs.LocalEntry == nil || obs.RemoteEntry == nil {
		return false
	}
	if obs.LocalEntry.Kind == KindUnknown || obs.RemoteEntry.Kind == KindUnknown {
		return false
	}
	return obs.LocalEntry.Kind != obs.RemoteEntry.Kind
}

func isDir(obs Observation) bool {
	if obs.LocalEntry != nil && obs.LocalEntry.Kind == KindDir {
		return true
	}
	return obs.RemoteEntry != nil && obs.RemoteEntry.Kind == KindDir
}

func isFile(obs Observation) bool {
	if obs.LocalEntry != nil && obs.LocalEntry.Kind == KindFile {
		return true
	}
	return obs.RemoteEntry != nil && obs.RemoteEntry.Kind == KindFile
}

func recordFromObservation(obs Observation) filedb.FileRecord {
	if obs.FileRecord != nil {
		return *obs.FileRecord
	}

	remotePath := obs.RemotePath
	if remotePath == "" {
		if obs.LocalEntry != nil {
			remotePath = obs.LocalEntry.RemotePath
		} else if obs.RemoteEntry != nil {
			remotePath = obs.RemoteEntry.Path
		}
	}

	name := obs.Name
	if name == "" {
		name = path.Base(remotePath)
	}

	dir := obs.Dir
	if dir == "" {
		dir = path.Dir(remotePath)
		if dir == "." {
			dir = "/"
		}
	}

	return filedb.FileRecord{
		Path:             remotePath,
		Dir:              dir,
		Name:             name,
		IsCleanLocalCopy: false,
	}
}

func (r *Reconciler) reconcileDirectory(obs Observation, record filedb.FileRecord) Decision {
	state := classify(obs)

	switch state {
	case ObservationLocalOnly:
		return skip(recordWithLocal(obs, record), "local directory exists only locally")
	case ObservationRemoteOnly:
		return skip(recordWithRemote(obs, record), "remote directory exists only remotely")
	case ObservationBoth:
		updatedRecord := recordWithLocalAndRemote(obs, record)
		if obs.FileRecord != nil && recordIsStale(obs) {
			return dbUpdate(updatedRecord, "directory metadata changed")
		}
		return skip(updatedRecord, "directory exists locally and remotely")
	default:
		return skip(record, "no directory action needed")
	}
}

func (r *Reconciler) reconcileRegularFile(ctx context.Context, obs Observation, record filedb.FileRecord, state ObservationState) (Decision, error) {
	switch state {
	case ObservationLocalOnly:
		return r.reconcileLocalOnlyFile(ctx, obs, record)
	case ObservationRemoteOnly:
		return r.reconcileRemoteOnlyFile(obs, record)
	case ObservationBoth:
		return r.reconcileBothSidesFile(ctx, obs, record)
	default:
		return skip(record, "no file action needed"), nil
	}
}

func (r *Reconciler) reconcileLocalOnlyFile(ctx context.Context, obs Observation, record filedb.FileRecord) (Decision, error) {
	updatedRecord := recordWithLocal(obs, record)

	switch r.mode {
	case ModeStatus:
		if obs.FileRecord != nil && localEntryMatchesRecord(obs) {
			return upload(updatedRecord, "local only, uploadable - record matches local entry"), nil
		}
		if obs.FileRecord != nil {
			return upload(updatedRecord, "local only, uploadable - update local record"), nil
		}
		return upload(updatedRecord, "local only, uploadable - add local record"), nil

	case ModeUpload:
		if obs.FileRecord != nil && localEntryMatchesRecord(obs) && obs.FileRecord.LocalChecksum != nil {
			return upload(updatedRecord, "local only"), nil
		}

		localChecksum, err := r.localChecksum(ctx, obs)
		if err != nil {
			return Decision{}, err
		}
		updatedRecord.LocalChecksum = &localChecksum

		if obs.FileRecord != nil {
			return upload(updatedRecord, "local only - update local record"), nil
		}
		return upload(updatedRecord, "local only - add local record"), nil

	case ModeDownload:
		updatedRecord = clearRemote(updatedRecord)

		if obs.FileRecord != nil && localEntryMatchesRecord(obs) {
			return skip(updatedRecord, "local entry matches existing record; no remote file to download"), nil
		}

		localChecksum, err := r.localChecksum(ctx, obs)
		if err != nil {
			return Decision{}, err
		}
		updatedRecord.LocalChecksum = &localChecksum

		if obs.FileRecord != nil {
			return dbUpdate(updatedRecord, "local only, uploadable - update local record"), nil
		}
		return dbUpdate(updatedRecord, "local only, uploadable - add local record"), nil

	case ModeSync:
		return Decision{}, ErrSyncUnsupported

	default:
		return Decision{}, fmt.Errorf("%w: %s", ErrInvalidMode, r.mode)
	}
}

func (r *Reconciler) reconcileRemoteOnlyFile(obs Observation, record filedb.FileRecord) (Decision, error) {
	updatedRecord := recordWithRemote(obs, record)

	switch r.mode {
	case ModeStatus:
		if obs.FileRecord != nil {
			return download(updatedRecord, "remote only, downloadable - update local record"), nil
		}
		return download(updatedRecord, "remote only, downloadable - add local record"), nil

	case ModeUpload:
		if obs.FileRecord != nil {
			return dbUpdate(updatedRecord, "remote only, update local record"), nil
		}
		return dbUpdate(updatedRecord, "remote only, add local record"), nil

	case ModeDownload:
		if obs.FileRecord != nil {
			return download(updatedRecord, "remote only - update local record"), nil
		}
		return download(updatedRecord, "remote only - add local record"), nil

	case ModeSync:
		return Decision{}, ErrSyncUnsupported

	default:
		return Decision{}, fmt.Errorf("%w: %s", ErrInvalidMode, r.mode)
	}
}

func (r *Reconciler) reconcileBothSidesFile(ctx context.Context, obs Observation, record filedb.FileRecord) (Decision, error) {
	updatedRecord := recordWithLocalAndRemote(obs, record)

	switch r.mode {
	case ModeStatus:
		return r.reconcileBothStatus(obs, updatedRecord), nil
	case ModeUpload:
		return r.reconcileBothUpload(ctx, obs, updatedRecord)
	case ModeDownload:
		return r.reconcileBothDownload(ctx, obs, updatedRecord)
	case ModeSync:
		return Decision{}, ErrSyncUnsupported
	default:
		return Decision{}, fmt.Errorf("%w: %s", ErrInvalidMode, r.mode)
	}
}

func (r *Reconciler) reconcileBothStatus(obs Observation, updatedRecord filedb.FileRecord) Decision {
	if obs.FileRecord == nil {
		return skip(updatedRecord, "status unknown; run scan to reconcile with checksum")
	}

	if localEntryMatchesRecord(obs) {
		if remoteIDMatchesRecord(obs) {
			return skip(updatedRecord, "local file matches remote")
		}
		return download(updatedRecord, "local file differs from remote, previous version uploaded")
	}

	if remoteIDMatchesRecord(obs) {
		return upload(updatedRecord, "local file changed, previous file uploaded")
	}

	return skip(updatedRecord, "status unknown; run scan to reconcile with checksum")
}

func (r *Reconciler) reconcileBothUpload(ctx context.Context, obs Observation, updatedRecord filedb.FileRecord) (Decision, error) {
	if obs.FileRecord != nil {
		if localEntryMatchesRecord(obs) {
			if remoteIDMatchesRecord(obs) {
				return skip(updatedRecord, "local matches remote"), nil
			}
			return skip(updatedRecord, "local matches remote, but remote file differs (downloadable)"), nil
		}

		localChecksum, err := r.localChecksum(ctx, obs)
		if err != nil {
			return Decision{}, err
		}
		updatedRecord.LocalChecksum = &localChecksum
		return upload(updatedRecord, "local file changed"), nil
	}

	localChecksum, err := r.localChecksum(ctx, obs)
	if err != nil {
		return Decision{}, err
	}

	if obs.RemoteEntry != nil && localChecksum == obs.RemoteEntry.Checksum {
		updatedRecord.LocalChecksum = &localChecksum
		return dbUpdate(updatedRecord, "local matches remote, add file record"), nil
	}

	updatedRecord.LocalChecksum = &localChecksum
	return upload(updatedRecord, "local file changed"), nil
}

func (r *Reconciler) reconcileBothDownload(ctx context.Context, obs Observation, updatedRecord filedb.FileRecord) (Decision, error) {
	if obs.FileRecord == nil {
		localChecksum, err := r.localChecksum(ctx, obs)
		if err != nil {
			return Decision{}, err
		}
		updatedRecord.LocalChecksum = &localChecksum

		if obs.RemoteEntry != nil && localChecksum == obs.RemoteEntry.Checksum {
			return dbUpdate(updatedRecord, "local file matches remote checksum"), nil
		}

		// Previous-version lookup needs the gomcapi integration. Until that is
		// wired in, be conservative.
		return conflict(updatedRecord, "local file changed, remote changed, previous version check unavailable"), nil
	}

	if localEntryMatchesRecord(obs) {
		if remoteIDMatchesRecord(obs) {
			return skip(updatedRecord, "local and remote versions match"), nil
		}
		return download(updatedRecord, "local and remote versions differ"), nil
	}

	localChecksum, err := r.localChecksum(ctx, obs)
	if err != nil {
		return Decision{}, err
	}
	updatedRecord.LocalChecksum = &localChecksum

	if obs.FileRecord.LocalChecksum != nil && localChecksum == *obs.FileRecord.LocalChecksum {
		return download(updatedRecord, "local and remote versions differ"), nil
	}

	// Previous-version lookup needs the gomcapi integration. Until that is
	// wired in, be conservative.
	return conflict(updatedRecord, "local file has changed and previous version check is unavailable"), nil
}

func (r *Reconciler) localChecksum(ctx context.Context, obs Observation) (string, error) {
	if obs.LocalEntry == nil {
		return "", fmt.Errorf("cannot checksum missing local entry for %q", obs.RemotePath)
	}
	return r.checksum(ctx, obs.LocalEntry.Path)
}

func recordWithLocal(obs Observation, record filedb.FileRecord) filedb.FileRecord {
	if obs.LocalEntry == nil {
		return record
	}

	record.LocalSize = obs.LocalEntry.Size
	record.LocalMTimeNS = obs.LocalEntry.MTimeNS
	record.LocalCTimeNS = obs.LocalEntry.CTimeNS
	record.LocalLastSeenTS = obs.LocalEntry.LastSeenTS

	return record
}

func recordWithRemote(obs Observation, record filedb.FileRecord) filedb.FileRecord {
	if obs.RemoteEntry == nil {
		return record
	}

	record.RemoteFileID = &obs.RemoteEntry.RemoteFileID
	record.RemoteSize = &obs.RemoteEntry.Size
	record.RemoteCTimeNS = &obs.RemoteEntry.CTimeNS
	record.RemoteChecksum = stringPtr(obs.RemoteEntry.Checksum)

	return record
}

func recordWithLocalAndRemote(obs Observation, record filedb.FileRecord) filedb.FileRecord {
	return recordWithRemote(obs, recordWithLocal(obs, record))
}

func clearRemote(record filedb.FileRecord) filedb.FileRecord {
	record.RemoteFileID = nil
	record.RemoteSize = nil
	record.RemoteCTimeNS = nil
	record.RemoteChecksum = nil
	record.RemoteLastSeenTS = nil
	record.Status = nil
	record.Origin = nil
	record.TransferID = nil
	return record
}

func localEntryMatchesRecord(obs Observation) bool {
	if obs.LocalEntry == nil || obs.FileRecord == nil {
		return false
	}

	return obs.LocalEntry.Size == obs.FileRecord.LocalSize &&
		obs.LocalEntry.MTimeNS == obs.FileRecord.LocalMTimeNS &&
		obs.LocalEntry.CTimeNS == obs.FileRecord.LocalCTimeNS
}

func remoteIDMatchesRecord(obs Observation) bool {
	if obs.RemoteEntry == nil || obs.FileRecord == nil || obs.FileRecord.RemoteFileID == nil {
		return false
	}
	return obs.RemoteEntry.RemoteFileID == *obs.FileRecord.RemoteFileID
}

func recordIsStale(obs Observation) bool {
	if obs.FileRecord == nil {
		return false
	}
	if obs.LocalEntry != nil && !localEntryMatchesRecord(obs) {
		return true
	}
	if obs.RemoteEntry != nil && !remoteIDMatchesRecord(obs) {
		return true
	}
	return false
}

func decision(action Action, record filedb.FileRecord, reason string, updated bool) Decision {
	return Decision{
		Action:        action,
		Reason:        reason,
		Updated:       updated,
		UpdatedRecord: record,
	}
}

func skip(record filedb.FileRecord, reason string) Decision {
	return decision(ActionSkip, record, reason, false)
}

func upload(record filedb.FileRecord, reason string) Decision {
	return decision(ActionUpload, record, reason, true)
}

func download(record filedb.FileRecord, reason string) Decision {
	return decision(ActionDownload, record, reason, true)
}

func conflict(record filedb.FileRecord, reason string) Decision {
	return decision(ActionConflict, record, reason, false)
}

func dbUpdate(record filedb.FileRecord, reason string) Decision {
	return decision(ActionDBUpdate, record, reason, true)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
