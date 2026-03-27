import argparse
import asyncio
from asyncio import Task
from pathlib import Path

import igittigitt

import materials_commons.cli.functions as clifuncs
from materials_commons.cli.server import projects
from materials_commons.cli.server.db.db_manager import DBManager
from materials_commons.cli.server.indexer.file_index_manager import FileIndexManager
from materials_commons.cli.server.project_filedbs import ProjectFileDBs
from materials_commons.cli.walk import async_walk, make_ignore_func


def scan_subcommand(argv, working_dir):
    """Runs the scan subcommand asynchronously"""
    parser = make_parser()
    args = parser.parse_args(argv)
    if args.list:
        print("Not implemented yet")
        return

    asyncio.run(scan_subcommand_async(argv, working_dir))


def make_parser():
    mc_scan_usage = 'mc scan [--list]'
    parser = argparse.ArgumentParser(
        description='Scan a project and build a database of current files and directories. This speeds uploads by storing state on the file.',
        usage=mc_scan_usage,
        prog='mc scan')

    parser.add_argument('--list', action='store_true', help='List scanned files and directories.')

    return parser


async def scan_subcommand_async(argv, working_dir):
    """Builds and populates the file index database for project scan; skips ignored directories and files"""
    proj = clifuncs.make_local_project(working_dir)
    proj.remote.raise_exception = False

    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore", add_default_patterns=False)
    project_file_dbs = ProjectFileDBs()
    file_index_queue = asyncio.Queue()
    db_queue = asyncio.Queue()

    # Hack for now: preload the existing project_id until we make projectFileDBs async safe
    await project_file_dbs.get_filedb(proj.id)

    file_index_manager = FileIndexManager(project_file_dbs, db_queue, file_index_queue)
    file_index_workers = await file_index_manager.start_workers()

    db_manager = DBManager(db_queue, project_file_dbs)
    db_workers = await db_manager.start_workers()

    await scan_files_async(proj.local_path, proj.local_path, file_index_queue, ignore_parser, proj)

    # Wait for all tasks to complete
    await stop_workers(file_index_queue, file_index_workers, file_index_manager)
    await stop_workers(db_queue, db_workers, db_manager)
    await project_file_dbs.close_dbs()


async def stop_workers(queue: asyncio.Queue, workers: list[Task[None]], manager) -> None:
    """Stop all workers and wait for them to finish"""
    await queue.join()
    manager.stop_workers()
    for worker in workers:
        try:
            await worker
        except asyncio.CancelledError:
            pass


async def scan_files_async(project_root, mc_path, file_index_queue, ignore_parser, proj):
    """Scan files and directories in a project asynchronously"""
    ignore_fn = make_ignore_func(ignore_parser, project_root)
    async for path, entries in async_walk(path=project_root, recursive=True, ignore_fn=ignore_fn):
        remote_dir = projects.local_to_remote_project_path(Path(project_root), Path(path))
        remote_entries = await projects.list_remote_project_dir_by_path(proj.remote, proj.id, remote_dir.as_posix())
        for entry in entries:
            if entry.is_dir:
                continue
            remote_entry_path = remote_dir / entry.name
            remote_entry = remote_entries.get(remote_entry_path.as_posix(), None)
            await file_index_queue.put((entry.path, remote_entry_path.as_posix(), remote_entry, proj.id))
