import asyncio
from datetime import timezone
from pathlib import Path
from typing import AsyncIterator, Callable, Optional, Awaitable

import materials_commons.api.models as mcmodel

from materials_commons.cli.models import OldLocalProject, Observation, LocalFileEntry, RemoteFileEntry, EntryKind
from materials_commons.cli.server import projects

IgnoreFunc = Callable[[Path, bool], bool]
ListDirFunc = Callable[[Path], Awaitable[list[Observation]]]


async def async_walk(
        path: str | Path,
        listdir_fn: ListDirFunc,
        recursive: bool = True,
        ignore_fn: Optional[IgnoreFunc] = None,
) -> AsyncIterator[tuple[Path, list[Observation]]]:
    """Asynchronously walk a directory tree, yielding WalkObservation objects for each directory and file.

    Args:
        path: The root directory to start the walk from.
        listdir_fn: A function to list the contents of a directory.
        recursive: Whether to recursively visit subdirectories.
        ignore_fn: An optional function to ignore directories or files.

    Returns:
        An async iterator yielding tuples of (Path, list[WalkObservation]) for each directory visited.
        The list contains WalkObservation objects for each file and directory within the directory.
        Path is the current directory that list[WalkObservation] entries belong to.
        The iterator stops when all directories have been visited.
    """
    root = Path(path)
    stack = [root]

    while stack:
        current = stack.pop()
        if ignore_fn and ignore_fn(current, True):
            continue

        observations = await listdir_fn(current)

        filtered: list[Observation] = []
        for entry in observations:
            if _default_files_to_ignore(entry.path):
                continue

            if ignore_fn and ignore_fn(entry.path, entry.is_dir):
                continue

            filtered.append(entry)

        yield current, filtered

        # if recursive, then add all subdirectories that haven't been filtered out to the stack,
        # reversed so that the subdirectories are processed in the same order as the parent directories
        if recursive:
            stack.extend(entry.path for entry in reversed(filtered) if entry.is_dir)


def path_to_local_file_entry(entry: Path) -> Optional[LocalFileEntry]:
    """Convert a Path object to a LocalFileEntry object."""

    if not entry.exists():
        return None

    is_symlink = entry.is_symlink()
    size = None
    mtime_ns = None
    ctime_ns = None
    kind: EntryKind | None = None

    try:
        sinfo = entry.stat()
        size = sinfo.st_size
        mtime_ns = sinfo.st_mtime_ns
        ctime_ns = sinfo.st_ctime_ns
    except OSError:
        pass

    try:
        if is_symlink:
            if entry.is_dir():
                kind = "dir"
            elif entry.is_file():
                kind = "file"
            else:
                kind = None
        else:
            if entry.is_dir():
                kind = "dir"
            elif entry.is_file():
                kind = "file"
            else:
                kind = None
    except OSError:
        kind = None

    local = LocalFileEntry(
        path=entry,
        name=entry.name,
        kind=kind,
        is_symlink=is_symlink,
        size=size,
        mtime_ns=mtime_ns,
        ctime_ns=ctime_ns,
        raw=entry,
    )
    return local


async def local_listdir(path: str | Path) -> list[Observation]:
    """Asynchronously list the contents of a directory."""
    root = Path(path)

    def _scan() -> list[Observation]:
        items: list[Observation] = []

        for entry in root.iterdir():
            local = path_to_local_file_entry(entry)

            items.append(
                Observation(
                    local_path=entry,
                    remote_path=None,
                    local_entry=local,
                    file_record=None,
                    remote_entry=None,
                )
            )

        return items

    return await asyncio.to_thread(_scan)


def mcapi_file_to_remote_file_entry(entry: mcmodel.File) -> RemoteFileEntry:
    kind: EntryKind = "dir" if entry.mime_type == "directory" else "file"
    remote_entry = RemoteFileEntry(
        path=Path(entry.directory.path) / entry.name,
        name=entry.name,
        kind=kind,
        size=entry.size,
        mtime_ns=int(entry.updated_at.replace(tzinfo=timezone.utc).timestamp() * 1_000_000_000),
        ctime_ns=int(entry.created_at.replace(tzinfo=timezone.utc).timestamp() * 1_000_000_000),
        remote_file_id=getattr(entry, "id", None),
        checksum=getattr(entry, "checksum", None),
        raw=entry,
    )
    return remote_entry


async def remote_listdir(project_path: str | Path, proj: OldLocalProject) -> list[Observation]:
    """Asynchronously list the contents of a remote directory.

    Args:
        project_path: The path within the project to list directory contents of.
        proj: The local project object representing the remote project.
    """
    entries = await asyncio.to_thread(proj.remote.list_directory_by_path, proj.id, project_path.as_posix())
    items: list[Observation] = []
    for entry in entries:
        remote_entry = mcapi_file_to_remote_file_entry(entry)
        items.append(
            Observation(
                local_path=None,
                remote_path=Path(entry.path),
                local_entry=None,
                file_record=None,
                remote_entry=remote_entry,
            )
        )
    return items


def make_remote_listdir_func(proj: OldLocalProject) -> ListDirFunc:
    """
    Create an asynchronous directory listing function for a remote project.

    Args:
        proj: The local project object representing the remote project.

    Returns:
        An asynchronous function that lists directory contents of a remote project.
    """

    async def _remote_listdir(path: Path) -> list[Observation]:
        return await remote_listdir(path, proj)

    return _remote_listdir


async def merged_local_remote_listdir(
        local_listdir_fn: Callable[[Path], Awaitable[list[Observation]]],
        remote_listdir_fn: Callable[[Path], Awaitable[list[Observation]]],
        proj: OldLocalProject,
        path: Path,
) -> list[Observation]:
    """
    Merge local and remote directory listings, prioritizing local entries.
    """
    local_entries = await local_listdir_fn(path)
    project_path = projects.local_to_remote_project_path(Path(proj.local_path), path)
    remote_entries = await remote_listdir_fn(project_path)

    local_by_name = {obs.name: obs for obs in local_entries}
    remote_by_name = {obs.name: obs for obs in remote_entries}

    merged: list[Observation] = []

    names = set(local_by_name) | set(remote_by_name)
    for name in sorted(names, key=lambda s: s.lower()):
        local_obs = local_by_name.get(name)
        remote_obs = remote_by_name.get(name)
        merged.append(
            Observation(
                local_path=local_obs.local_path if local_obs else None,
                remote_path=project_path / name,
                file_record=None,
                local_entry=local_obs.local_entry if local_obs else None,
                remote_entry=remote_obs.remote_entry if remote_obs else None,
            )
        )

    return merged


def make_merged_listdir_func(proj: OldLocalProject) -> ListDirFunc:
    """
    Create a merged listdir function for a given project.
    """
    remote_listdir_fn = make_remote_listdir_func(proj)

    async def _merged_listdir(path: Path) -> list[Observation]:
        return await merged_local_remote_listdir(local_listdir_fn=local_listdir,
                                                 remote_listdir_fn=remote_listdir_fn,
                                                 proj=proj,
                                                 path=path)

    return _merged_listdir


def _default_files_to_ignore(path: Path) -> bool:
    name = path.name
    return name == ".DS_Store" or name == ".mc"


def make_ignore_func(ignore_parser, root: Path) -> IgnoreFunc:
    def _ignore_func(path: Path, is_dir: bool) -> bool:
        rel = path.relative_to(root).as_posix()
        if is_dir:
            return ignore_parser.match(path.name) or ignore_parser.match(rel + "/")
        else:
            return ignore_parser.match(path.name) or ignore_parser.match(rel)

    return _ignore_func
