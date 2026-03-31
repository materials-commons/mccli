import os
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Optional, Literal

import materials_commons.api.models as mcmodel
from materials_commons.api import models

from materials_commons.cli.filedb import FileIndexDB

from materials_commons.cli.models import FileRecord, LocalObserved
from materials_commons.cli.reconcile import observe_local_file

Action = Literal["skip", "download", "conflict", "adopt", "db_update"]


@dataclass(frozen=True)
class FileDecision:
    action: Action
    reason: str
    updated: bool
    updated_record: FileRecord


def reconcile_file(
        remote_entry: Optional[mcmodel.File],
        local_record: Optional[FileRecord],
        local_observed: LocalObserved,
        now_ts: int,
) -> FileDecision:
    """
    Decide what to do for one file path using only the authoritative remote entry,
    the stored local record, and the current local observation.
    """
    remote_exists = remote_entry is not None
    local_exists = local_observed.exists
    known_server_file = local_record is not None and local_record.remote_file_id is not None

    if remote_entry is None:
        if local_exists:
            updated = build_updated_record(
                path=local_record.path if local_record else local_observed.path,
                record=local_record,
                remote=None,
                local_obs=local_observed,
                now_ts=now_ts,
                is_clean_local_copy=0,
                status="local_only" if local_record is None else "dirty",
                origin=(local_record.origin if local_record else None),
            )
            return FileDecision(
                action="db_update",
                reason="remote file is missing; preserve local file",
                updated_record=updated,
                updated=True,
            )

        updated = build_updated_record(
            path=local_record.path if local_record else local_observed.path,
            record=local_record,
            remote=None,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="unknown",
            origin=(local_record.origin if local_record else None),
        )
        return FileDecision(
            action="db_update",
            reason="both remote and local file are missing",
            updated_record=updated,
            updated=False,
        )

    checksums_match = (
            local_exists
            and local_observed.local_checksum is not None
            and remote_entry.checksum is not None
            and local_observed.local_checksum == remote_entry.checksum
    )

    local_is_clean = is_local_still_clean(local_observed, local_record)

    if remote_exists and not local_exists:
        updated = build_updated_record(
            path=remote_entry.path,
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="pending_download",
            origin="downloaded" if local_record is None else (local_record.origin or "downloaded"),
        )
        return FileDecision(
            action="download",
            reason="local file is missing",
            updated_record=updated,
            updated=True,
        )

    if local_exists and checksums_match:
        updated = build_updated_record(
            path=remote_entry.path,
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=1,
            status="ok",
            origin=(local_record.origin if local_record else "downloaded"),
        )
        return FileDecision(
            action="skip" if known_server_file else "adopt",
            reason="local content matches remote",
            updated_record=updated,
            updated=False,
        )

    # At this point we know that checksums don't match, so we need to decide what to do.
    if local_exists and known_server_file and local_is_clean:
        updated = build_updated_record(
            path=remote_entry.path,
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="pending_download",
            origin=local_record.origin if local_record else None,
        )
        return FileDecision(
            action="download",
            reason="remote changed and local is a trusted clean copy",
            updated_record=updated,
            updated=True,
        )

    if local_exists and not known_server_file:
        updated = build_updated_record(
            path=remote_entry.path,
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="local_only" if local_record is None else "dirty",
            origin=local_record.origin if local_record else None,
        )
        return FileDecision(
            action="db_update",
            reason="local file is not known to be server-backed; preserve it",
            updated_record=updated,
            updated=True,
        )

    if local_exists and known_server_file:
        updated = build_updated_record(
            path=remote_entry.path,
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="conflict",
            origin=local_record.origin if local_record else None,
        )
        return FileDecision(
            action="conflict",
            reason="remote and local diverged",
            updated_record=updated,
            updated=True,
        )

    updated = build_updated_record(
        path=remote_entry.path,
        record=local_record,
        remote=remote_entry,
        local_obs=local_observed,
        now_ts=now_ts,
        is_clean_local_copy=0,
        status="unknown",
        origin=local_record.origin if local_record else None,
    )
    return FileDecision(
        action="db_update",
        reason="no matching rule applied; preserving local state",
        updated_record=updated,
        updated=False,
    )


def is_local_still_clean(
        local_obs: "LocalObserved",
        local_record: Optional["FileRecord"],
) -> bool:
    """
    True if the current local file still appears to be a clean server-backed copy.
    """
    if local_record is None:
        return False
    if not local_obs.exists:
        return False
    if local_record.is_clean_local_copy != 1:
        return False

    if (
            local_obs.local_size == local_record.local_size
            and local_obs.local_mtime_ns == local_record.local_mtime_ns
    ):
        return True

    if local_obs.local_checksum and local_record.remote_checksum:
        return local_obs.local_checksum == local_record.remote_checksum

    return False


def build_updated_record(
        *,
        path: str,
        record: Optional[FileRecord],
        remote: Optional[mcmodel.File],
        local_obs: LocalObserved,
        now_ts: int,
        is_clean_local_copy: int,
        status: Optional[str],
        origin: Optional[str],
) -> FileRecord:
    """
    Placeholder for merging current observations into a new FileRecord.
    """
    return FileRecord(
        path=path,
        dir=os.path.dirname(path),
        name=os.path.basename(path),

        local_size=local_obs.local_size,
        local_mtime_ns=local_obs.local_mtime_ns,
        local_ctime_ns=local_obs.local_ctime_ns,
        local_checksum=local_obs.local_checksum,
        local_last_seen_ts=now_ts if local_obs.exists else record.local_last_seen_ts if record else None,

        remote_file_id=remote.id if remote is not None else (record.remote_file_id if record else None),
        remote_size=remote.size if remote is not None else (record.remote_size if record else None),
        remote_checksum=remote.checksum if remote is not None else (record.remote_checksum if record else None),
        remote_last_seen_ts=now_ts if remote is not None else (record.remote_last_seen_ts if record else None),

        is_clean_local_copy=is_clean_local_copy,
        origin=origin if origin is not None else (record.origin if record else None),
        status=status if status is not None else (record.status if record else None),
        transfer_id=record.transfer_id if record else None,
    )


async def observe_and_reconcile(db: FileIndexDB,
                                project_path: str,
                                file_path: str,
                                remote_entry: Optional[models.File],
                                recompute_checksum: bool = True) -> FileDecision:
    file_record = await db.get_file_by_path(project_path)
    local_observed = await observe_local_file(local_path=file_path,
                                              file_record=file_record,
                                              project_path=project_path,
                                              recompute_checksum=recompute_checksum)
    return reconcile_file(remote_entry=remote_entry,
                          local_record=await db.get_file_by_path(project_path),
                          local_observed=local_observed,
                          now_ts=int(datetime.now(timezone.utc).timestamp()))


async def observe_and_reconcile2(file_record: Optional[FileRecord],
                                 project_path: str,
                                 file_path: str,
                                 remote_entry: Optional[models.File],
                                 recompute_checksum: bool = True) -> FileDecision:
    local_observed = await observe_local_file(local_path=file_path,
                                              file_record=file_record,
                                              project_path=project_path,
                                              recompute_checksum=recompute_checksum)
    return reconcile_file(remote_entry=remote_entry,
                          local_record=file_record,
                          local_observed=local_observed,
                          now_ts=int(datetime.now(timezone.utc).timestamp()))
