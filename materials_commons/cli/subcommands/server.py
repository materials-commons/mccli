import argparse
import asyncio
import json
import os
import signal
import socket
import ssl
import uuid
from typing import Awaitable, Callable, Dict, Any, Optional

# Requires: websockets (asyncio-based websocket client)
import websockets
from materials_commons.cli import desktop
from websockets.exceptions import ConnectionClosedError, ConnectionClosedOK, InvalidStatus

from materials_commons.cli.desktop.websocket_server import WebSocketCommandListener
from materials_commons.cli.user_config import Config
import materials_commons.cli.desktop

CommandHandler = Callable[[Dict[str, Any]], Awaitable[None]]

DEFAULT_WS_URL = "wss://materialscommons.org/ws/commands"
DEFAULT_RECONNECT_MIN_SEC = 1
DEFAULT_RECONNECT_MAX_SEC = 30


async def _handle_command(ws, cmd: Dict[str, Any], handlers: Dict[str, CommandHandler]) -> None:
    kind = cmd.get("type") or cmd.get("command")
    if not kind:
        return
    handler = handlers.get(kind)
    if handler:
        await handler(ws, cmd)


async def _ws_receive_loop(
        ws_url: str,
        token: Optional[str],
        client_uuid: str,
        handlers: Dict[str, CommandHandler],
) -> None:
    """
    Connects to the websocket and processes incoming JSON messages.
    Reconnects with exponential backoff on transient failures.
    """
    backoff = DEFAULT_RECONNECT_MIN_SEC

    ssl_context = ssl.create_default_context()
    ssl_context.check_hostname = False
    ssl_context.verify_mode = ssl.CERT_NONE

    while True:
        headers = {}
        if token:
            headers["Authorization"] = f"Bearer {token}"

        headers["MC-Client-ID"] = client_uuid
        headers["MC-Client-Hostname"] = socket.gethostname()
        headers["MC-Connection-Type"] = "cli"

        projects = desktop.list_local_projects()
        headers["MC-Client-Projects"] = ",".join([str(p["project_id"]) for p in projects])

        try:
            async with websockets.connect(ws_url, additional_headers=headers, open_timeout=15, ssl=ssl_context) as ws:
                # Reset backoff after a successful connection
                backoff = DEFAULT_RECONNECT_MIN_SEC  # keep a minimal reconnection state
                while True:
                    raw = await ws.recv()  # may raise if the connection closes
                    # Expecting a JSON object or array
                    try:
                        data = json.loads(raw)
                    except json.JSONDecodeError:
                        continue

                    # Support either a single command or a batch
                    if isinstance(data, dict):
                        await _handle_command(ws, data, handlers)
                    elif isinstance(data, list):
                        for item in data:
                            if isinstance(item, dict):
                                await _handle_command(ws, item, handlers)
        except (ConnectionClosedOK, ConnectionClosedError, InvalidStatus, OSError, asyncio.TimeoutError):
            await asyncio.sleep(backoff)
            backoff = min(DEFAULT_RECONNECT_MAX_SEC, max(DEFAULT_RECONNECT_MIN_SEC, backoff * 2))


async def run_command_listener(url: str, token: Optional[str], client_uuid: str,
                               handlers: Dict[str, CommandHandler], ) -> None:
    await _ws_receive_loop(url, token, client_uuid, handlers)


# Example handlers
async def handle_sync(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] sync -> {cmd}")


async def handle_refresh_cache(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] refresh_cache -> {cmd}")


async def handle_shutdown(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] shutdown -> {cmd}")
    os.kill(os.getpid(), signal.SIGINT)


async def handle_list_dir(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] list_dir -> {cmd}")


async def handle_upload_file(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] upload_file -> {cmd}")


async def handle_download_file(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] download_file -> {cmd}")


async def handle_upload_dir(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] upload_dir -> {cmd}")


async def handle_download_dir(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] download_dir -> {cmd}")

async def handle_list_projects(ws, cmd: Dict[str, Any]) -> None:
    print(f"[handler] list_projects -> {cmd}")


def build_handlers() -> Dict[str, CommandHandler]:
    return {
        "sync": handle_sync,
        "refresh_cache": handle_refresh_cache,
        "shutdown": handle_shutdown,
        "list_dir": handle_list_dir,
        "upload_file": handle_upload_file,
        "download_file": handle_download_file,
        "upload_dir": handle_upload_dir,
        "download_dir": handle_download_dir,
    }


def _install_signal_handlers(loop: asyncio.AbstractEventLoop):
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, lambda s=sig: asyncio.create_task(_graceful_stop(loop)))
        except NotImplementedError:
            signal.signal(sig, lambda *_: asyncio.create_task(_graceful_stop(loop)))


async def _graceful_stop(loop: asyncio.AbstractEventLoop):
    tasks = [t for t in asyncio.all_tasks(loop) if t is not asyncio.current_task(loop)]
    for t in tasks:
        t.cancel()
    await asyncio.gather(*tasks, return_exceptions=True)
    loop.stop()


def server_subcommand(argv, working_dir=None):
    parser = argparse.ArgumentParser(description='Start the Materials Commons server')
    parser.add_argument('--port', type=int, default=443, help='Port to run the server on')
    parser.add_argument('--init', action="store_true", default=False, help='Initialize the server')
    parser.add_argument('--base', type=str, default=None, help='Base directory to serve projects from')
    parser.add_argument('--debug', action="store_true", default=False, help='Run in debug mode')
    parser.add_argument('--ws-url', type=str, default=DEFAULT_WS_URL, help='WebSocket URL for commands')
    # parser.add_argument('--token', type=str, default=os.getenv("MC_TOKEN"), help='Auth token for materialscommons.org')
    args = parser.parse_args(argv)

    handlers = build_handlers()

    config = Config()
    if config.client_uuid is None:
        config.client_uuid = str(uuid.uuid4())
        config.save()

    async def main():
        listener = WebSocketCommandListener(args.ws_url, config.default_remote.mcapikey, config.client_uuid)
        await listener.run()
        # listener = asyncio.create_task(
        #     run_command_listener(args.ws_url, config.default_remote.mcapikey, config.client_uuid, handlers))
        # await listener

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
