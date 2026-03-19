import argparse
import asyncio
import os

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.tree_functions as treefuncs
from pathlib import Path
import igittigitt
from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.server.db.db_manager import DBManager
from materials_commons.cli.server.indexer.file_index_manager import FileIndexManager
from materials_commons.cli.server.project_filedbs import ProjectFileDBs


def make_parser():
    mc_scan_usage = 'mc scan [--list]'
    parser = argparse.ArgumentParser(
        description='Scan a project and build a database of current files and directories. This speeds uploads by storing state on the file.',
        usage=mc_scan_usage,
        prog='mc scan')

    parser.add_argument('--list', action='store_true', help='List scanned files and directories.')

    return parser

async def scan_files_async(local_path, mc_path, file_index_queue, ignore_parser, project_id):
    """Scan files and directories in a project asynchronously"""

    loop = asyncio.get_event_loop()

    def walk_and_enqueue():

        for root, dirs, files in os.walk(local_path):
            current_dir = os.path.basename(root)
            if current_dir == ".mc":
                continue
            for filename in files:
                if filename == ".DS_Store":
                    continue
                if ignore_parser.match(filename):
                    continue
                file_path = os.path.join(root, filename)
                # Calculate relative path and construct mc_path
                relative = Path(file_path).relative_to(local_path)
                file_mc_path = str(Path(mc_path) / relative)
                # print(f"Indexing {file_mc_path}, {file_path}")
                asyncio.run_coroutine_threadsafe(file_index_queue.put((file_path, file_mc_path, project_id)), loop).result()

    await asyncio.to_thread(walk_and_enqueue)

async def scan_subcommand_async(argv, working_dir):
    """Builds and populates the file index database for project scan; skips ignored directories and files"""
    proj = clifuncs.make_local_project(working_dir)

    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore",
                                   add_default_patterns=False)
    project_file_dbs = ProjectFileDBs()
    file_index_queue = asyncio.Queue()
    db_queue = asyncio.Queue()

    file_index_manager = FileIndexManager(project_file_dbs, db_queue, file_index_queue)
    file_index_workers = await file_index_manager.start_workers()

    db_manager = DBManager(db_queue, project_file_dbs)
    db_workers = await db_manager.start_workers()

    local_abspaths = treefuncs.clipaths_to_local_abspaths(proj.local_path, [proj.local_path], proj.local_path)
    local_abspaths = treefuncs.filter_local_abspaths(proj.local_path, local_abspaths, working_dir)
    mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, local_abspaths, working_dir)
    for local_path, mc_path in zip(local_abspaths, mcpaths):
        await scan_files_async(local_path, mc_path, file_index_queue, ignore_parser, proj.id)

    # Wait for all tasks to complete
    await file_index_queue.join()
    await file_index_manager.stop_workers()
    for worker in file_index_workers:
        try:
            await worker
        except asyncio.CancelledError:
            pass

    await db_queue.join()
    await db_manager.stop_workers()
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