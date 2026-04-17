import asyncio
from asyncio import Task
from pathlib import Path

from materials_commons.cli.async_reconciler import AsyncReconciler
from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.models import LocalProject, FileState
from materials_commons.cli.reconcile2 import observe_and_reconcile_to_file_state
from materials_commons.cli.server import projects
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
        file_state = await observe_and_reconcile_to_file_state(db=self.db,
                                                               proj=self.proj,
                                                               file_path=path,
                                                               recompute_checksum=recompute_checksum)
        if not file_state.observation.remote_entry:
            # No remote entry to download
            return
        if file_state.observation.remote_entry.is_dir:
            # download_file only downloads a file, not a directory
            return

        # A potential file we can download
        await self._download_file_state(file_state)

    async def download_dir(self, path: str, recursive: bool = False, ignore_fn=None):
        async for current_path, file_states in self.async_reconciler.walk(path=path, listdir_fn=local_listdir,
                                                                          recursive=recursive, ignore_fn=ignore_fn):
            for file_state in file_states.values():
                if not file_state.observation.remote_entry:
                    # No remote entry so skip
                    continue

                if file_state.observation.remote_entry.is_dir:
                    # Skip directories
                    continue

                # A potential file we can download
                await self._download_file_state(file_state)

    async def _download_file_state(self, state: FileState):
        """
        Download a file based on the provided FileState object.

        This method handles the decision-making process for downloading files, including checking for local and remote entries,
        handling forced downloads, and managing file conflicts.
        """
        # Sanity checks
        if not state.file_decision:
            # Bug, no decision was computed for this file
            print(f"Skipping {state.observation.local_path} because no decision was computed")
            return

        if not state.observation.remote_entry:
            # No remote entry to download
            print(f"Skipping {state.observation.local_path} because no remote entry")
            return

        if state.observation.remote_entry.is_dir:
            print(f"Skipping {state.observation.remote_entry.name} because remote file is a directory")
            return

        if state.observation.local_entry and state.observation.local_entry.is_dir:
            print(f"Skipping {state.observation.remote_entry.name} because local file is a directory")
            return

        # Validations have passed, so now we need to determine if we can actually download the file. At this
        # point we know we have a decision and a remote entry, and the remote entry is a file.
        decision = state.file_decision
        download_to = projects.remote_to_local_project_path(proj_base=Path(self.proj.local_path),
                                                            remote_path=state.observation.remote_path)
        remote_name = state.observation.remote_entry.name
        remote_size = state.observation.remote_entry.size
        if self.force_download:
            if not state.observation.local_entry:
                # local entry doesn't exist, so just download it
                print(f"Downloading {remote_name} to {download_to} ({remote_size} bytes)")
                await self.file_download_manager.download_file(self.proj.id, state.observation.remote_entry.raw.id,
                                                               download_to.as_posix())
            else:
                # local entry exists, so we need to overwrite it
                print(f"Forcing download of {remote_name} to {download_to} ({remote_size} bytes)")
                await self.file_download_manager.download_file(self.proj.id, state.observation.remote_entry.raw.id,
                                                               state.observation.local_path.as_posix())
        elif decision.action == 'download':
            # We can safely download the file. At this point we are in one of two situations:
            #   1. The file doesn't exist locally, so it is safe to download, or
            #   2. The file exists locally, the remote file is newer, and we know the local file has
            #      been uploaded, so we can safely overwrite it.
            print(f"Downloading {remote_name} to {download_to} ({remote_size} bytes)")
            await self.file_download_manager.download_file(self.proj.id, state.observation.remote_entry.raw.id,
                                                           download_to.as_posix())
