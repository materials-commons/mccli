import asyncio
import json
import os
import signal
import socket
import ssl
from typing import Dict, Any, Optional, Callable, Awaitable

import websockets
from websockets.exceptions import ConnectionClosedError, ConnectionClosedOK, InvalidStatus
from materials_commons.cli import desktop

# Type alias for handler functions
CommandHandler = Callable[[Any, Dict[str, Any]], Awaitable[None]]


class WebSocketCommandListener:
    DEFAULT_RECONNECT_MIN_SEC = 1
    DEFAULT_RECONNECT_MAX_SEC = 30

    def __init__(self, ws_url: str, token: Optional[str], client_uuid: str):
        self.ws_url = ws_url
        self.token = token
        self.client_uuid = client_uuid
        self.backoff = self.DEFAULT_RECONNECT_MIN_SEC
        self.handlers = self._build_handlers()

    def _build_handlers(self) -> Dict[str, CommandHandler]:
        return {
            "sync": self.handle_sync,
            "refresh_cache": self.handle_refresh_cache,
            "shutdown": self.handle_shutdown,
            "list_dir": self.handle_list_dir,
            "upload_file": self.handle_upload_file,
            "download_file": self.handle_download_file,
            "upload_dir": self.handle_upload_dir,
            "download_dir": self.handle_download_dir,
            "list_projects": self.handle_list_projects,
        }

    async def run(self) -> None:
        """
        Main loop: Connects to the websocket and processes incoming JSON messages.
        Reconnects with exponential backoff on transient failures.
        """
        ssl_context = ssl.create_default_context()
        ssl_context.check_hostname = False
        ssl_context.verify_mode = ssl.CERT_NONE

        while True:
            headers = self._build_headers()

            try:
                async with websockets.connect(
                        self.ws_url,
                        additional_headers=headers,
                        open_timeout=15,
                        ssl=ssl_context
                ) as ws:
                    # Reset backoff after a successful connection
                    self.backoff = self.DEFAULT_RECONNECT_MIN_SEC

                    while True:
                        raw = await ws.recv()
                        try:
                            data = json.loads(raw)
                        except json.JSONDecodeError:
                            continue

                        # Support either a single command or a batch
                        if isinstance(data, dict):
                            await self._dispatch(ws, data)
                        elif isinstance(data, list):
                            for item in data:
                                if isinstance(item, dict):
                                    await self._dispatch(ws, item)

            except (ConnectionClosedOK, ConnectionClosedError, InvalidStatus, OSError, asyncio.TimeoutError):
                await asyncio.sleep(self.backoff)
                self.backoff = min(
                    self.DEFAULT_RECONNECT_MAX_SEC,
                    max(self.DEFAULT_RECONNECT_MIN_SEC, self.backoff * 2)
                )

    async def _dispatch(self, ws, cmd: Dict[str, Any]) -> None:
        kind = cmd.get("type") or cmd.get("command")
        if not kind:
            return
        handler = self.handlers.get(kind)
        if handler:
            await handler(ws, cmd)

    def _build_headers(self) -> Dict[str, str]:
        headers = {}
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"

        headers["MC-Client-ID"] = self.client_uuid
        headers["MC-Client-Hostname"] = socket.gethostname()
        headers["MC-Connection-Type"] = "cli"

        try:
            projects = desktop.list_local_projects()
            headers["MC-Client-Projects"] = ",".join([str(p["project_id"]) for p in projects])
        except Exception:
            # Fallback if listing projects fails
            headers["MC-Client-Projects"] = ""

        return headers

    # --- Handlers ---

    async def handle_sync(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] sync -> {cmd}")

    async def handle_refresh_cache(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] refresh_cache -> {cmd}")

    async def handle_shutdown(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] shutdown -> {cmd}")
        os.kill(os.getpid(), signal.SIGINT)

    async def handle_list_dir(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] list_dir -> {cmd}")

    async def handle_upload_file(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] upload_file -> {cmd}")

    async def handle_download_file(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] download_file -> {cmd}")

    async def handle_upload_dir(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] upload_dir -> {cmd}")

    async def handle_download_dir(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] download_dir -> {cmd}")

    async def handle_list_projects(self, ws, cmd: Dict[str, Any]) -> None:
        print(f"[handler] list_projects -> {cmd}")
