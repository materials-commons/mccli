from typing import Optional
from models import FileRecord, RemoteFileEntry, FileDecision, LocalObserved, DirectoryDecision
from os.path import dirname, basename
from aiofiles import os as aio_os

from materials_commons.cli.functions import checksum_async

async def reconcile_directory(
    directory_path: str,
    remote_entries: list[RemoteFileEntry],
    db_records_by_name: dict[str, FileRecord],
    checksum_func,
    now_ts: int,
) -> DirectoryDecision:
    result = DirectoryDecision(
        to_download=[],
        to_skip=[],
        to_conflict=[],
        to_db_update_only=[],
    )

    for remote in remote_entries:
        record = db_records_by_name.get(remote.name)
        decision = await reconcile_file(remote, record, checksum_func, now_ts)

        if decision.action == "download":
            result.to_download.append(decision)
        elif decision.action == "skip":
            result.to_skip.append(decision)
        elif decision.action == "conflict":
            result.to_conflict.append(decision)
        else:
            result.to_db_update_only.append(decision)

    return result

async def reconcile_file(
        remote: RemoteFileEntry,
        record: Optional[FileRecord],
        now_ts: int,
) -> FileDecision:

    local_obs = await observe_local_file(remote.path, record)

    # 1. Local file missing -> download
    if not local_obs.exists:
        updated = merge_record(
            path=remote.path,
            local_obs=local_obs,
            remote=remote,
            old=record,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="pending_download",
        )
        return FileDecision(
            action="download",
            reason="local file is missing",
            updated_record=updated,
        )

    # 2. If local content equals current remote, skip and mark clean
    if local_matches_remote(local_obs, remote):
        updated = merge_record(
            path=remote.path,
            local_obs=local_obs,
            remote=remote,
            old=record,
            now_ts=now_ts,
            is_clean_local_copy=1,
            origin="downloaded" if record is None else (record.origin or "downloaded"),
            status="ok",
        )
        return FileDecision(
            action="skip",
            reason="local content already matches remote",
            updated_record=updated,
        )

    remote_changed = not remote_matches_cache(remote, record)
    local_clean = local_is_still_clean(local_obs, record)

    # 3. Remote changed, local is still a trusted clean copy -> safe overwrite
    if remote_changed and local_clean:
        updated = merge_record(
            path=remote.path,
            local_obs=local_obs,
            remote=remote,
            old=record,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="pending_download",
        )
        return FileDecision(
            action="download",
            reason="remote changed and local is still a clean copy",
            updated_record=updated,
        )

    # 4. Remote unchanged, but local differs from remote -> local dirty/unknown
    if not remote_changed:
        updated = merge_record(
            path=remote.path,
            local_obs=local_obs,
            remote=remote,
            old=record,
            now_ts=now_ts,
            is_clean_local_copy=0,
            status="local_only" if record is None else "dirty",
        )
        return FileDecision(
            action="db_update_only",
            reason="remote unchanged, local differs, preserve local file",
            updated_record=updated,
        )

    # 5. Remote changed, local not clean -> conflict
    updated = merge_record(
        path=remote.path,
        local_obs=local_obs,
        remote=remote,
        old=record,
        now_ts=now_ts,
        is_clean_local_copy=0,
        status="conflict",
    )
    return FileDecision(
        action="conflict",
        reason="remote changed but local file is dirty or unknown",
        updated_record=updated,
    )

def local_stat_matches_cache(local_obs: LocalObserved, record: Optional[FileRecord]) -> bool:
    """Verifies a local file matches a record using stat cache"""
    if record is None:
        return False
    if not local_obs.exists:
        return False

    return (
        local_obs.local_size == record.local_size and
        local_obs.local_mtime_ns == record.local_mtime_ns
    )

def remote_matches_cache(remote: RemoteFileEntry, record: Optional[FileRecord]) -> bool:
    """Verifies a remote file matches a record using checksum or stat cache"""
    if record is None:
        return False

    if remote.remote_checksum and record.remote_checksum:
        return remote.remote_checksum == record.remote_checksum

    # Use ctime for remote rather than mtime, since ctime changes everytime the file changes
    # (because we create new versions of the file)
    return (
        remote.remote_file_id == record.remote_file_id and
        remote.remote_size == record.remote_size and
        remote.remote_ctime_ns == record.remote_ctime_ns
    )

def local_matches_remote(local_obs: LocalObserved, remote: RemoteFileEntry) -> bool:
    """Verifies a local file matches a remote file using checksum"""
    if not local_obs.exists:
        return False
    if not local_obs.local_checksum:
        return False
    if not remote.remote_checksum:
        return False

    return local_obs.local_checksum == remote.remote_checksum

def local_is_still_clean(local_obs: LocalObserved, record: Optional[FileRecord]) -> bool:
    """Verifies a local file remains clean against the record using stat cache or checksum"""
    if record is None:
        return False
    if not local_obs.exists:
        return False
    if record.is_clean_local_copy != 1:
        return False

    if local_stat_matches_cache(local_obs, record):
        return True

    if local_obs.local_checksum and record.remote_checksum:
        return local_obs.local_checksum == record.remote_checksum

    return False

async def observe_local_file(local_path: str, file_record: Optional[FileRecord], recompute_checksum: bool = True) -> LocalObserved:
    """Attempts to stat a local file and return a LocalObserved object"""
    exists = await aio_os.path.exists(local_path)
    if not exists:
        return LocalObserved(
            path=local_path,
            dir=dirname(local_path),
            name=basename(local_path),
            exists=False,
            local_size=None,
            local_mtime_ns=None,
            local_ctime_ns=None,
            local_checksum=None
        )
    sinfo = await aio_os.stat(local_path)
    local_observed = LocalObserved(
        path=local_path,
        dir=dirname(local_path),
        name=basename(local_path),
        exists=True,
        local_size=sinfo.st_size,
        local_mtime_ns=sinfo.st_mtime_ns,
        local_ctime_ns=sinfo.st_ctime_ns,
        local_checksum=None
    )

    if local_stat_matches_cache(local_observed, file_record):
        local_observed.local_checksum = file_record.local_checksum
        return local_observed

    if not recompute_checksum:
        local_observed.checksum_outdated = True
        return local_observed

    local_observed.local_checksum = await checksum_async(local_path)
    return local_observed

def merge_record(
    path: str,
    local_obs: LocalObserved,
    remote: RemoteFileEntry,
    old: Optional[FileRecord],
    now_ts: int,
    is_clean_local_copy: int,
    origin: Optional[str] = None,
    status: Optional[str] = None,
) -> FileRecord:
    return FileRecord(
        path=path,
        dir=remote.dir,
        name=remote.name,

        local_size=local_obs.local_size,
        local_mtime_ns=local_obs.local_mtime_ns,
        local_ctime_ns=local_obs.local_ctime_ns,
        local_checksum=local_obs.local_checksum,
        local_last_seen_ts=now_ts if local_obs.exists else old.local_last_seen_ts if old else None,

        remote_file_id=remote.remote_file_id,
        remote_size=remote.remote_size,
        remote_ctime_ns=remote.remote_ctime_ns,
        remote_checksum=remote.remote_checksum,
        remote_last_seen_ts=now_ts,

        is_clean_local_copy=is_clean_local_copy,
        origin=origin if origin is not None else (old.origin if old else None),
        status=status if status is not None else (old.status if old else None),
        transfer_id=old.transfer_id if old else None,
    )
