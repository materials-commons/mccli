import argparse
import asyncio
import os

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.tree_functions as treefuncs
from pathlib import Path
import igittigitt
from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.server.indexer.file_index_manager import FileIndexManager


def make_parser():
    mc_scan_usage = 'mc scan'
    parser = argparse.ArgumentParser(
        description='Scan a project and build a database of current files and directories. This speeds uploads by storing state on the file.',
        usage=mc_scan_usage,
        prog='mc scan')

    return parser

async def scan_files_async(local_path, mc_path, file_index_queue, ignore_parser):
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
                asyncio.run_coroutine_threadsafe(file_index_queue.put((file_path, file_mc_path)), loop).result()

    await asyncio.to_thread(walk_and_enqueue)

async def scan_subcommand_async(argv, working_dir):
    """Builds and populates the file index database for project scan; skips ignored directories and files"""
    parser = make_parser()
    parser.parse_args(argv)

    proj = clifuncs.make_local_project(working_dir)

    filedb = FileIndexDB(db_path=Path(proj.local_path) / ".mc" / "mc2.sqlite")

    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore",
                                   add_default_patterns=False)

    file_index_queue = asyncio.Queue()
    file_index_manager = FileIndexManager(filedb, file_index_queue)

    workers = await file_index_manager.start_workers()

    local_abspaths = treefuncs.clipaths_to_local_abspaths(proj.local_path, [proj.local_path], proj.local_path)
    local_abspaths = treefuncs.filter_local_abspaths(proj.local_path, local_abspaths, working_dir)
    mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, local_abspaths, working_dir)
    for local_path, mc_path in zip(local_abspaths, mcpaths):
        await scan_files_async(local_path, mc_path, file_index_queue, ignore_parser)

    # Wait for all tasks to complete
    await file_index_queue.join()
    await file_index_manager.stop_workers()
    print("called stop_workers")
    for worker in workers:
        try:
            await worker
        except asyncio.CancelledError:
            pass

def scan_subcommand(argv, working_dir):
    """Runs the scan subcommand asynchronously"""
    asyncio.run(scan_subcommand_async(argv, working_dir))