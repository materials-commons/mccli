import asyncio
import uuid
from functools import cached_property

from materials_commons.cli.server.db.db_manager import DBManager
from materials_commons.cli.server.downloader.file_download_manager import FileDownloadManager
from materials_commons.cli.server.local_rest_server import LocalRestServer
from materials_commons.cli.server.project_filedbs import ProjectFileDBs
from materials_commons.cli.server.uploader.file_upload_manager import FileUploadManager
from materials_commons.cli.config import Config


class ServiceContainer:
    def __init__(self, config: Config, loop: asyncio.AbstractEventLoop):
        self.config = config
        self.loop = loop

        self.send_queue = asyncio.Queue()
        self.db_queue = asyncio.Queue()
        self.project_dbs = ProjectFileDBs()

    @classmethod
    def create(cls) -> "ServiceContainer":
        config = Config.load()
        if config.client_uuid is None:
            config.client_uuid = str(uuid.uuid4())
            config.save()

        return cls(config=config, loop=asyncio.get_running_loop())

    @cached_property
    def local_rest_server(self) -> LocalRestServer:
        return LocalRestServer(loop=self.loop, queue=self.send_queue)

    @cached_property
    def file_upload_manager(self) -> FileUploadManager:
        return FileUploadManager(
            send_queue=self.send_queue,
            db_write_queue=self.db_queue,
            project_dbs=self.project_dbs,
            client_id=self.config.client_uuid,
        )

    @cached_property
    def file_download_manager(self) -> FileDownloadManager:
        return FileDownloadManager(
            send_queue=self.send_queue,
            client_id=self.config.client_uuid,
            mcurl=self.config.default_remote.mcurl,
            apitoken=self.config.default_remote.mcapikey,
        )

    @cached_property
    def db_manager(self) -> DBManager:
        return DBManager(db_queue=self.db_queue, project_dbs=self.project_dbs)