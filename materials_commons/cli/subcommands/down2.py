import argparse
import asyncio
from pathlib import Path

from materials_commons.cli.filedb import FileIndexDB, to_project_db_path
from materials_commons.cli.models import LocalProject
from materials_commons.cli.reconcile2 import AsyncReconciler, FileEntry

from materials_commons.cli.server import projects
from materials_commons.cli.server.downloader.file_download_manager import FileDownloadManager
from materials_commons.cli.user_config import Config
from materials_commons.cli.walk import async_walk


class Downloader:
    def __init__(self, proj: LocalProject, db: FileIndexDB):
        self.proj = proj
        self.db = db
        self.queue = asyncio.Queue()
        self.config = Config()
        self.file_download_manager = FileDownloadManager(send_queue=self.queue,
                                                         client_id=self.config.client_uuid,
                                                         mcurl=self.config.default_remote.mcurl,
                                                         apitoken=self.config.default_remote.mcapikey,
                                                         max_concurrent=3)
        self.async_reconciler = AsyncReconciler(db=db, proj=proj, recompute_checksum=True)

    async def download_file(self, path: str):
        # Turn this into a FileEntry and then download it
        pass

    async def download_dir(self, path: str, recursive: bool = False, ignore_fn=None):
        async for current_path, entries in self.async_reconciler.walk(path=path, recursive=recursive,
                                                                      ignore_fn=ignore_fn):
            for entry in entries.values():
                await self._download_file_entry(entry)

    async def _download_file_entry(self, entry: FileEntry):
        if entry.file_decision is not None:
            decision = entry.file_decision
            if decision.action == 'download' and entry.remote_entry.mime_type != 'directory':
                # Create the path to download to
                download_to = projects.remote_to_local_project_path(proj_base=Path(self.proj.local_path),
                                                                    remote_path=entry.remote_entry.path)
                # Check to make sure there isn't a directory with the same name
                if entry.local_entry is None:
                    await self.file_download_manager.download_file(self.proj.id, entry.remote_entry.id,
                                                                   download_to.as_posix())
                elif not entry.local_entry.is_dir:
                    await self.file_download_manager.download_file(self.proj.id, entry.remote_entry.id,
                                                                   download_to.as_posix())
                else:
                    return  # let user know why?
                print(f"Downloading {entry.remote_entry.name} to {download_to} ({entry.remote_entry.size} bytes)")


def make_parser():
    parser = argparse.ArgumentParser(
        description='Download files from Materials Commons',
        usage='mc down2 [-r] <pathspec> [<pathspec> ...]',
        prog='mc down2')

    parser.add_argument('paths', nargs='*', default=None, help='Files or directories')
    parser.add_argument('-r', '--recursive', action='store_true', help='Download directories recursively')
    return parser


def down2_subcommand(argv, working_dir):
    parser = make_parser()
    args = parser.parse_args(argv)
    asyncio.run(down2_subcommand_async(args, working_dir))


async def down2_subcommand_async(args, working_dir):
    proj = await projects.get_local_project(working_dir)
    db = await FileIndexDB.create(to_project_db_path(proj.local_path))
    config = Config()
    if proj is None:
        print("Error: Not in a Materials Commons project directory")
        return

    queue = asyncio.Queue()
    file_download_manager = FileDownloadManager(send_queue=queue,
                                                client_id=config.client_uuid,
                                                mcurl=config.default_remote.mcurl,
                                                apitoken=config.default_remote.mcapikey,
                                                max_concurrent=3)
    download_workers = await file_download_manager.start_workers()

    for path in args.paths:
        is_dir = await projects.is_dir(db, proj, path)
        if is_dir:
            await download_dir(path, recursive=args.recursive, ignore_fn=None)
        else:
            await download_file()


async def download_dir(path: str, recursive: bool = False, ignore_fn=None):
    async for current_path, entries in async_walk(path=path, recursive=recursive, ignore_fn=ignore_fn):
        pass


async def download_file():
    pass
