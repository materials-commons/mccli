import argparse
import asyncio
import os
from pathlib import Path

import igittigitt

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.server import projects
from materials_commons.cli.server.db.db_manager import DBManager
from materials_commons.cli.server.indexer.file_index_manager import FileIndexManager
from materials_commons.cli.server.project_filedbs import ProjectFileDBs
from materials_commons.cli.walk import async_walk, make_ignore_func


def make_parser():
    mc_scan_usage = 'mc scan [--list]'
    parser = argparse.ArgumentParser(
        description='Scan a project and build a database of current files and directories. This speeds uploads by storing state on the file.',
        usage=mc_scan_usage,
        prog='mc scan')

    parser.add_argument('--list', action='store_true', help='List scanned files and directories.')

    return parser


async def scan_files_async(project_root, mc_path, file_index_queue, ignore_parser, proj):
    """Scan files and directories in a project asynchronously"""

    loop = asyncio.get_event_loop()

    ignore_fn = make_ignore_func(ignore_parser, project_root)

    async for path, entries in async_walk(path=project_root, recursive=True, ignore_fn=ignore_fn):
        remote_dir = projects.local_to_remote_project_path(Path(project_root), Path(path))
        # print(f"remote_dir: {remote_dir}")
        remote_entries = await projects.list_remote_project_dir_by_path(proj.remote, proj.id, remote_dir.as_posix())
        for entry in entries:
            if entry.is_dir:
                continue
            remote_entry_path = remote_dir / entry.name
            remote_entry = remote_entries.get(remote_entry_path.as_posix(), None)
            # if remote_entry is None:
            #     print(f"Remote entry not found for {remote_entry_path}, skipping")
            #     continue
            # print(f"Remote entry found: {remote_entry_path}")
            print(f"Indexing {remote_entry_path}, {entry.path}")
            # await file_index_queue.put((entry.path, remote_entry_path, proj.id))

    # print(f"mc_path: {mc_path}")
    # print(f"local_path: {project_root}")
    # def walk_and_enqueue():
    #     for root, dirs, files in os.walk(project_root):
    #         print(f"root: {root}")
    #
    #         current_dir = os.path.basename(root)
    #         if current_dir == ".mc":
    #             continue
    #         remote_dir = projects.local_to_remote_project_path(Path(project_root), Path(root))
    #         print(f"remote_dir: {remote_dir}")
    #         remote_entries = asyncio.run_coroutine_threadsafe(projects.list_remote_project_dir_by_path(proj.remote, proj.id, remote_dir.as_posix()), loop).result()
    #         for filename in files:
    #             if filename == ".DS_Store":
    #                 continue
    #             if ignore_parser.match(filename):
    #                 continue
    #             file_path = os.path.join(root, filename)
    #             # Calculate relative path and construct mc_path
    #             relative = Path(file_path).relative_to(project_root)
    #             file_mc_path = str(Path(mc_path) / relative)
    #             print(f"Looking up remote entry: {file_mc_path}")
    #             remote_entry = remote_entries.get(file_mc_path, None)
    #             if remote_entry is None:
    #                 print(f"Remote entry not found for {file_mc_path}, skipping")
    #                 continue
    #             print(f"Remote entry found: {file_mc_path}")
    #             # print(f"Indexing {file_mc_path}, {file_path}")
    #             # asyncio.run_coroutine_threadsafe(file_index_queue.put((file_path, file_mc_path, project_id)), loop).result()
    #
    # await asyncio.to_thread(walk_and_enqueue)


async def scan_subcommand_async(argv, working_dir):
    """Builds and populates the file index database for project scan; skips ignored directories and files"""
    proj = clifuncs.make_local_project(working_dir)
    proj.remote.raise_exception = False

    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore", add_default_patterns=False)
    project_file_dbs = ProjectFileDBs()
    file_index_queue = asyncio.Queue()
    db_queue = asyncio.Queue()

    file_index_manager = FileIndexManager(project_file_dbs, db_queue, file_index_queue)
    file_index_workers = await file_index_manager.start_workers()

    db_manager = DBManager(db_queue, project_file_dbs)
    db_workers = await db_manager.start_workers()

    await scan_files_async(proj.local_path, proj.local_path, file_index_queue, ignore_parser, proj)

    # local_abspaths = treefuncs.clipaths_to_local_abspaths(proj.local_path, [proj.local_path], proj.local_path)
    # local_abspaths = treefuncs.filter_local_abspaths(proj.local_path, local_abspaths, working_dir)
    # mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, local_abspaths, working_dir)
    # for local_path, mc_path in zip(local_abspaths, mcpaths):
    #     await scan_files_async(local_path, mc_path, file_index_queue, ignore_parser, proj)

    # Wait for all tasks to complete
    await file_index_queue.join()
    file_index_manager.stop_workers()
    for worker in file_index_workers:
        try:
            await worker
        except asyncio.CancelledError:
            pass

    await db_queue.join()
    db_manager.stop_workers()
    for worker in db_workers:
        try:
            await worker
        except asyncio.CancelledError:
            pass

    await project_file_dbs.close_dbs()


def scan_subcommand(argv, working_dir):
    """Runs the scan subcommand asynchronously"""
    parser = make_parser()
    args = parser.parse_args(argv)
    if args.list:
        print("Not implemented yet")
        return

    asyncio.run(scan_subcommand_async(argv, working_dir))
