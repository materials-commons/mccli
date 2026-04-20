import argparse
import asyncio
import signal
import uuid
from asyncio import Task
from typing import Awaitable, Callable, Dict, Any

from materials_commons.cli.server.command_handlers.admin_handler_lookup import AdminHandlerLookup
from materials_commons.cli.server.command_handlers.download_handler_lookup import DownloadHandlerLookup
from materials_commons.cli.server.command_handlers.list_handler_lookup import ListHandlerLookup
from materials_commons.cli.server.command_handlers.multi_handler_lookup import MultiHandlerLookup
from materials_commons.cli.server.command_handlers.search_find_handler_lookup import SearchFindHandlerLookup
from materials_commons.cli.server.command_handlers.upload_handler_lookup import UploadHandlerLookup
from materials_commons.cli.server.db.db_manager import DBManager
from materials_commons.cli.server.downloader.file_download_manager import FileDownloadManager
from materials_commons.cli.server.ocommand_handlers import register_handlers
from materials_commons.cli.server.local_rest_server import LocalRestServer
from materials_commons.cli.server.project_filedbs import ProjectFileDBs
from materials_commons.cli.server.uploader.file_upload_manager import FileUploadManager
from materials_commons.cli.server.websocket_server import WebSocketCommandListener
from materials_commons.cli.user_config import Config

CommandHandler = Callable[[Dict[str, Any]], Awaitable[None]]

DEFAULT_WS_URL = "wss://materialscommons.org/ws"
DEFAULT_RECONNECT_MIN_SEC = 1
DEFAULT_RECONNECT_MAX_SEC = 30


def make_parser():
    mc_server_usage = 'mc server [--ws-url]'
    parser = argparse.ArgumentParser(description=mc_server_usage)
    parser.add_argument('--ws-url', type=str, default=DEFAULT_WS_URL, help='WebSocket URL for commands')
    return parser


def server_subcommand(argv, working_dir=None):
    parser = make_parser()
    args = parser.parse_args(argv)
    try:
        asyncio.run(server_subcommand_async(args, working_dir))
    except KeyboardInterrupt:
        pass


async def server_subcommand_async(args, working_dir=None):
    config = Config()
    if config.client_uuid is None:
        config.client_uuid = str(uuid.uuid4())
        config.save()

    send_queue = asyncio.Queue()
    db_queue = asyncio.Queue()
    project_dbs = ProjectFileDBs()

    local_rest_server = LocalRestServer(loop=asyncio.get_running_loop(), queue=send_queue)
    local_rest_server.start()

    file_upload_manager = FileUploadManager(send_queue=send_queue, db_write_queue=db_queue,
                                            project_dbs=project_dbs, client_id=config.client_uuid)

    file_download_manager = FileDownloadManager(send_queue=send_queue, client_id=config.client_uuid,
                                                mcurl=config.default_remote.mcurl,
                                                apitoken=config.default_remote.mcapikey)
    db_manager = DBManager(db_queue=db_queue, project_dbs=project_dbs)

    await file_upload_manager.start_workers()
    await file_download_manager.start_workers()
    await db_manager.start_workers()

    lookup_handler = MultiHandlerLookup(UploadHandlerLookup(file_upload_manager),
                                        DownloadHandlerLookup(file_download_manager), ListHandlerLookup(),
                                        SearchFindHandlerLookup(), AdminHandlerLookup())

    listener = WebSocketCommandListener(
        ws_url=args.ws_url,
        token=config.default_remote.mcapikey,
        client_uuid=config.client_uuid,
        handler_lookup=lookup_handler,
        ws_send_queue=send_queue,
    )

    try:
        await listener.run()
    except asyncio.CancelledError:
        print("\nServer is shutting down...", flush=True)
    finally:
        local_rest_server.stop()
        await listener.shutdown()
        await file_upload_manager.stop_workers()
        await file_download_manager.stop_workers()
        await db_manager.stop_workers()
