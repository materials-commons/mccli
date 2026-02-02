import argparse
import asyncio
import signal
import uuid
from typing import Awaitable, Callable, Dict, Any

from materials_commons.cli.desktop.command_handlers import register_handlers
from materials_commons.cli.desktop.websocket_server import WebSocketCommandListener
from materials_commons.cli.user_config import Config
from materials_commons.cli.desktop.local_rest_server import LocalRestServer

CommandHandler = Callable[[Dict[str, Any]], Awaitable[None]]

DEFAULT_WS_URL = "wss://materialscommons.org/ws"
DEFAULT_RECONNECT_MIN_SEC = 1
DEFAULT_RECONNECT_MAX_SEC = 30


def _install_signal_handlers(loop: asyncio.AbstractEventLoop):
    # Installs signal handlers for graceful shutdown
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, lambda s=sig: asyncio.create_task(_graceful_stop(loop)), None)
        except NotImplementedError:
            signal.signal(sig, lambda *_: asyncio.create_task(_graceful_stop(loop)))


async def _graceful_stop(loop: asyncio.AbstractEventLoop):
    """Cancels pending tasks then stops the event loop"""
    tasks = [t for t in asyncio.all_tasks(loop) if t is not asyncio.current_task(loop)]
    for t in tasks:
        t.cancel()
    await asyncio.gather(*tasks, return_exceptions=True)
    loop.stop()


def server_subcommand(argv, working_dir=None):
    parser = argparse.ArgumentParser(description='Start the Materials Commons server')
    parser.add_argument('--ws-url', type=str, default=DEFAULT_WS_URL, help='WebSocket URL for commands')
    args = parser.parse_args(argv)

    config = Config()
    if config.client_uuid is None:
        config.client_uuid = str(uuid.uuid4())
        config.save()

    # Runs local REST server and remote command listener
    async def main():
        queue = asyncio.Queue()
        running_loop = asyncio.get_running_loop()
        local_rest_server = LocalRestServer(loop=running_loop, queue=queue)
        local_rest_server.start()

        listener = WebSocketCommandListener(args.ws_url, config.default_remote.mcapikey, config.client_uuid,
                                            register_handlers(), queue)
        try:
            await listener.run()
        finally:
            await listener.shutdown()
            local_rest_server.stop()

    # Creates event loop; runs main; handles interrupts
    try:
        loop = asyncio.new_event_loop()
        asyncio.set_event_loop(loop)
        _install_signal_handlers(loop)
        loop.run_until_complete(main())
    except KeyboardInterrupt:
        pass
    finally:
        try:
            loop.run_until_complete(_graceful_stop(loop))
        except Exception:
            pass
        loop.close()
