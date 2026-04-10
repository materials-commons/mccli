import asyncio
import os
from dataclasses import dataclass
from datetime import timezone
from pathlib import Path
from typing import AsyncIterator, Callable, Optional, Awaitable, Protocol

from materials_commons.api import models
from materials_commons.cli.server import projects

from materials_commons.cli.models import LocalProject


@dataclass(frozen=True)
class EntryStatInfo:
    size: int
    mtime_ns: int
    ctime_ns: int


class DirEntryInfo(Protocol):
    @property
    def path(self) -> Path: ...

    @property
    def name(self) -> str: ...

    @property
    def is_dir(self) -> bool: ...

    @property
    def is_file(self) -> bool: ...

    @property
    def is_symlink(self) -> bool: ...

    def stat(self) -> EntryStatInfo: ...


IgnoreFunc = Callable[[Path, bool], bool]
VisitorFunc = Callable[[Path, list[DirEntryInfo]], Awaitable[None]]
ListDirFunc = Callable[[Path], Awaitable[list[DirEntryInfo]]]


@dataclass
class LocalDirEntryInfo:
    entry: os.DirEntry

    @property
    def path(self) -> Path:
        return Path(self.entry.path)

    @property
    def name(self) -> str:
        return self.entry.name

    @property
    def is_dir(self) -> bool:
        return self.entry.is_dir(follow_symlinks=False)

    @property
    def is_file(self) -> bool:
        return self.entry.is_file(follow_symlinks=False)

    @property
    def is_symlink(self) -> bool:
        return self.entry.is_symlink()

    def stat(self) -> EntryStatInfo:
        sinfo = self.entry.stat()
        return EntryStatInfo(
            size=sinfo.st_size,
            mtime_ns=sinfo.st_mtime_ns,
            ctime_ns=sinfo.st_ctime_ns,
        )


@dataclass
class RemoteDirEntryInfo:
    remote_file: models.File

    @property
    def path(self) -> Path:
        return Path(self.remote_file.directory.path) / self.remote_file.name

    @property
    def name(self) -> str:
        return self.remote_file.name

    @property
    def is_dir(self) -> bool:
        return self.remote_file.mime_type == "directory"

    @property
    def is_file(self) -> bool:
        return self.remote_file.mime_type != "directory"

    @property
    def is_symlink(self) -> bool:
        return False

    def stat(self) -> EntryStatInfo:
        return EntryStatInfo(
            size=self.remote_file.size,
            ctime_ns=int(self.remote_file.created_at.replace(tzinfo=timezone.utc).timestamp() * 1_000_000_000),
            mtime_ns=int(self.remote_file.updated_at.replace(tzinfo=timezone.utc).timestamp() * 1_000_000_000),
        )


async def async_walk(
        path: str | Path,
        listdir_fn: ListDirFunc,
        recursive: bool = True,
        ignore_fn: Optional[IgnoreFunc] = None,
) -> AsyncIterator[tuple[Path, list[DirEntryInfo]]]:
    """Asynchronously walk a directory tree, yielding DirEntryInfo objects for each directory and file.

    Args:
        path: The root directory to start the walk from.
        listdir_fn: A function to list the contents of a directory.
        recursive: Whether to recursively visit subdirectories.
        ignore_fn: An optional function to ignore directories or files.

    Returns:
        An async iterator yielding tuples of (Path, list[DirEntryInfo]) for each directory visited.
        The list contains DirEntryInfo objects for each file and directory within the directory.
        Path is the current directory that list[DirEntryInfo] entries belong to.
        The iterator stops when all directories have been visited.
    """
    root = Path(path)
    stack = [root]

    while stack:
        current = stack.pop()
        if ignore_fn and ignore_fn(current, True):
            continue

        entries = await listdir_fn(current)

        filtered: list[DirEntryInfo] = []
        for entry in entries:
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


async def local_listdir(path: str | Path) -> list[DirEntryInfo]:
    """Asynchronously list the contents of a directory.

    Args:
        path: The directory path to create a list of entries of.
    """

    # scan encapsulates os.scandir() which is not async. We need a function to wrap it that we can
    # call asyncio.to_thread on to run it.
    def _scan():
        items = []
        with os.scandir(path) as entries:
            for entry in entries:
                items.append(LocalDirEntryInfo(entry))
        return items

    return await asyncio.to_thread(_scan)


async def remote_listdir(project_path: str | Path, proj: LocalProject) -> list[DirEntryInfo]:
    """Asynchronously list the contents of a remote directory.

    Args:
        project_path: The path within the project to list directory contents of.
        proj: The local project object representing the remote project.
    """
    entries = await asyncio.to_thread(proj.remote.list_directory_by_path, proj.id, project_path)
    return [RemoteDirEntryInfo(entry) for entry in entries]


def make_remote_listdir_func(proj: LocalProject) -> ListDirFunc:
    """
    Create an asynchronous directory listing function for a remote project.

    Args:
        proj: The local project object representing the remote project.

    Returns:
        An asynchronous function that lists directory contents of a remote project.
    """

    async def _remote_listdir(path: Path) -> list[DirEntryInfo]:
        return await remote_listdir(path, proj)

    return _remote_listdir


async def merged_local_remote_listdir(
        local_listdir_fn: Callable[[Path], Awaitable[list[DirEntryInfo]]],
        remote_listdir_fn: Callable[[Path], Awaitable[list[DirEntryInfo]]],
        proj: LocalProject,
        path: Path,
) -> list[DirEntryInfo]:
    """
    Merge local and remote directory listings, prioritizing local entries.
    """
    local_entries = await local_listdir_fn(path)
    project_path = projects.local_to_remote_project_path(Path(proj.local_path), path)
    remote_entries = await remote_listdir_fn(project_path)

    seen = {entry.name for entry in local_entries}

    merged = list(local_entries)
    for entry in remote_entries:
        key = entry.name
        if key not in seen:
            merged.append(entry)
            seen.add(key)

    return sorted(merged, key=lambda entry: entry.name.lower())


def make_merged_listdir_func(proj: LocalProject) -> ListDirFunc:
    """
    Create a merged listdir function for a given project.
    """
    remote_listdir_fn = make_remote_listdir_func(proj)

    async def _merged_listdir(path: Path) -> list[DirEntryInfo]:
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
