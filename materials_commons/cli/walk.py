from dataclasses import dataclass
from pathlib import Path
from typing import AsyncIterator, Callable, Optional, Awaitable
import asyncio
import os


@dataclass
class DirEntryInfo:
    path: Path
    name: str
    is_dir: bool
    is_file: bool
    is_symlink: bool


IgnoreFunc = Callable[[Path, bool], bool]
VisitorFunc = Callable[[Path, list[DirEntryInfo]], Awaitable[None]]


async def async_listdir(path: str | Path) -> list[DirEntryInfo]:
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
                entry.is_dir()
                items.append(
                    DirEntryInfo(
                        path=Path(entry.path),
                        name=entry.name,
                        is_dir=entry.is_dir(follow_symlinks=False),
                        is_file=entry.is_file(follow_symlinks=False),
                        is_symlink=entry.is_symlink(),
                    )
                )
        return items

    return await asyncio.to_thread(_scan)


async def async_walk(
        path: str | Path,
        recursive: bool = True,
        ignore_fn: Optional[IgnoreFunc] = None,
) -> AsyncIterator[tuple[Path, list[DirEntryInfo]]]:
    """Asynchronously walk a directory tree, yielding DirEntryInfo objects for each directory and file.

    Args:
        path: The root directory to start the walk from.
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

        entries = await async_listdir(current)
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


async def async_walk_visit(
        root: str | Path, 
        visitor_fn: VisitorFunc, 
        recursive: bool = True, 
        ignore_fn: Optional[IgnoreFunc] = None
) -> None:
    """Visit directories and files asynchronously using async_walk and a visitor function.
    
    Args:
        root: The root directory to start the walk from.
        visitor_fn: The visitor function to call for each directory and its entries.
        recursive: Whether to recursively visit subdirectories.
        ignore_fn: An optional function to ignore directories or files.
    """
    async for path, entries in async_walk(root, recursive, ignore_fn):
        await visitor_fn(path, entries)

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
