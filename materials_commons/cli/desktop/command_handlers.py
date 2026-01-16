import asyncio
from typing import Dict, Any,Callable, Awaitable
from materials_commons.cli import desktop
import os
import signal
import logging

logger = logging.getLogger(__name__)

CommandHandler = Callable[[Any, Dict[str, Any]], Awaitable[None]]
def register_handlers() -> Dict[str, CommandHandler]:
    return {
        "sync": handle_sync,
        "refresh_cache": handle_refresh_cache,
        "shutdown": handle_shutdown,
        "list_dir": handle_list_dir,
        "download_file": handle_download_file,
        "download_dir": handle_download_dir,
        "LIST_PROJECTS": handle_list_projects,
        "UPLOAD_FILE": handle_upload_file,
        "UPLOAD_DIRECTORY": handle_upload_directory,
        "CANCEL_UPLOAD": handle_cancel_upload,
        "UPLOAD_PAUSE": handle_pause_upload,
        "UPLOAD_RESUME": handle_resume_upload,
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

async def handle_download_file(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] download_file -> {cmd}")

async def handle_download_dir(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] download_dir -> {cmd}")

async def handle_list_projects(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    # print(f"[handler] list_projects -> {cmd}")
    projects = desktop.list_local_projects()
    await queue.put({"command": "list_projects", "payload": projects})

async def handle_upload_file(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    """
    Handler for UPLOAD_FILE command from the server. It will receive a request that
    contains the project_id and project_path. It will optionally receive the file_path
    if the file being requested is not in the project. Optionally, the server can send
    a chunk_size to use for uploading the file.

    Expected payload:
    {
        "project_path": "/path/to/file/in/project", # This assumes / for project root, client will resolve to file_path
        "file_path": "path/to/file/in/project/file.csv", # Optional
        "project_id": 1047,
        "chunk_size": 1048576 # Optional
    }
    """
    print(f"[handler] upload_file_request -> {cmd}")
    payload = cmd.get("payload") or {}
    project_path = payload.get("project_path")
    file_path = payload.get("file_path")
    project_id = payload.get("project_id")
    chunk_size = payload.get("chunk_size", 1024 * 1024)

    # Validate that we at least have a project_id and project_path
    if not project_path or not project_id:
        logger.error("Missing required fields in UPLOAD_FILE command")
        return

    # The server may send us just a project_path. If that happens, then we need to resolve it to a file_path
    # based on the local project's path. The project_path the server sends us is the path on the MC server.
    # All MC server projects start with /, so we need to resolve to the local client path to the project.
    if project_path and not file_path:
        p = desktop.get_local_project_by_id(project_id)
        if not p:
            logger.error(f"Project {project_id} not found in local projects")
            return
        file_path = os.path.join(p["project_dir_path"], project_path)

    print(f"Resolved file_path: {file_path}")

    # This will be set by the listener
    file_manager = cmd.get("_file_manager")
    if not file_manager:
        logger.error("File transfer manager not available")
        return

    logger.info(f"Received upload request for: {file_path}")

    try:
        transfer_id = await file_manager.upload_file(
            file_path=file_path,
            project_id=project_id,
            project_path=project_path,
            chunk_size=chunk_size,
            progress_callback=lambda sent, total: logger.debug(f"{file_path}: {sent}/{total}")
        )
        logger.info(f"Queued upload: {file_path} (transfer_id: {transfer_id})")
    except Exception as e:
        logger.error(f"Failed to queue upload: {e}")


async def handle_upload_directory(queue: asyncio.Queue, cmd: Dict[str, Any]):
    """
    Handler for UPLOAD_DIRECTORY command from the server.

    Expected payload:
    {
        "directory_path": "/local/path/to/data",
        "project_id": 1047,
        "recursive": true,
        "chunk_size": 1048576 // optional
    }
    """
    payload = cmd.get("payload") or {}
    directory_path = payload.get("directory_path")
    project_id = payload.get("project_id")
    recursive = payload.get("recursive", True)
    chunk_size = payload.get("chunk_size", 1024 * 1024)

    if not directory_path or not project_id:
        logger.error("Missing required fields in UPLOAD_DIRECTORY command")
        return

    file_manager = cmd.get("_file_manager")
    if not file_manager:
        logger.error("File transfer manager not available")
        return

    logger.info(f"Received directory upload request for: {directory_path}")

    # try:
    #     transfer_ids = await file_manager.upload_directory(
    #         dir_path=directory_path,
    #         project_id=project_id,
    #         directory_id=directory_id,
    #         recursive=recursive,
    #         progress_callback=lambda fname, sent, total: logger.debug(f"{fname}: {sent}/{total}")
    #     )
    #     logger.info(f"Queued {len(transfer_ids)} files from {directory_path}")
    # except Exception as e:
    #     logger.error(f"Failed to queue directory upload: {e}")


async def handle_cancel_upload(queue: asyncio.Queue, cmd: Dict[str, Any]):
    """
    Handler for CANCEL_UPLOAD command from the server.

    Expected payload:
    {
        "transfer_id": "uuid-here"
    }
    """
    payload = cmd.get("payload", {})
    transfer_id = payload.get("transfer_id")

    if not transfer_id:
        logger.error("Missing transfer_id in CANCEL_UPLOAD command")
        return

    file_manager = cmd.get("_file_manager")
    if not file_manager:
        logger.error("File transfer manager not available")
        return

    logger.info(f"Received cancel request for transfer: {transfer_id}")
    file_manager.cancel_upload(transfer_id)


async def handle_pause_upload(queue: asyncio.Queue, cmd: Dict[str, Any]):
    """
    Handler for PAUSE_UPLOAD command from the server.

    Expected payload:
    {
        "transfer_id": "uuid-here"
    }
    """
    payload = cmd.get("payload", {})
    transfer_id = payload.get("transfer_id")

    if not transfer_id:
        logger.error("Missing transfer_id in PAUSE_UPLOAD command")
        return

    file_manager = cmd.get("_file_manager")
    if not file_manager:
        return

    logger.info(f"Received pause request for transfer: {transfer_id}")
    file_manager.pause_upload(transfer_id)


async def handle_resume_upload(queue: asyncio.Queue, cmd: Dict[str, Any]):
    """
    Handler for RESUME_UPLOAD command from the server.

    Expected payload:
    {
        "transfer_id": "uuid-here"
    }
    """
    payload = cmd.get("payload", {})
    transfer_id = payload.get("transfer_id")

    if not transfer_id:
        logger.error("Missing transfer_id in RESUME_UPLOAD command")
        return

    file_manager = cmd.get("_file_manager")
    if not file_manager:
        return

    logger.info(f"Received resume request for transfer: {transfer_id}")
    file_manager.resume_upload(transfer_id)