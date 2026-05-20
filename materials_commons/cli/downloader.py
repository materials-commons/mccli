import logging
from pathlib import Path

from igittigitt import IgnoreParser, igittigitt

from materials_commons.cli.async_reconciler import AsyncReconciler
from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.local_project import LocalProject
from materials_commons.cli.requests import DownloadRequest
from materials_commons.cli.server.service_container import ServiceContainer
from materials_commons.cli.server.service_runtime import ServiceRuntime
from materials_commons.cli.walk import make_merged_listdir_func

logger = logging.getLogger(__name__)


class Downloader:
    def __init__(self, proj: LocalProject, db: FileIndexDB, ignore_parser: IgnoreParser, container: ServiceContainer,
                 service_runtime: ServiceRuntime):
        self.proj = proj
        self.db = db
        self.container = container
        self.service_runtime = service_runtime
        self.ignore_parser = ignore_parser
        self.async_reconciler = AsyncReconciler(db=db, proj=proj, reconcile_mode="download")
        self.listdir_fn = make_merged_listdir_func(proj)
        self.transfer_ids = []

    @classmethod
    async def init(cls, proj: LocalProject):
        db = await proj.get_filedb()

        # Setup ignore parser
        ignore_parser = igittigitt.IgnoreParser()
        ignore_parser.parse_rule_files(base_dir=proj.local_path, filename=".mcignore", add_default_patterns=False)

        # Start services
        container = ServiceContainer.create()
        service_runtime = ServiceRuntime(container)
        await service_runtime.start(db_manager=True, file_download_manager=True)

        return cls(proj=proj, db=db, ignore_parser=ignore_parser, container=container, service_runtime=service_runtime)

    async def run(self, paths: list[str | Path], recursive: bool = False, force: bool = False):
        try:
            for path in paths:
                p = Path(path)
                if p.is_dir():
                    # Walk and download local dir
                    await self._download_dir(p, recursive=recursive)
                elif p.is_file():
                    # Download local file
                    await self._download_file(p)
                else:
                    # If we are here, then the path entry is a remote path. We look up the path to
                    # determine the type of download to perform.
                    remote_path = self.proj.to_remote_path(p)
                    try:
                        f = self.proj.remote.get_file_by_path(self.proj.id, remote_path.as_posix())
                    except Exception as e:
                        # logger.error(f"Error getting file by path {remote_path}: {e}")
                        print(f"No such remote file {p}")
                        continue

                    if f is None:
                        continue

                    if f.mime_type == "directory":
                        # Remote entry is a directory
                        await self._download_dir(p, recursive=recursive, force=force)
                    else:
                        # Remote entry is a file
                        await self._download_file(p, force=force)

        except Exception as e:
            logger.error(f"Unexpected error during download: {e}")
            return False

        finally:
            await self.container.file_download_manager.wait_all()
            await self.service_runtime.stop()
            await self.db.close()

    async def _download_file(self, path: Path, force: bool = False) -> None:
        file_state = await self.async_reconciler.reconcile_file(path)
        if file_state.exception:
            logger.error(f"Error encountered while processing {path}: {file_state.exception}")
            print(f"Error encountered while processing {path}: {file_state.exception}")

        if file_state.file_decision.action == "download" or force:
            download_request = DownloadRequest(observation=file_state.observation,
                                               updated_record=file_state.file_decision.updated_record,
                                               project=self.proj)
            print(f"Downloading {path}...")
            transfer_id = await self.container.file_download_manager.download_file(download_request)
            self.transfer_ids.append(transfer_id)

    async def _download_dir(self, path: Path, recursive: bool = False, force: bool = False):
        async for current_path, path_entries in self.async_reconciler.walk(path=path, listdir_fn=self.listdir_fn,
                                                                           recursive=recursive, ignore_fn=None):
            for entry_name in sorted(path_entries):
                file_state = path_entries[entry_name]
                if file_state.exception:
                    logger.error(f"Error encountered while processing {entry_name}: {file_state.exception}")
                    print(f"Error encountered while processing {entry_name}: {file_state.exception}")
                    continue
                if file_state.file_decision.action == "download" or force:
                    download_request = DownloadRequest(observation=file_state.observation,
                                                       updated_record=file_state.file_decision.updated_record,
                                                       project=self.proj)
                    print(f"Downloading {entry_name} to {current_path / entry_name}...")
                    transfer_id = await self.container.file_download_manager.download_file(download_request)
                    self.transfer_ids.append(transfer_id)
