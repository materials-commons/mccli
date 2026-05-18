from __future__ import annotations

import asyncio
from dataclasses import replace
from typing import Literal, Optional

from materials_commons.api import models as mcmodel

from materials_commons.cli.local_project import LocalProject
from materials_commons.cli.models import FileRecord, Observation, FileDecision
from materials_commons.cli.old.functions import checksum

Action = Literal["skip", "upload", "download", "conflict", "adopt", "db_update"]
ObservationState = Literal["neither", "local_only", "remote_only", "both"]
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

    def __init__(self, *,
                 proj: LocalProject,
                 mode: ReconcileMode,
                 reuse_checksum_requires_ctime_match: bool = False):
        self._mode: ReconcileMode = mode
        self._proj: LocalProject = proj
        self._reuse_checksum_requires_ctime_match = reuse_checksum_requires_ctime_match

    async def reconcile_file(self, observation: Observation) -> FileDecision:
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

    def _classify_observation(self, observation: Observation) -> ObservationState:
        if observation.has_local() and observation.has_remote():
            return "both"
        if observation.has_local():
            return "local_only"
        if observation.has_remote():
            return "remote_only"
        return "neither"

    def _record_from_observation(self, observation: Observation) -> FileRecord:
        if observation.file_record is not None:
            return observation.file_record

        return FileRecord(
            path=str(observation.path),
            name=observation.name,
            dir=str(observation.path.parent),
            is_clean_local_copy=0,
        )

    async def _reconcile_directory(self, observation: Observation, record: FileRecord) -> FileDecision:
        state = self._classify_observation(observation)

        if state == "local_only":
            updated_record = self._record_with_local(observation, record)
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
                                      observation: Observation,
                                      record: FileRecord,
                                      state: ObservationState) -> FileDecision:
        if state == "local_only":
            return await self._reconcile_local_only_file(observation, record)

        if state == "remote_only":
            return await self._reconcile_remote_only_file(observation, record)

        if state == "both":
            return await self._reconcile_file_present_on_both_sides(observation, record)

        return self._skip(record, "no file action needed")

    async def _reconcile_local_only_file(self, observation: Observation, record: FileRecord) -> FileDecision:
        updated_record = self._record_with_local(observation, record)
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

    def _reconcile_local_only_status(self, observation: Observation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            if observation.local_entry_matches_record():
                reason = "local only, uploadable - record matches local entry"
            else:
                reason = "local only, uploadable - update local record"
        else:
            reason = "local only, uploadable - add local record"
        return self._upload(record, reason)

    async def _reconcile_local_only_upload(self, observation: Observation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            if observation.local_entry_matches_record() and observation.file_record.local_checksum:
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

    async def _reconcile_local_only_download(self, observation: Observation, record: FileRecord) -> FileDecision:
        record = record.clear_remote()
        if observation.file_record:
            if observation.local_entry_matches_record():
                return self._skip(record, "local entry matches existing record; no remote file to download")
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

    async def _reconcile_remote_only_file(self, observation: Observation, record: FileRecord) -> FileDecision:
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

    def _reconcile_remote_only_status(self, observation: Observation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            # We've seen a previous version of this file
            reason = "remote only, downloadable - update local record"
        else:
            reason = "remote only, downloadable - add local record"
        return self._download(record, reason)

    def _reconcile_remote_only_upload(self, observation: Observation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            reason = "remote only, update local record"
        else:
            reason = "remote only, add local record"
        return self._db_update(record, reason)

    def _reconcile_remote_only_download(self, observation: Observation, record: FileRecord) -> FileDecision:
        if observation.file_record:
            reason = "remote only - update local record"
        else:
            reason = "remote only - add local record"
        return self._download(record, reason)

    async def _reconcile_file_present_on_both_sides(
            self,
            observation: Observation,
            record: FileRecord,
    ) -> FileDecision:
        updated_record = self._record_with_local_and_remote(observation, record)

        if self._mode == "status":
            return self._reconcile_both_sides_only_status(observation, updated_record)
        elif self._mode == "upload":
            return await self._reconcile_both_sides_only_upload(observation, updated_record)
        elif self._mode == "download":
            return await self._reconcile_both_sides_only_download(observation, updated_record)
        elif self._mode == "sync":
            raise ValueError(f"Mode sync not currently supported")
        else:
            raise ValueError(f"Invalid mode: {self._mode}")

    def _reconcile_both_sides_only_status(self, observation: Observation,
                                          updated_record: FileRecord) -> FileDecision:
        if observation.file_record:
            # We have a local file_record
            if observation.local_entry_matches_record():
                # The file record matches the local entry
                if observation.file_record.remote_file_id == observation.remote_entry.remote_file_id:
                    # The file record also matches the remote entry file id
                    return self._skip(updated_record, "local file matches remote")
                else:
                    # The file record has a different remote file id, suggest download
                    return self._download(updated_record, "local file differs from remote, previous version uploaded")
            else:
                # The file record doesn't match the local entry
                if observation.file_record.remote_file_id == observation.remote_entry.remote_file_id:
                    # The out-of-date file record remote id matches the remote id on the server. The on disk
                    # file has changed, suggesting upload.
                    return self._upload(updated_record, "local file changed, previous file uploaded")
                else:
                    # The out-of-date file record has a different id than on the server. We don't know the
                    # state without computing the checksum and doing additional work. We will suggest that
                    # the user run a scan to reconcile the state.
                    return self._skip(updated_record, "status unknown; run scan to reconcile with checksum")
        else:
            # We don't have a local file record, so we can't figure out the state. We will suggest that
            # the user run a scan to reconcile the state.
            return self._skip(updated_record, "status unknown; run scan to reconcile with checksum")

    async def _reconcile_both_sides_only_upload(self, observation: Observation,
                                                updated_record: FileRecord) -> FileDecision:
        if observation.file_record:
            return await self._reconcile_both_sides_upload_have_record(observation, updated_record)
        return await self._reconcile_both_sides_upload_no_record(observation, updated_record)

    async def _reconcile_both_sides_upload_have_record(self, observation: Observation,
                                                       updated_record: FileRecord) -> FileDecision:
        if observation.local_entry_matches_record():
            if observation.file_record.remote_file_id == observation.remote_entry.remote_file_id:
                # No action needed, files are identical
                return self._skip(updated_record, "local matches remote")
            else:
                # No action needed, local has been uploaded, but remote changed, and we could download it.
                return self._skip(updated_record, "local matches remote, but remote file differs (downloadable)")
        # Local entry is different. Compute new checksum and upload.
        local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
        updated_record = replace(updated_record, local_checksum=local_checksum)
        return self._upload(updated_record, "local file changed")

    async def _reconcile_both_sides_upload_no_record(self, observation: Observation,
                                                     updated_record: FileRecord) -> FileDecision:
        # No file record. Let's compute the checksum and see if that matches the remote
        local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
        if local_checksum == observation.remote_entry.checksum:
            return self._db_update(updated_record, "local matches remote, add file record")
        updated_record = replace(updated_record, local_checksum=local_checksum)
        return self._upload(updated_record, "local file changed")

    async def _reconcile_both_sides_only_download(self, observation: Observation,
                                                  updated_record: FileRecord) -> FileDecision:
        if observation.file_record:
            return await self._reconcile_download_both_sides_have_file_record(observation, updated_record)
        return await self._reconcile_download_both_sides_no_file_record(observation, updated_record)

    async def _reconcile_download_both_sides_have_file_record(self, observation: Observation,
                                                              updated_record: FileRecord) -> FileDecision:
        """
        Handle the case where there is a local file, a remote file, and we have a file record.
        """
        # We have a file record. Check if the local entry and the file record match
        if observation.local_entry_matches_record():
            # Local entry matches the file record. Now check if file record id and remote file id match.
            if observation.file_record.remote_file_id == observation.remote_entry.remote_file_id:
                # If the ids match, then skip because local and remote versions match
                return self._skip(updated_record, "local and remote versions match")
            else:
                # The ids don't match. However, since we have a file_record, and the file record and
                # the local file match (local_entry_matches_record(), we know that this version of
                # the file was previously uploaded. Since the file_record id, and the remote_entry id
                # don't match, we know the remote has changed, but we can safely download it.
                return self._download(updated_record, "local and remote versions differ")
        else:
            # The file record and the local file don't match. We need to compute the checksum for the
            # local to determine if the local file has ever been uploaded.
            local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
            # Check if the local checksum matches the file record checksum
            if local_checksum == observation.file_record.local_checksum:
                # The checksum matches, so this file has been uploaded. So we can safely download the remote.
                return self._download(updated_record, "local and remote versions match")
            else:
                updated_record = replace(updated_record, local_checksum=local_checksum)
                # The local_checksum does not match the file record. So we need to ask the server
                # if a previous version of this file has been uploaded.
                previous_version = await self._previous_version_uploaded(observation, local_checksum)
                if previous_version:
                    # A previous version of this file has been uploaded. We can safely download the remote.
                    return self._download(updated_record, "local and remote versions match")
                else:
                    # No previously uploaded version of the file matches this checksum. This is a conflict.
                    return self._conflict(updated_record, "local file has changed and was never uploaded")

    async def _reconcile_download_both_sides_no_file_record(self, observation: Observation,
                                                            updated_record: FileRecord) -> FileDecision:
        """
           Handle the case where there is a local file, a remote file, and we do not have a file record.
        """
        # There is no file_record; that means we don't know the state of the local file. First, lets compute
        # the checksum of the local file.
        local_checksum = await asyncio.to_thread(checksum, observation.local_entry.path.as_posix())
        if local_checksum == observation.remote_entry.checksum:
            # Need to create a file_record
            return self._db_update(updated_record, "local file matches remote checksum")

        # The local_checksum doesn't match. Let's see if there is a previous version on the server that
        # matches the local_checksum
        updated_record = replace(updated_record, local_checksum=local_checksum)
        remote_record = await self._previous_version_uploaded(observation, local_checksum)
        if remote_record:
            # There is a previous version that matches. We can safely download the remote file
            return self._download(updated_record, "local file already upload, remote changed")

        # There is no matching previous version, the local file has changed, and the remote has changed.
        # This is a conflict.
        return self._conflict(updated_record, "local file changed, remote changed, local file never uploaded")

    async def _previous_version_uploaded(self, observation: Observation,
                                         local_checksum: str) -> Optional[mcmodel.File]:
        """Gets the previous versions of a file and checks if any match the local checksum"""

        # If we don't have a remote file id then we need to get on by querying on the path.
        if observation.remote_entry is None or observation.remote_entry.remote_file_id is None:
            remote_entry = await asyncio.to_thread(self._proj.remote.get_file_by_path, self._proj.id,
                                                   observation.remote_path)
            if remote_entry is None:
                # Didn't find a match for the remote file
                return None
            elif remote_entry.checksum == local_checksum:
                # The remote file matches the local checksum, no need to check previous versions
                return remote_entry
            else:
                # The remote file doesn't match the local checksum, but we can use its id to get previous versions.
                remote_file_id = remote_entry.remote_file_id
        else:
            # There is a remote entry use its id
            remote_file_id = observation.remote_entry.remote_file_id

        # Get previous versions and look for a match
        previous_versions = await asyncio.to_thread(self._proj.remote.get_file_versions, self._proj.id, remote_file_id)
        if previous_versions is None:
            return None
        for prev_version in previous_versions:
            if prev_version.checksum == local_checksum:
                return prev_version
        return None

    def _record_with_local(self, observation: Observation, record: FileRecord) -> FileRecord:
        if observation.local_entry is None:
            return record

        return replace(
            record,
            local_size=observation.local_entry.size,
            local_mtime_ns=observation.local_entry.mtime_ns,
            local_ctime_ns=observation.local_entry.ctime_ns,
        )

    def _record_with_remote(self, observation: Observation, record: FileRecord) -> FileRecord:
        if observation.remote_entry is None:
            return record

        return replace(
            record,
            remote_file_id=observation.remote_entry.remote_file_id,
            remote_size=observation.remote_entry.size,
            remote_ctime_ns=observation.remote_entry.ctime_ns,
            remote_checksum=observation.remote_entry.checksum,
        )

    def _record_with_local_and_remote(self, observation: Observation, record: FileRecord) -> FileRecord:
        return self._record_with_remote(observation, self._record_with_local(observation, record))

    def _decision(self, action: Action, record: FileRecord, reason: str, updated: bool) -> FileDecision:
        return FileDecision(
            action=action,
            reason=reason,
            updated=updated,
            updated_record=record,
        )

    def _skip(self, record: FileRecord, reason: str) -> FileDecision:
        return self._decision("skip", record, reason, updated=False)

    def _upload(self, record: FileRecord, reason: str) -> FileDecision:
        return self._decision("upload", record, reason, updated=True)

    def _download(self, record: FileRecord, reason: str) -> FileDecision:
        return self._decision("download", record, reason, updated=True)

    def _conflict(self, record: FileRecord, reason: str) -> FileDecision:
        return self._decision("conflict", record, reason, updated=False)

    def _db_update(self, record: FileRecord, reason: str) -> FileDecision:
        return self._decision("db_update", record, reason, updated=True)
