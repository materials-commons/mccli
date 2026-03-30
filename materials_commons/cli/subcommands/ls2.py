import argparse
import asyncio
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

from materials_commons.api import models
from materials_commons.cli.filedb import FileIndexDB, to_project_db_path

from materials_commons.cli.server import projects

from materials_commons.cli.walk import async_walk, DirEntryInfo
import materials_commons.cli.functions as clifuncs



def make_parser():
    parser = argparse.ArgumentParser(
        description='List local and remote files and directories',
        usage="mc ls2 [--checksum] [<pathspec>...]",
        prog='mc ls2'
    )
    parser.add_argument('paths', nargs='*', default=[os.getcwd()], help='Files or directories')
    parser.add_argument('--checksum', action="store_true", default=False, help='Calculate MD5 checksum for local files')
    return parser


def ls2_subcommand(argv, working_dir):
    parser = make_parser()
    args = parser.parse_args(argv)
    asyncio.run(ls2_subcommand_async(args, working_dir))


def show_dir_entry(entry, param, checksum):
    pass


@dataclass
class FileEntry:
    local_entry: Optional[DirEntryInfo]
    remote_entry: Optional[models.File]

async def ls2_subcommand_async(args, working_dir):
    print("ls2 async")

    proj = await projects.get_local_project(working_dir)
    db = await FileIndexDB.create(to_project_db_path(proj.local_path))

    for path in args.paths:
        path_entries = {}
        async for current_path, entries in async_walk(path=path, recursive=False, ignore_fn=None):
            remote_dir = projects.local_to_remote_project_path(Path(proj.local_path), Path(current_path))
            remote_entries = await projects.list_remote_project_dir_by_path(proj.remote, proj.id, remote_dir.as_posix())

            # First, we go through all the remote entries and add them to the path_entries dict
            for remote_entry in remote_entries.values():
                path_entries[remote_entry.name] = FileEntry(remote_entry=remote_entry, local_entry=None)

            # Next, we go through all the local entries. If that local entry exists, then the remote and the
            # local entries are linked. Otherwise, we have a local only entry.
            for entry in entries:
                found_remote_entry = path_entries.get(entry.name, None)
                if found_remote_entry:
                    found_remote_entry.local_entry = entry
                else:
                    path_entries[entry.name] = FileEntry(local_entry=entry, remote_entry=None)

        # At this point path_entries contains entries in one of 3 states:
        # 1. Both remote and local entries exist
        # 2. Only remote entry exists
        # 3. Only local entry exists
        for entry_name in sorted(path_entries):
            entry = path_entries[entry_name]
            if entry.local_entry and entry.remote_entry:
                print(f"local and remote: {entry.local_entry.name}")
            elif entry.local_entry:
                print(f"local only: {entry.local_entry.name}")
            else:
                print(f"remote only: {entry.remote_entry.name}")