import argparse
import asyncio
from typing import Awaitable, Callable, Dict, Any

from materials_commons.cli.server.command_handlers.admin_handler_lookup import AdminHandlerLookup
from materials_commons.cli.server.command_handlers.download_handler_lookup import DownloadHandlerLookup
from materials_commons.cli.server.command_handlers.list_handler_lookup import ListHandlerLookup
from materials_commons.cli.server.command_handlers.multi_handler_lookup import MultiHandlerLookup
from materials_commons.cli.server.command_handlers.search_find_handler_lookup import SearchFindHandlerLookup
from materials_commons.cli.server.command_handlers.upload_handler_lookup import UploadHandlerLookup
from materials_commons.cli.server.service_container import ServiceContainer
from materials_commons.cli.server.service_runtime import ServiceRuntime
from materials_commons.cli.server.websocket_server import WebSocketCommandListener

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
    container = ServiceContainer.create()
    service_runtime = ServiceRuntime(container)
    await service_runtime.start(
        local_rest_server=True,
        file_upload_manager=True,
        file_download_manager=True,
        db_manager=True,
    )

    lookup_handler = MultiHandlerLookup(UploadHandlerLookup(container.file_upload_manager),
                                        DownloadHandlerLookup(container.file_download_manager), ListHandlerLookup(),
                                        SearchFindHandlerLookup(), AdminHandlerLookup())

    listener = WebSocketCommandListener(
        ws_url=args.ws_url,
        token=container.config.default_remote.mcapikey,
        client_uuid=container.config.client_uuid,
        handler_lookup=lookup_handler,
        ws_send_queue=container.send_queue,
    )

    try:
        await listener.run()
    except asyncio.CancelledError:
        print("\nServer is shutting down...", flush=True)
    finally:
        await listener.shutdown()
        await service_runtime.stop()
