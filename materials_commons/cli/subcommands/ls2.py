import argparse
import asyncio
import os

from tabulate import tabulate

from materials_commons.cli.filedb import FileIndexDB, to_project_db_path
from materials_commons.cli.old.functions import humanize, format_time
from materials_commons.cli.models import LSAction, LSEntry
from materials_commons.cli.async_reconciler import AsyncReconciler
from materials_commons.cli.server import projects
from materials_commons.cli.walk import make_merged_listdir_func


class LSTable:
    def __init__(self):
        self._full_headers = ['l_updated_at', 'l_size', 'l_type', 'l_id', 'r_updated_at', 'r_size',
                              'r_type', 'r_id', 'eq', 'name']
        self._action_headers = ['name', 'l_type', 'r_type', 'local/remote', 'action', 'reason']
        self._action_rows: list[LSAction] = []
        self._full_rows: list[LSEntry] = []

    def add_row(self, entry: LSAction | LSEntry):
        if isinstance(entry, LSAction):
            self._action_rows.append(entry)
        else:
            self._full_rows.append(entry)

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

    async_reconciler = AsyncReconciler(db=db, proj=proj, recompute_checksum=args.checksum)
    which_class = LSAction if args.action else LSEntry
    listdir_fn = make_merged_listdir_func(proj)
    for path in args.paths:
        async for current_path, path_entries in async_reconciler.walk(path=path, listdir_fn=listdir_fn,
                                                                      recursive=False, ignore_fn=None):
            for entry_name in sorted(path_entries):
                entry = path_entries[entry_name]
                if entry.exception:
                    print(f"Error reconciling file: {entry.exception}")
                    continue
                lstable.add_row(which_class.from_file_state(entry))
            lstable.print_table()
