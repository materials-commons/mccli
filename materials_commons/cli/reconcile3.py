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

    def __init__(self,  *, compute_checksum: bool = False, reuse_checksum_requires_ctime_match: bool = False):
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
            return await self._reconcile_regular_file(observation, record)

        return self._conflict(record, "entry kind is unknown")

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
                return self.db_update(updated_record, "local and remote checksums match; metadata changed only")

        if observation.file_record is not None and observation.file_record.remote_checksum is not None:
            if local_checksum == observation.file_record.remote_checksum:
                return self.db_update(
                    updated_record,
                    "local checksum matches recorded remote checksum; metadata changed only",
                )

        if observation.file_record is not None and observation.file_record.local_checksum is not None:
            if local_checksum == observation.file_record.local_checksum:
                if observation.remote_is_stale():
                    return self.download(
                        updated_record,
                        "local checksum matches record; remote changed",
                    )

                return self.db_update(
                    updated_record,
                    "local checksum matches record; local metadata changed only",
                )
        return None

    def _classify_observation(self, observation: WalkObservation) -> ObservationState:
        if observation.has_local() and observation.has_remote():
            return "both"
        if observation.has_local():
            return "local_only"
        if observation.has_remote():
            return "remote_only"
        return "neither"

    async def _reconcile_directory(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        state = self._classify_observation(observation)

        if state == "local_only":
            updated_record = self.record_with_local(observation, record)
            return self._skip(updated_record, "local directory exists only locally")

        if state == "remote_only":
            updated_record = self.record_with_remote(observation, record)
            return self._skip(updated_record, "remote directory exists only remotely")

        if state == "both":
            updated_record = self._record_with_local_and_remote(observation, record)

            if observation.has_record() and observation.record_is_stale():
                return self.db_update(updated_record, "directory metadata changed")

            return self._skip(updated_record, "directory exists locally and remotely")

        return self._skip(record, "no directory action needed")

    async def _reconcile_regular_file(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        state = self._classify_observation(observation)

        if state == "local_only":
            return await self._reconcile_local_only_file(observation, record)

        if state == "remote_only":
            return await self._reconcile_remote_only_file(observation, record)

        if state == "both":
            return await self._reconcile_file_present_on_both_sides(observation, record)

        return self._skip(record, "no file action needed")

    async def _reconcile_local_only_file(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        updated_record = self.record_with_local(observation, record)
        local_checksum = await self._get_local_checksum(observation)
        if local_checksum is not None:
            updated_record = replace(updated_record, local_checksum=local_checksum)

        if observation.has_record() and observation.local_entry_matches_record(include_checksum=self._compute_checksum):
            return self.upload(updated_record, "local file matches record but is missing remotely")

        if observation.has_record() and observation.local_is_stale():
            return self.upload(updated_record, "local file changed and is missing remotely")

        return self.upload(updated_record, "local file exists only locally")

    async def _reconcile_remote_only_file(self, observation: WalkObservation, record: FileRecord) -> FileDecision:
        updated_record = self.record_with_remote(observation, record)

        if observation.has_record() and observation.remote_entry_matches_record():
            return self.download(updated_record, "remote file matches record but is missing locally")

        if observation.has_record() and observation.remote_is_stale():
            return self.download(updated_record, "remote file changed and is missing locally")

        return self.download(updated_record, "remote file exists only remotely")

    async def _reconcile_file_present_on_both_sides(
            self,
            observation: WalkObservation,
            record: FileRecord,
    ) -> FileDecision:
        updated_record = self._record_with_local_and_remote(observation, record)

        if self._local_and_remote_metadata_match(observation):
            local_checksum = await self._get_local_checksum(observation)
            if local_checksum is not None:
                updated_record = replace(updated_record, local_checksum=local_checksum)

            if observation.has_record() and observation.record_is_stale():
                return self.db_update(
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
            return self.upload(updated_record, "local file changed; remote still matches record")

        if remote_changed and not local_changed:
            return self.download(updated_record, "remote file changed; local still matches record")

        if local_changed and remote_changed:
            return self._conflict(updated_record, "local and remote files both changed")

        return self._conflict(updated_record, "unable to determine safe file reconciliation action")

    def _record_from_observation(self, observation: WalkObservation) -> FileRecord:
        if observation.file_record is not None:
            return observation.file_record

        return FileRecord(
            path=str(observation.path),
            name=observation.name,
            dir=str(observation.path.parent),
            is_clean_local_copy=0,
        )

    def record_with_local(self, observation: WalkObservation, record: FileRecord) -> FileRecord:
        if observation.local_entry is None:
            return record

        return replace(
            record,
            local_size=observation.local_entry.size,
            local_mtime_ns=observation.local_entry.mtime_ns,
            local_ctime_ns=observation.local_entry.ctime_ns,
        )

    def record_with_remote(self, observation: WalkObservation, record: FileRecord) -> FileRecord:
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
        return self.record_with_remote(observation, self.record_with_local(observation, record))

    def _local_and_remote_metadata_match(self, observation: WalkObservation) -> bool:
        if observation.local_entry is None or observation.remote_entry is None:
            return False

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

    def upload(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("upload", record, reason, updated=True)

    def download(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("download", record, reason, updated=True)

    def _conflict(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("conflict", record, reason, updated=False)

    def adopt(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("adopt", record, reason, updated=True)

    def db_update(self, record: FileRecord, reason: str) -> FileDecision:
        return self.decision("db_update", record, reason, updated=True)
