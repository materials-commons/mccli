import argparse
import asyncio
import logging
from pathlib import Path

import igittigitt
from materials_commons.cli.async_reconciler import AsyncReconciler

from materials_commons.cli.local_project import LocalProject
from materials_commons.cli.requests import IndexRequest, DBWriteRequest
from materials_commons.cli.server import projects
from materials_commons.cli.server.service_container import ServiceContainer
from materials_commons.cli.server.service_runtime import ServiceRuntime
from materials_commons.cli.walk import async_walk, make_ignore_func, local_listdir

logger = logging.getLogger(__name__)


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


async def scan2_subcommand_async(args, working_dir):
    # Load project and set exception handling
    proj = LocalProject.load(working_dir)
    # proj.remote.raise_exception = False

    # Initialize database
    db = await proj.get_filedb()

    # Setup ignore parser
    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore", add_default_patterns=False)

    # Start services
    container = ServiceContainer.create(ws_url=args.ws_url)
    service_runtime = ServiceRuntime(container)
    await service_runtime.start(db_manager=True)
    async_reconciler = AsyncReconciler(db=db, proj=proj, reconcile_mode="status")
    try:
        async for current_path, path_entries in async_reconciler.walk(path=proj.local_path, listdir_fn=local_listdir,
                                                                      recursive=True, ignore_fn=None):
            for entry_name in sorted(path_entries):
                file_state = path_entries[entry_name]
                if file_state.exception:
                    logger.error(f"Error encountered while processing {entry_name}: {file_state.exception}")
                    continue
                if file_state.observation.local_path is None:
                    continue
                if file_state.observation.local_path.is_dir():
                    continue

                # We have a file, process it
                db_request = DBWriteRequest(project=proj, command="single",
                                            data=file_state.file_decision.updated_record)
                await container.db_queue.put(db_request)
    except Exception as e:
        return
    finally:
        await service_runtime.drain()
        await service_runtime.stop()
        await db.close()


async def scan_subcommand_async(argv, working_dir):
    """Builds and populates the file index database for project scan; skips ignored directories and files"""

    # Load project
    proj = LocalProject.load(working_dir)
    # proj.remote.raise_exception = False

    # Initialize database
    await proj.get_filedb()

    # Setup ignore parser
    ignore_parser = igittigitt.IgnoreParser()
    ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore", add_default_patterns=False)

    # Start services
    container = ServiceContainer.create()
    service_runtime = ServiceRuntime(container)
    await service_runtime.start(file_index_manager=True)

    try:
        # Run the scan
        await scan_files_async(container.file_index_queue, ignore_parser, proj)

        # Let services drain any in-progress work
        await service_runtime.drain()

    finally:
        # Shutdown services
        await service_runtime.stop()

        # Close database
        db = await proj.get_filedb()
        await db.close()


async def scan_files_async(file_index_queue: asyncio.Queue[IndexRequest],
                           ignore_parser: igittigitt.IgnoreParser,
                           proj: LocalProject) -> None:
    """Scan files and directories in a project asynchronously"""
    project_root = proj.local_path
    ignore_fn = make_ignore_func(ignore_parser, project_root)
    async for path, entries in async_walk(path=project_root, recursive=True, ignore_fn=ignore_fn,
                                          listdir_fn=local_listdir):
        remote_dir = projects.local_to_remote_project_path(Path(project_root), Path(path))
        remote_entries = await projects.list_remote_project_dir_by_path(proj.remote, proj.id, remote_dir.as_posix())
        for entry in entries:
            if entry.is_dir:
                continue
            remote_entry_path = remote_dir / entry.name
            remote_entry = remote_entries.get(remote_entry_path.as_posix(), None)
            index_request = IndexRequest(file_path=entry.path,
                                         project_path=remote_entry_path.as_posix(),
                                         remote_entry=remote_entry,
                                         project=proj)
            await file_index_queue.put(index_request)
