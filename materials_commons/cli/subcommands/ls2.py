import argparse
import asyncio
import os

from tabulate import tabulate

from materials_commons.cli.filedb import FileIndexDB, to_project_db_path
from materials_commons.cli.functions import humanize, format_time
from materials_commons.cli.models import LSAction, LSEntry, FileEntry
from materials_commons.cli.reconcile2 import AsyncReconciler
from materials_commons.cli.server import projects
from materials_commons.cli.walk import async_listdir


class LSTable:
    def __init__(self):
        self._full_headers = ['l_updated_at', 'l_size', 'l_type', 'l_id', 'r_updated_at', 'r_size',
                              'r_type', 'r_id', 'eq', 'name']
        self._action_headers = ['name', 'l_type', 'r_type', 'local/remote', 'action', 'reason']
        self._action_rows: list[LSAction] = []
        self._full_rows: list[LSEntry] = []

    def add_action_row(self, entry: FileEntry):
        self._action_rows.append(LSAction.from_file_entry(entry))

    def add_full_row(self, entry: FileEntry):
        self._full_rows.append(LSEntry.from_file_entry(entry))

    def print_table(self):
        if self._action_rows:
            self._print_action_table()
        else:
            self._print_full_table()

    def _print_full_table(self):
        # Headers:
        #   ['l_updated_at', 'l_size', 'l_type', 'l_id', 'r_updated_at', 'r_size', 'r_type', 'r_id', 'eq', 'name']
        rows = [[
            "-" if entry.l_updated_at is None else format_time(entry.l_updated_at, fmt="%b %d  %Y"),
            "-" if entry.l_size is None else humanize(entry.l_size),
            "-" if entry.l_type is None else entry.l_type,
            "-" if entry.l_id is None else entry.l_id,
            "-" if entry.r_updated_at is None else format_time(entry.r_updated_at, fmt="%b %d  %Y"),
            "-" if entry.r_size is None else humanize(entry.r_size),
            "-" if entry.r_type is None else entry.r_type,
            "-" if entry.r_id is None else entry.r_id,
            "-" if entry.eq is None else entry.eq,
            entry.name,
        ] for entry in self._full_rows]
        print(tabulate(rows, headers=self._full_headers))

    def _print_action_table(self):
        # Headers:
        #   ['name', 'l_type', 'r_type', 'local/remote', 'action', 'reason']
        rows = [[
            entry.name,
            entry.l_type,
            entry.r_type,
            entry.local_remote,
            entry.action,
            entry.reason,
        ] for entry in self._action_rows]
        print(tabulate(rows, headers=self._action_headers))


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

    async_reconciler = AsyncReconciler(db=db, proj=proj, recompute_checksum=args.checksum, listdir_fn=async_listdir)
    for path in args.paths:
        async for current_path, path_entries in async_reconciler.walk(path=path, recursive=False, ignore_fn=None):
            if args.action:
                for entry_name in sorted(path_entries):
                    entry = path_entries[entry_name]
                    lstable.add_action_row(entry)
            else:
                for entry_name in sorted(path_entries):
                    entry = path_entries[entry_name]
                    lstable.add_full_row(entry)
            lstable.print_table()
