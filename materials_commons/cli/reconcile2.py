import os
import stat
from datetime import datetime, timezone
from os.path import dirname, basename
from pathlib import Path
from typing import Optional

import materials_commons.api.models as mcmodel
from aiofiles import os as aio_os
from materials_commons.api import models

from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.old.functions import checksum_async
from materials_commons.cli.models import FileRecord, LocalObserved, LocalProject, FileState, FileDecision, \
    WalkObservation
from materials_commons.cli.server import projects
from materials_commons.cli.walk import path_to_local_file_entry, mcapi_file_to_remote_file_entry


def reconcile_file(
        remote_entry: Optional[mcmodel.File],
        local_record: Optional[FileRecord],
        local_observed: LocalObserved,
        now_ts: int,
) -> FileDecision:
    """
    Decide what to do for one file path using only the authoritative remote entry,
    the stored local record, and the current local observation.

    This function is a decision engine for one file path. It compares:
        - The remote authoritative file (remote_entry)
        - The stored local database record (local_record)
        - The current local filesystem observation (local_observed)
    and returns a FileDecision describing what should happen next.

    Big picture
    The function answers questions like:
    - Should we download the remote file?
    - Should we preserve/update the database only?
    - Should we skip because local and remote match?
    - Should we mark a conflict?
    It also builds an updated FileRecord every time, so the database state can be refreshed even when no file
    transfer happens.
    """

    # Is there a file on the server?
    remote_exists = remote_entry is not None

    # Is there a file in the local filesystem?
    local_exists = local_observed.exists

    # Do we already know this local record is tied to a remote file?
    known_server_file = local_record is not None and local_record.remote_file_id is not None

    # Check if the remote file is missing
    if remote_entry is None:
        if local_exists:
            # The remote file is missing, but the local file exists. So, we want to
            # keep the local file. Deletes only happen when `mc rm` or `mc mv` are run.
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

        # Local file is also missing. Right now mark the file as unknown (because it exists
        # in the database, but not on the remote or the local filesystem). This means that
        # the file was removed without running the `mc rm` or `mc mv` commands.
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

    # If we are here then a remote entry exists. Let's build out whether the remote and
    # local checksums match. Note: the local file on the file system may not exist (we
    # determine this checking the local_exists flag). When that happens checksums_match
    # will automatically be False.
    checksums_match = (
            local_exists  # Does the local file exist?
            and local_observed.local_checksum is not None  # Does the local file have a checksum?
            and remote_entry.checksum is not None  # Does the remote file have a checksum?
            and local_observed.local_checksum == remote_entry.checksum  # Do the checksums match?
    )

    # Check if the local file is still clean by comparing the file system observed file
    # and the entry from the local database.
    local_is_clean = is_local_still_clean(local_observed, local_record)

    if remote_exists and not local_exists:
        # The file exists on the server, but the local file is missing. We can download this file.
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
        # The local file exists and the checksums match, so we can assume the local file is clean
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

        # The action will be 'skip' if this is already a known server file (and thus already trusted and safely
        # stored on the server). Otherwise, the action is 'adopt' because we don't yet know about the remote file
        # (and the local version matches the remote, so we just need to update the database with the remote info).
        return FileDecision(
            action="skip" if known_server_file else "adopt",
            reason="local content matches remote",
            updated_record=updated,
            updated=False,
        )

    # At this point we know that checksums don't match, so we need to decide what to do.
    if local_exists and known_server_file and local_is_clean:
        # The local file exists and was uploaded to the server, but since the checksums don't
        # match, that means there is a newer version on the server. We need to download the
        # remote version.
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
        # The local file exists, but we don't trust that this version is on the server. We
        # need to preserve the local file if we are doing downloads or upload the file
        # if we are doing uploads. This ensures we don't overwrite user changes that
        # have not been uploaded to the server.
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
        # The local file is known to be server-backed, but because the checksums don't match that means
        # the local and remote files are different (meaning that the files have diverged). Mark this
        # as a conflict. The way to think about this state is that the local file was backed up
        # to the server at some point. Then a different client uploaded a new version to the server,
        # and the local file was also changed. We can't download because that would overwrite the
        # local changes. We could upload, but it's not clear if we should. The user needs to decide
        # the best course of action. So, we mark this as a conflict.
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

    # This is a conservative catch-all case. If we are here, then we missed a case. When that
    # happens, we document that none of our rules matched and preserve the local file. This
    # is a conservative approach, but it is the best we can do.
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
        local_obs: LocalObserved,
        local_record: Optional[FileRecord],
) -> bool:
    """
    True if the current local file still appears to be a clean server-backed copy.
    """
    if local_record is None:
        # There is no local record, so we can't tell if the local file is clean.
        return False
    if not local_obs.exists:
        # The local file doesn't exist, so we can't tell if the local file is clean.
        return False
    if local_record.is_clean_local_copy != 1:
        # The local file is not marked as a clean server-backed copy, so we can't
        # tell if the local file is clean.
        return False

    if (
            local_obs.local_size == local_record.local_size
            and local_obs.local_mtime_ns == local_record.local_mtime_ns
    ):
        # The local file in the file system, and the record from the database match.
        return True

    if local_obs.local_checksum and local_record.remote_checksum:
        # If we generated a new local_checksum, and the record in the database matches
        # the remote checksum, then we can assume that the local file is clean. We would
        # generate a new local checksum if we think the local filesystem file was modified
        # (There can be changes that don't modify the file, but changed the mtime).
        return local_obs.local_checksum == local_record.remote_checksum

    return False


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
    updated = FileRecord(
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

    return updated


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


async def observe_local_file(
        local_path: str,
        file_record: Optional[FileRecord],
        project_path: str,
        recompute_checksum: bool = True
) -> LocalObserved:
    """Observe the local file at the given path, optionally recomputing checksum if needed"""
    sinfo = await safe_stat(local_path)
    if not sinfo:
        return LocalObserved(
            path=local_path,
            project_path=project_path,
            dir=dirname(local_path),
            name=basename(local_path),
            exists=False,
            local_size=None,
            local_mtime_ns=None,
            local_ctime_ns=None,
            local_checksum=None
        )

    is_dir = stat.S_ISDIR(sinfo.st_mode)
    is_symlink = stat.S_ISLNK(sinfo.st_mode)
    is_file = not is_dir and not is_symlink
    local_observed = LocalObserved(
        path=local_path,
        project_path=project_path,
        dir=dirname(local_path),
        name=basename(local_path),
        is_dir=is_dir,
        is_symlink=is_symlink,
        is_file=is_file,
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


async def safe_stat(path: str) -> Optional[os.stat_result]:
    """
    Retrieve the status of a given file or directory.

    Attempts to retrieve the status of a file or directory at the specified path
    asynchronously. If the operation encounters an error such as the file not
    being found, a permission exception, or other OS-related errors, the function
    will return None.

    Parameters:
    path (str): The path to the file or directory whose status is to be retrieved.

    Returns:
    Optional[os.stat_result]: An os.stat_result object containing metadata about
    the file or directory if retrieval is successful. Returns None if an exception
    occurs during the retrieval process.
    """
    try:
        return await aio_os.stat(path)
    except FileNotFoundError:
        return None
    except PermissionError:
        return None
    except NotADirectoryError:
        return None
    except OSError:
        return None
    except Exception:
        return None


async def observe_and_reconcile_to_file_state(db: FileIndexDB,
                                              proj: LocalProject,
                                              file_path: str,
                                              recompute_checksum: bool = True) -> FileState:
    project_path = projects.local_to_remote_project_path(Path(proj.local_path), Path(file_path))

    remote_entry = await projects.get_remote_file_by_path(proj.remote, proj.id, project_path.as_posix())

    file_record = await db.get_file_by_path(project_path.as_posix())

    local_observed = await observe_local_file(local_path=file_path,
                                              file_record=file_record,
                                              project_path=project_path.as_posix(),
                                              recompute_checksum=recompute_checksum)

    decision = reconcile_file(remote_entry=remote_entry,
                              local_record=file_record,
                              local_observed=local_observed,
                              now_ts=int(datetime.now(timezone.utc).timestamp()))

    observation = WalkObservation(
        local_path=Path(file_path),
        remote_path=project_path,
        file_record=file_record,
        local_entry=path_to_local_file_entry(Path(file_path)),
        remote_entry=mcapi_file_to_remote_file_entry(remote_entry)
    )

    file_state = FileState(
        observation=observation,
        file_decision=decision,
    )

    return file_state
