import asyncio
from asyncio import Task
from pathlib import Path

from materials_commons.cli.server import projects

from materials_commons.cli.async_reconciler import AsyncReconciler
from materials_commons.cli.filedb import FileIndexDB

from materials_commons.cli.models import LocalProject, FileState
from materials_commons.cli.reconcile2 import observe_and_reconcile_to_file_entry
from materials_commons.cli.server.downloader.file_download_manager import FileDownloadManager
from materials_commons.cli.user_config import Config
from materials_commons.cli.walk import local_listdir


class Downloader:
    def __init__(self, proj: LocalProject, db: FileIndexDB, config: Config, force_download: bool,
                 max_concurrent: int = 3):
        self.proj = proj
        self.db = db
        self.queue = asyncio.Queue()
        self.config = config
        self.force_download = force_download
        self.file_download_manager = FileDownloadManager(send_queue=self.queue,
                                                         client_id=self.config.client_uuid,
                                                         mcurl=self.config.default_remote.mcurl,
                                                         apitoken=self.config.default_remote.mcapikey,
                                                         max_concurrent=max_concurrent)
        self.async_reconciler = AsyncReconciler(db=db, proj=proj, recompute_checksum=True)
        self.tasks: list[Task[None]] = []

    async def start_workers(self):
        self.tasks = await self.file_download_manager.start_workers()

    async def stop_workers(self):
        self.file_download_manager.stop_workers()
        for task in self.tasks:
            try:
                await task
            except asyncio.CancelledError:
                pass

    async def download_file(self, path: str):
        recompute_checksum = self.async_reconciler.recompute_checksum
        file_entry = await observe_and_reconcile_to_file_entry(db=self.db,
                                                               proj=self.proj,
                                                               file_path=path,
                                                               recompute_checksum=recompute_checksum)
        await self._download_file_entry(file_entry)

    async def download_dir(self, path: str, recursive: bool = False, ignore_fn=None):
        async for current_path, entries in self.async_reconciler.walk(path=path, listdir_fn=local_listdir,
                                                                      recursive=recursive, ignore_fn=ignore_fn):
            for entry in entries.values():
                await self._download_file_entry(entry)

    async def _download_file_entry(self, state: FileState):
        if state.file_decision is not None:
            decision = state.file_decision
            if self.force_download:
                if not state.observation.local_entry.is_dir:
                    print(f"Forcing download of {state.observation.remote_entry.name}")
                    await self.file_download_manager.download_file(self.proj.id, state.observation.remote_entry.raw.id,
                                                                   state.observation.local_path.as_posix())
                else:
                    print(f"Skipping {state.observation.remote_entry.name} because local file is a directory")
            elif decision.action == 'download' and not state.observation.remote_entry.is_dir:
                # Create the path to download to
                download_to = projects.remote_to_local_project_path(proj_base=Path(self.proj.local_path),
                                                                    remote_path=state.observation.remote_path)
                # Check to make sure there isn't a directory with the same name
                if state.observation.local_entry is None:
                    # There is no local file system entry with the same name
                    print(
                        f"Downloading {state.observation.remote_entry.name} to {download_to} ({state.observation.remote_entry.size} bytes)")
                    await self.file_download_manager.download_file(self.proj.id, state.observation.remote_entry.raw.id,
                                                                   download_to.as_posix())
                elif not state.observation.local_entry.is_dir:
                    # There is a local file with the same name
                    print(
                        f"Downloading {state.observation.remote_entry.name} to {download_to} ({state.observation.remote_entry.size} bytes)")
                    await self.file_download_manager.download_file(self.proj.id, state.observation.remote_entry.raw.id,
                                                                   download_to.as_posix())
                else:
                    # If we are here, then there is an attempt to download a file that has the same name
                    # as a directory in the local file system.
                    print(f"Skipping {state.observation.remote_entry.name} because local file is a directory")

