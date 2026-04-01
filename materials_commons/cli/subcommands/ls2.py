import argparse
import asyncio
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

from materials_commons.api import models
from tabulate import tabulate

from materials_commons.cli.filedb import FileIndexDB, to_project_db_path
from materials_commons.cli.functions import humanize, format_time
from materials_commons.cli.reconcile2 import observe_and_reconcile, FileDecision
from materials_commons.cli.server import projects
from materials_commons.cli.walk import async_walk, DirEntryInfo


@dataclass
class FileEntry:
    local_entry: Optional[DirEntryInfo]
    remote_entry: Optional[models.File]
    file_decision: Optional[FileDecision]


class LSTable:
    def __init__(self):
        self._full_headers = ['l_updated_at', 'l_size', 'l_type', 'l_id', 'r_updated_at', 'r_size',
                              'r_type', 'r_id', 'eq', 'name']
        self._action_headers = ['name', 'l_type', 'r_type', 'local/remote', 'action', 'reason']
        self._rows = []

    def add_full_row(self, entry: FileEntry):
        if entry.local_entry and entry.remote_entry:
            self.add_local_and_remote_row(entry)
        elif entry.local_entry:
            self.add_local_only_row(entry)
        else:
            self.add_remote_only_row(entry)

    def add_action_row(self, entry: FileEntry):
        action = entry.file_decision.action
        if action == "db_update":
            action = "preserve"

        reason = entry.file_decision.reason

        if entry.local_entry and entry.remote_entry:
            l_type = "D" if entry.local_entry.is_dir else "F"
            r_type = "D" if entry.remote_entry.mime_type == "directory" else "F"
            if r_type == "D" and l_type == "D":
                reason = "local and remote directories exist"
                action = "skip"
            self._rows.append(
                [entry.local_entry.name, l_type, r_type, "L/R", action, reason])
        elif entry.local_entry:
            l_type = "D" if entry.local_entry.is_dir else "F"
            r_type = "-"
            if l_type == "F":
                action = "upload"
            self._rows.append(
                [entry.local_entry.name, l_type, r_type, "L", action, reason])
        else:
            l_type = "-"
            r_type = "D" if entry.remote_entry.mime_type == "directory" else "F"
            self._rows.append(
                [entry.remote_entry.name, l_type, r_type, "R", action, reason])

    def add_local_and_remote_row(self, entry: FileEntry):
        checksums_equal = entry.file_decision.updated_record.local_checksum == entry.remote_entry.checksum
        mtime = entry.file_decision.updated_record.local_mtime_ns / 1_000_000_000
        entry = [
            format_time(mtime, fmt="%b %d  %Y"),  # l_updated_at
            humanize(entry.file_decision.updated_record.local_size),  # l_size
            "D" if entry.local_entry.is_dir else "F",  # l_type
            entry.file_decision.updated_record.remote_file_id,  # l_id
            format_time(entry.remote_entry.updated_at, fmt="%b %d  %Y"),  # r_updated_at
            humanize(entry.remote_entry.size),  # r_size
            "D" if entry.remote_entry.mime_type == "directory" else "F",  # r_type
            entry.remote_entry.id,  # r_id
            "eq" if checksums_equal else "-",  # eq
            entry.local_entry.name,  # name
        ]
        self._rows.append(entry)

    def add_local_only_row(self, entry: FileEntry):
        local_id = "-"
        if entry.file_decision.updated_record.remote_file_id is not None:
            local_id = entry.file_decision.updated_record.remote_file_id
        mtime = entry.file_decision.updated_record.local_mtime_ns / 1_000_000_000
        entry = [
            format_time(mtime, fmt="%b %d  %Y"),  # l_updated_at
            humanize(entry.file_decision.updated_record.local_size),  # l_size
            "-",  # l_type
            local_id,  # l_id
            "-",  # r_updated_at
            "-",  # r_size
            "-",  # r_type
            "-",  # r_id
            "-",  # eq
            entry.local_entry.name,  # name
        ]
        self._rows.append(entry)

    def add_remote_only_row(self, entry: FileEntry):

        r_type = "D" if entry.remote_entry.mime_type == "directory" else "F"
        entry = [
            "-",  # l_updated_at
            "-",  # l_size
            "-",  # l_type
            "-",  # l_id
            format_time(entry.remote_entry.updated_at, fmt="%b %d  %Y"),  # r_updated_at
            humanize(entry.remote_entry.size),  # r_size
            r_type,  # r_type
            entry.remote_entry.id,  # r_id
            "-",  # eq
            entry.remote_entry.name,  # name
        ]
        self._rows.append(entry)

    def print_full_table(self):
        print(tabulate(self._rows, headers=self._full_headers))

    def print_action_table(self):
        print(tabulate(self._rows, headers=self._action_headers))


def make_parser():
    parser = argparse.ArgumentParser(
        description='List local and remote files and directories',
        usage="mc ls2 [--checksum] [--action][<pathspec>...]",
        prog='mc ls2'
    )
    parser.add_argument('paths', nargs='*', default=[os.getcwd()], help='Files or directories')
    parser.add_argument('--checksum', action="store_true", default=False, help='Calculate MD5 checksum for local files')
    parser.add_argument('--action', action="store_true", default=False, help='Show action and reason for file decision')
    return parser


def ls2_subcommand(argv, working_dir):
    parser = make_parser()
    args = parser.parse_args(argv)
    asyncio.run(ls2_subcommand_async(args, working_dir))


async def ls2_subcommand_async(args, working_dir):
    proj = await projects.get_local_project(working_dir)
    db = await FileIndexDB.create(to_project_db_path(proj.local_path))
    lstable = LSTable()

    for path in args.paths:
        path_entries = {}
        async for current_path, entries in async_walk(path=path, recursive=False, ignore_fn=None):
            remote_dir = projects.local_to_remote_project_path(Path(proj.local_path), Path(current_path))
            remote_entries = await projects.list_remote_project_dir_by_path(proj.remote, proj.id, remote_dir.as_posix())

            # First, we go through all the remote entries and add them to the path_entries dict
            for remote_entry in remote_entries.values():
                path_entries[remote_entry.name] = FileEntry(remote_entry=remote_entry, local_entry=None,
                                                            file_decision=None)

            # Next, we go through all the local entries. If that local entry exists, then the remote and the
            # local entries are linked. Otherwise, we have a local only entry.
            for entry in entries:
                # TODO: Optimize so we look up all database entries in one query. For now just do individual queries
                found_remote_entry = path_entries.get(entry.name, None)
                remote_entry = found_remote_entry.remote_entry if found_remote_entry else None
                project_path = projects.local_to_remote_project_path(Path(proj.local_path), entry.path)
                file_decision = await observe_and_reconcile(db=db,
                                                            project_path=project_path.as_posix(),
                                                            file_path=entry.path.as_posix(),
                                                            remote_entry=remote_entry,
                                                            recompute_checksum=args.checksum)
                if found_remote_entry:
                    found_remote_entry.local_entry = entry
                    found_remote_entry.file_decision = file_decision
                else:
                    path_entries[entry.name] = FileEntry(local_entry=entry, remote_entry=None,
                                                         file_decision=file_decision)

            # We've run observe_and_reconcile on all local entries. Now we need to do that on all
            # remote only entries.
            for entry_name in path_entries:
                entry = path_entries[entry_name]
                if not entry.file_decision:
                    remote_path = Path(entry.remote_entry.directory.path) / entry.remote_entry.name
                    file_path = projects.remote_to_local_project_path(proj_base=Path(proj.local_path),
                                                                      remote_path=remote_path)
                    file_decision = await observe_and_reconcile(db=db,
                                                                project_path=entry.remote_entry.directory.path,
                                                                file_path=file_path.as_posix(),
                                                                remote_entry=entry.remote_entry,
                                                                recompute_checksum=args.checksum)
                    entry.file_decision = file_decision

        # At this point path_entries contains entries in one of 3 states:
        # 1. Both remote and local entries exist
        # 2. Only remote entry exists
        # 3. Only local entry exists

        if args.action:
            for entry_name in sorted(path_entries):
                entry = path_entries[entry_name]
                lstable.add_action_row(entry)
            lstable.print_action_table()
        else:
            for entry_name in sorted(path_entries):
                entry = path_entries[entry_name]
                lstable.add_full_row(entry)
            lstable.print_full_table()
