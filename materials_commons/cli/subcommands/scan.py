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

def scan_subcommand(argv, working_dir):
    parser = make_parser()
    parser.parse_args(argv)

    ignore_parser = igittigitt.IgnoreParser()

    filedb = FileIndexDB(db_path=Path(working_dir) / ".mc" / "mc2.sqlite")
    proj = clifuncs.make_local_project(working_dir)
    file_index_queue = asyncio.Queue()
    file_index_manager = FileIndexManager(filedb, file_index_queue)
    file_index_manager.start_workers()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore", add_default_patterns=False)
    print(f"Scanning {proj.local_path}...")
    local_abspaths = treefuncs.clipaths_to_local_abspaths(proj.local_path, [proj.local_path], proj.local_path)
    local_abspaths = treefuncs.filter_local_abspaths(proj.local_path, local_abspaths, working_dir)
    mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, local_abspaths, working_dir)
    for local_path, mc_path in zip(local_abspaths, mcpaths):
        # print(f"local_path: {local_path}, mc_path: {mc_path}")
        for root, dirs, files in os.walk(local_path):
            # print(f"root = {root}")
            current_dir = os.path.basename(root)
            if current_dir == ".mc":
                continue
            # print(f"current_dir = {current_dir}")
            for filename in files:
                if filename == ".DS_Store":
                    continue
                if ignore_parser.match(filename):
                    continue
                # print(f"filename = {filename}, root = {root}")
                file_path = os.path.join(root, filename)
                # Calculate relative path and construct mc_path
                relative = Path(file_path).relative_to(local_path)
                file_mc_path = str(Path(mc_path) / relative)
                # print(f"Indexing {file_mc_path}, {file_path}")
                file_index_queue.put_nowait((file_path, file_mc_path))





