import asyncio
from typing import Dict, Any,Callable, Awaitable
from materials_commons.cli import desktop
import os
import signal

CommandHandler = Callable[[Any, Dict[str, Any]], Awaitable[None]]
def register_handlers() -> Dict[str, CommandHandler]:
    return {
        "sync": handle_sync,
        "refresh_cache": handle_refresh_cache,
        "shutdown": handle_shutdown,
        "list_dir": handle_list_dir,
        "upload_file": handle_upload_file,
        "download_file": handle_download_file,
        "upload_dir": handle_upload_dir,
        "download_dir": handle_download_dir,
        "list_projects": handle_list_projects,
    }

async def handle_sync(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] sync -> {cmd}")

async def handle_refresh_cache(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] refresh_cache -> {cmd}")

async def handle_shutdown(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] shutdown -> {cmd}")
    os.kill(os.getpid(), signal.SIGINT)

async def handle_list_dir(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] list_dir -> {cmd}")

async def handle_upload_file(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] upload_file -> {cmd}")

async def handle_download_file(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] download_file -> {cmd}")

async def handle_upload_dir(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] upload_dir -> {cmd}")

async def handle_download_dir(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] download_dir -> {cmd}")

async def handle_list_projects(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] list_projects -> {cmd}")
    projects = desktop.list_local_projects()
    await queue.put({"command": "list_projects", "payload": projects})
