from materials_commons.cli.models import RemoteFileEntry, FileRecord, LocalObserved
import materials_commons.api.models as mcmodel

from dataclasses import dataclass
from typing import Optional, Literal

Action = Literal["skip", "download", "conflict", "adopt", "db_update"]


@dataclass(frozen=True)
class FileDecision:
    action: Action
    reason: str
    updated_record: FileRecord


def reconcile_remote_file(
        remote_entry: mcmodel.File,
        local_record: Optional[FileRecord],
        local_observed: LocalObserved,
        now_ts: int,
) -> FileDecision:
    """
    Decide what to do for one file path using only the authoritative remote entry,
    the stored local record, and the current local observation.
    """

    known_server_file = local_record is not None and local_record.remote_file_id is not None

    checksums_match = (
            local_observed.exists
            and local_observed.local_checksum is not None
            and remote_entry.checksum is not None
            and local_observed.local_checksum == remote_entry.checksum
    )

    local_is_clean = is_local_still_clean(local_observed, local_record)

    # 1) Local missing -> download
    if remote_entry and not local_observed.exists:
        updated = build_updated_record(
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="pending_download",
        )
        return FileDecision(
            action="download",
            reason="local file is missing",
            updated_record=updated,
        )

    # 2) Exact content match -> skip or adopt
    if local_observed.exists and checksums_match:
        updated = build_updated_record(
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=1,
            status="ok",
        )
        return FileDecision(
            action="skip" if known_server_file else "adopt",
            reason="local content matches remote",
            updated_record=updated,
        )

    # 3) Server file changed, local is trusted clean -> download
    if local_observed.exists and known_server_file and local_is_clean:
        updated = build_updated_record(
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="pending_download",
        )
        return FileDecision(
            action="download",
            reason="remote changed and local is a trusted clean copy",
            updated_record=updated,
        )

    # 4) Local exists but isn't trusted as server-backed -> preserve it
    if local_observed.exists and not known_server_file:
        updated = build_updated_record(
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="local_only" if local_record is None else "dirty",
        )
        return FileDecision(
            action="db_update",
            reason="local file is not known to be server-backed; preserve it",
            updated_record=updated,
        )

    # 5) Server-backed file diverged -> conflict
    if local_observed.exists and known_server_file:
        updated = build_updated_record(
            record=local_record,
            remote=remote_entry,
            local_obs=local_observed,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="conflict",
        )
        return FileDecision(
            action="conflict",
            reason="remote and local diverged",
            updated_record=updated,
        )

    # Fallback
    updated = build_updated_record(
        record=local_record,
        remote=remote_entry,
        local_obs=local_observed,
        now_ts=now_ts,
        is_clean_local_copy=0,
        status="unknown",
    )
    return FileDecision(
        action="db_update",
        reason="no matching rule applied; preserving local state",
        updated_record=updated,
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

    # Fast path: cached stat matches
    if (
            local_obs.local_size == local_record.local_size
            and local_obs.local_mtime_ns == local_record.local_mtime_ns
    ):
        return True

    # Stronger path: checksum matches the last known remote checksum
    if local_obs.local_checksum and local_record.remote_checksum:
        return local_obs.local_checksum == local_record.remote_checksum

    return False


def build_updated_record(
        *,
        path: str,
        record: Optional[FileRecord],
        remote: mcmodel.File,
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
        dir=remote.dir,
        name=remote.name,

        local_size=local_obs.local_size,
        local_mtime_ns=local_obs.local_mtime_ns,
        local_ctime_ns=local_obs.local_ctime_ns,
        local_checksum=local_obs.local_checksum,
        local_last_seen_ts=now_ts if local_obs.exists else record.local_last_seen_ts if record else None,

        remote_file_id=remote.id,
        remote_size=remote.size,
        # remote_ctime_ns=remote.ctime * 1000,
        remote_checksum=remote.checksum,
        remote_last_seen_ts=now_ts,

        is_clean_local_copy=is_clean_local_copy,
        origin=origin if origin is not None else (record.origin if record else None),
        status=status if status is not None else (record.status if record else None),
        transfer_id=record.transfer_id if record else None,
    )
