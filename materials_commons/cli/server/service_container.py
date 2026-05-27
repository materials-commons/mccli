import asyncio
import uuid
from functools import cached_property
from typing import Optional

from materials_commons.cli.config import Config
from materials_commons.cli.requests import DBWriteRequest, IndexRequest
from materials_commons.cli.server.command_handlers.admin_handler_lookup import AdminHandlerLookup
from materials_commons.cli.server.command_handlers.download_handler_lookup import DownloadHandlerLookup
from materials_commons.cli.server.command_handlers.list_handler_lookup import ListHandlerLookup
from materials_commons.cli.server.command_handlers.multi_handler_lookup import MultiHandlerLookup
from materials_commons.cli.server.command_handlers.search_find_handler_lookup import SearchFindHandlerLookup
from materials_commons.cli.server.command_handlers.upload_handler_lookup import UploadHandlerLookup
from materials_commons.cli.server.db.db_manager import DBManager
from materials_commons.cli.server.downloader.file_download_manager import FileDownloadManager
from materials_commons.cli.server.indexer.file_index_manager import FileIndexManager
from materials_commons.cli.server.local_rest_server import LocalRestServer
from materials_commons.cli.server.project_filedbs import ProjectFileDBs
from materials_commons.cli.server.uploader.file_upload_manager import FileUploadManager
from materials_commons.cli.server.websocket_server import WebSocketCommandListener


class ServiceContainer:
    def __init__(self, config: Config, loop: asyncio.AbstractEventLoop, ws_url: Optional[str] = None):
        self.config = config
        self.loop = loop
        self.ws_url = ws_url

        self.send_queue = asyncio.Queue()
        self.db_queue: asyncio.Queue[DBWriteRequest] = asyncio.Queue()
        self.file_index_queue: asyncio.Queue[IndexRequest] = asyncio.Queue()
        self.project_dbs = ProjectFileDBs()

    @classmethod
    def create(cls, ws_url: Optional[str] = None) -> "ServiceContainer":
        config = Config.load()
        if config.client_uuid is None or config.client_uuid == "":
            config.client_uuid = str(uuid.uuid4())
            config.save()

        return cls(config=config, loop=asyncio.get_running_loop(), ws_url=ws_url)

    @cached_property
    def local_rest_server(self) -> LocalRestServer:
        return LocalRestServer(loop=self.loop, queue=self.send_queue)

    @cached_property
    def file_upload_manager(self) -> FileUploadManager:
        return FileUploadManager(
            send_queue=self.send_queue,
            db_write_queue=self.db_queue,
            client_id=self.config.client_uuid,
        )

    @cached_property
    def file_download_manager(self) -> FileDownloadManager:
        return FileDownloadManager(
            send_queue=self.send_queue,
            db_write_queue=self.db_queue,
            client_id=self.config.client_uuid,
            mcurl=self.config.default_remote.mcurl,
            apitoken=self.config.default_remote.mcapikey,
        )

    @cached_property
    def file_index_manager(self) -> FileIndexManager:
        return FileIndexManager(
            db_queue=self.db_queue,
            index_queue=self.file_index_queue,
        )

    @cached_property
    def db_manager(self) -> DBManager:
        return DBManager(db_queue=self.db_queue)

    @cached_property
    def command_handler_lookup(self) -> MultiHandlerLookup:
        return MultiHandlerLookup(
            UploadHandlerLookup(self.file_upload_manager),
            DownloadHandlerLookup(self.file_download_manager),
            ListHandlerLookup(),
            SearchFindHandlerLookup(),
            AdminHandlerLookup(),
        )

    @cached_property
    def websocket_listener(self) -> WebSocketCommandListener:
        if self.ws_url is None:
            raise ValueError("ws_url is required to create websocket_listener")

        return WebSocketCommandListener(
            ws_url=self.ws_url,
            token=self.config.default_remote.mcapikey,
            client_uuid=self.config.client_uuid,
            handler_lookup=self.command_handler_lookup,
            ws_send_queue=self.send_queue,
        )
