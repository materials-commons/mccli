import asyncio
import os
import signal
from typing import Optional, Dict, Any

from materials_commons.cli.server.command_handlers.protocol import CommandHandlerLookup, HandlerFunc


class AdminHandlerLookup(CommandHandlerLookup):
    def __init__(self):
        self._handlers: Dict[str, HandlerFunc] = {
            "REFRESH_CACHE": handle_refresh_cache,
            "SHUTDOWN": handle_shutdown,
        }

    def get_handler(self, cmd: str) -> Optional[HandlerFunc]:
        return self._handlers.get(cmd)


async def handle_refresh_cache(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] refresh_cache -> {cmd}")


async def handle_shutdown(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] shutdown -> {cmd}")
    os.kill(os.getpid(), signal.SIGINT)
