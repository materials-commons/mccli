from __future__ import annotations

import asyncio
from dataclasses import replace
from pathlib import Path
from typing import Literal, Callable, Awaitable, Optional

from materials_commons.cli.models import FileRecord, WalkObservation, FileDecision
from materials_commons.cli.old.functions import checksum

EntryKind = Literal["file", "dir"]
Action = Literal["skip", "upload", "download", "conflict", "adopt", "db_update"]
ObservationState = Literal["neither", "local_only", "remote_only", "both"]
ChecksumProvider = Callable[[Path], Awaitable[str]]
ReconcileMode = Literal["upload", "download", "sync", "status"]


class SingleFileReconciler:
    """
    Reconciles one WalkObservation.

    Default policy:
    - local-only file: upload
    - remote-only file: download
    - directories: structural; skip unless metadata refresh is useful
    - local/remote kind mismatch: conflict
    - symlink: conflict
    - both files changed from the record: conflict
    """

    def __init__(self, *, mode: ReconcileMode, compute_checksum: bool = False, reuse_checksum_requires_ctime_match: bool = False):
        self._mode: ReconcileMode = mode
        self._compute_checksum = compute_checksum
        self._reuse_checksum_requires_ctime_match = reuse_checksum_requires_ctime_match

    async def reconcile_file(self, observation: WalkObservation) -> FileDecision:
        state = self._classify_observation(observation)
        record = self._record_from_observation(observation)

        if state == "neither":
            return self._skip(record, "no local or remote entry observed")

        if observation.local_is_symlink():
            return self._conflict(record, "local path is a symlink")

        if observation.has_kind_conflict():
            return self._conflict(record, "local and remote entry kinds differ")

        if observation.is_dir:
            return await self._reconcile_directory(observation, record)

        if observation.is_file:
            return await self._reconcile_regular_file(observation, record, state)

        return self._conflict(record, "entry kind is unknown")

    def _classify_observation(self, observation: WalkObservation) -> ObservationState:
        if observation.has_local() and observation.has_remote():
            return "both"
        if observation.has_local():
            return "local_only"
        if observation.has_remote():
            return "remote_only"
        return "neither"

    def _record_from_observation(self, observation: WalkObservation) -> FileRecord:
        if observation.file_record is not None:
            return observation.file_record

        return FileRecord(
            path=str(observation.path),
            name=observation.name,
            dir=str(observation.path.parent),
            is_clean_local_copy=0,
        )

    async def _reconcile_directory(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        state = self._classify_observation(observation)

        if state == "local_only":
            updated_record = self.record_with_local(observation, record)
            return self._skip(updated_record, "local directory exists only locally")

        if state == "remote_only":
            updated_record = self._record_with_remote(observation, record)
            return self._skip(updated_record, "remote directory exists only remotely")

        if state == "both":
            updated_record = self._record_with_local_and_remote(observation, record)

            if observation.has_record() and observation.record_is_stale():
                return self._db_update(updated_record, "directory metadata changed")

            return self._skip(updated_record, "directory exists locally and remotely")

        return self._skip(record, "no directory action needed")

    async def _reconcile_regular_file(self,
                                      observation: WalkObservation,
                                      record: FileRecord,
                                      state: ObservationState) -> FileDecision:
        if state == "local_only":
            return await self._reconcile_local_only_file(observation, record)

        if state == "remote_only":
            return await self._reconcile_remote_only_file(observation, record)

        if state == "both":
            return await self._reconcile_file_present_on_both_sides(observation, record)

        return self._skip(record, "no file action needed")

    async def _reconcile_local_only_file(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        updated_record = self.record_with_local(observation, record)
        if self._mode == "status":
            return self._reconcile_local_only_status(observation, updated_record)
        elif self._mode == "upload":
            return await self._reconcile_local_only_upload(observation, updated_record)
        elif self._mode == "download":
            return await self._reconcile_local_only_download(observation, updated_record)
        elif self._mode == "sync":
            raise ValueError(f"Mode sync not currently supported")
        else:
            raise ValueError(f"Invalid mode: {self._mode}")

    def _reconcile_local_only_status(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            if observation.local_entry_matches_record():
                reason = "local only, uploadable - record matches local entry"
            else:
                reason = "local only, uploadable - update local record"
        else:
            reason = "local only, uploadable - add local record"
        return self._upload(record, reason)

    async def _reconcile_local_only_upload(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            if observation.local_entry_matches_record():
                return self._upload(record, "local only")
            else:
                local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
                record = replace(record, local_checksum=local_checksum)
                reason = "local only - update local record"
                return self._upload(record, reason)
        else:
            local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
            record = replace(record, local_checksum=local_checksum)
            reason = "local only - add local record"
            return self._upload(record, reason)

    async def _reconcile_local_only_download(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        record.clear_remote()
        if observation.file_record:
            if observation.local_entry_matches_record():
                return self._skip(record, "local entry matches remote record")
            else:
                local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
                record = replace(record, local_checksum=local_checksum)
                reason = "local only, uploadable - update local record"
                return self._db_update(record, reason)
        else:
            # No record in the project database
            local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
            record = replace(record, local_checksum=local_checksum)
            reason = "local only, uploadable - add local record"
            return self._db_update(record, reason)

    async def _reconcile_remote_only_file(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        updated_record = self._record_with_remote(observation, record)
        if self._mode == "status":
            return self._reconcile_remote_only_status(observation, updated_record)
        elif self._mode == "upload":
            return self._reconcile_remote_only_upload(observation, updated_record)
        elif self._mode == "download":
            return self._reconcile_remote_only_download(observation, updated_record)
        elif self._mode == "sync":
            raise ValueError(f"Mode sync not currently supported")
        else:
            raise ValueError(f"Invalid mode: {self._mode}")

    def _reconcile_remote_only_status(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            # We've seen a previous version of this file
            reason = "remote only, downloadable - update local record"
        else:
            reason = "remote only, downloadable - add local record"
        return self._download(record, reason)

    def _reconcile_remote_only_upload(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            reason = "remote only, update local record"
        else:
            reason = "remote only, add local record"
        return self._db_update(record, reason)

    def _reconcile_remote_only_download(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            reason = "remote only - update local record"
        else:
            reason = "remote only - add local record"
        return self._download(record, reason)

    async def _reconcile_file_present_on_both_sides(
            self,
            observation: WalkObservation,
            record: FileRecord,
    ) -> FileDecision:
        print("_reconcile_file_present_on_both_sides")
        updated_record = self._record_with_local_and_remote(observation, record)

        # Handle a file that we previously saw and recorded locally
        if observation.file_record is not None:
            return await self._reconcile_previously_seen_file(observation, updated_record)

        # If we are here, then we are handling a file that has not been previously recorded
        # in the local project database. So all decisions need to be based on the local
        # file we have stat() info on, and the remote file.

        if self._local_and_remote_metadata_match(observation):
            local_checksum = await self._get_local_checksum(observation)
            if local_checksum is not None:
                updated_record = replace(updated_record, local_checksum=local_checksum)

            if observation.has_record() and observation.record_is_stale():
                return self._db_update(
                    updated_record,
                    "local and remote metadata match each other; record metadata is stale",
                )

            return self._skip(updated_record, "local and remote file metadata match")

        checksum_decision = await self._try_reconcile_by_checksum(observation, updated_record)
        if checksum_decision is not None:
            return checksum_decision

        if not observation.has_record():
            return self._conflict(
                updated_record,
                "local and remote files both exist but no record is available",
            )

        local_matches_record = observation.local_entry_matches_record()
        remote_matches_record = observation.remote_entry_matches_record()

        local_changed = not local_matches_record
        remote_changed = not remote_matches_record

        if local_matches_record and remote_matches_record:
            return self._skip(updated_record, "local and remote both match record")

        if local_changed and not remote_changed:
            return self._upload(updated_record, "local file changed; remote still matches record")

        if remote_changed and not local_changed:
            return self._download(updated_record, "remote file changed; local still matches record")

        if local_changed and remote_changed:
            return self._conflict(updated_record, "local and remote files both changed")

        return self._conflict(updated_record, "unable to determine safe file reconciliation action")

    async def _reconcile_previously_seen_file(self, observation: WalkObservation, updated_record: FileRecord) -> FileDecision:
        """
        Handle the case where the file has been seen before and the remote version matches the last known version.
        """
        if observation.file_record.remote_file_id == observation.remote_entry.remote_file_id:
            # The last recorded version is the same as the version on the server. Let's see
            # what we need to do. First let's check if the file_record and the local_entry
            # are the same.
            if observation.file_record.local_size == observation.local_entry.size and \
                    observation.file_record.local_ctime_ns == observation.local_entry.ctime_ns:
                # File hasn't changed, so we can skip it.
                return self._skip(updated_record, "local and remote files match")
            else:
                # File has changed. First check if size is different, if it is then we can upload.
                if observation.file_record.local_size != observation.local_entry.size:
                    return self._upload(updated_record, "local file changed")
                else:
                    # Sizes match, so we need to compare checksums
                    local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
                    if local_checksum == observation.file_record.local_checksum:
                        # Sizes match, checksums match. The only thing that changed is the mtime.
                        updated_record = replace(updated_record, local_mtime_ns=observation.local_entry.mtime_ns)
                        return self._skip(updated_record, "local and remote files match")
                    else:
                        # Checksums are different
                        updated_record = replace(updated_record, local_mtime_ns=observation.local_entry.mtime_ns,
                                                 local_checksum=local_checksum)
                        return self._upload(updated_record, "local and remote checksums differ")
        else:
            # Last recorded remote_file_id is different than the current remote entry
            # TODO: Start working through the logic from here.
            updated_record = replace(updated_record, remote_file_id=observation.remote_entry.file_id)
            return self._upload(updated_record, "remote file ID changed, uploading local file")

    def record_with_local(self, observation: WalkObservation, record: FileRecord) -> FileRecord:
        if observation.local_entry is None:
            return record

        return replace(
            record,
            local_size=observation.local_entry.size,
            local_mtime_ns=observation.local_entry.mtime_ns,
            local_ctime_ns=observation.local_entry.ctime_ns,
        )

    def _record_with_remote(self, observation: WalkObservation, record: FileRecord) -> FileRecord:
        if observation.remote_entry is None:
            return record

        return replace(
            record,
            remote_file_id=observation.remote_entry.remote_file_id,
            remote_size=observation.remote_entry.size,
            remote_ctime_ns=observation.remote_entry.ctime_ns,
            remote_checksum=observation.remote_entry.checksum,
        )

    def _record_with_local_and_remote(self, observation: WalkObservation, record: FileRecord) -> FileRecord:
        return self._record_with_remote(observation, self.record_with_local(observation, record))

    def _local_and_remote_metadata_match(self, observation: WalkObservation) -> bool:
        if observation.local_entry is None or observation.remote_entry is None:
            return False

        print(f"{observation.local_entry.size} == {observation.remote_entry.size}")
        print(f"{observation.local_entry.ctime_ns} == {observation.remote_entry.ctime_ns}")
        return (
                observation.local_entry.size == observation.remote_entry.size
                and observation.local_entry.ctime_ns == observation.remote_entry.ctime_ns
        )

    def _remote_checksum_matches_local_record(self, observation: WalkObservation) -> bool:
        if observation.remote_entry is None or observation.file_record is None:
            return False

        return (
                observation.remote_entry.checksum is not None
                and observation.file_record.local_checksum is not None
                and observation.remote_entry.checksum == observation.file_record.local_checksum
        )

    def decision(self, action: Action, record: FileRecord, reason: str, updated: bool) -> FileDecision:
        return FileDecision(
            action=action,
            reason=reason,
            updated=updated,
            updated_record=record,
        )

    def _skip(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("skip", record, reason, updated=False)

    def _upload(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("upload", record, reason, updated=True)

    def _download(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("download", record, reason, updated=True)

    def _conflict(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("conflict", record, reason, updated=False)

    def _adopt(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("adopt", record, reason, updated=True)

    def _db_update(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("db_update", record, reason, updated=True)

    async def _compute_local_checksum(self, observation: WalkObservation) -> Optional[str]:
        if not self._compute_checksum:
            return None
        if observation.local_entry is None:
            return None

        return await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())

    async def _get_local_checksum(self, observation: WalkObservation) -> Optional[str]:
        csum = self._get_file_record_checksum_if_file_unchanged(observation)
        if csum is not None:
            return csum
        return await self._compute_local_checksum(observation)

    def _get_file_record_checksum_if_file_unchanged(self, observation: WalkObservation) -> Optional[str]:
        if observation.local_entry is None:
            return None

        if observation.file_record is None:
            return None

        if observation.file_record.local_checksum is None:
            return None

        if not observation.local_entry.is_file:
            return None

        if not self.local_metadata_allows_checksum_reuse(observation):
            return None

        return observation.file_record.local_checksum

    def local_metadata_allows_checksum_reuse(self, observation: WalkObservation) -> bool:
        if observation.local_entry is None or observation.file_record is None:
            return False

        size_matches = observation.local_entry.size == observation.file_record.local_size
        mtime_matches = observation.local_entry.mtime_ns == observation.file_record.local_mtime_ns

        if not size_matches or not mtime_matches:
            return False

        if self._reuse_checksum_requires_ctime_match:
            return observation.local_entry.ctime_ns == observation.file_record.local_ctime_ns

        return True

    async def _try_reconcile_by_checksum(self,
                                         observation: WalkObservation,
                                         record: FileRecord) -> Optional[FileDecision]:
        local_checksum = await self._get_local_checksum(observation)
        if local_checksum is None:
            return None

        updated_record = replace(record, local_checksum=local_checksum)

        if observation.remote_entry is not None and observation.remote_entry.checksum is not None:
            updated_record = replace(updated_record, remote_checksum=observation.remote_entry.checksum)

            if local_checksum == observation.remote_entry.checksum:
                return self._db_update(updated_record, "local and remote checksums match; metadata changed only")

        if observation.file_record is not None and observation.file_record.remote_checksum is not None:
            if local_checksum == observation.file_record.remote_checksum:
                return self._db_update(
                    updated_record,
                    "local checksum matches recorded remote checksum; metadata changed only",
                )

        if observation.file_record is not None and observation.file_record.local_checksum is not None:
            if local_checksum == observation.file_record.local_checksum:
                if observation.remote_is_stale():
                    return self._download(
                        updated_record,
                        "local checksum matches record; remote changed",
                    )

                return self._db_update(
                    updated_record,
                    "local checksum matches record; local metadata changed only",
                )
        return None
