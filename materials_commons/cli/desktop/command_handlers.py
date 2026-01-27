import asyncio
from typing import Dict, Any, Callable, Awaitable
from materials_commons.cli import desktop
import os
import signal
import logging
from pathlib import Path

logger = logging.getLogger(__name__)

CommandHandler = Callable[[Any, Dict[str, Any]], Awaitable[None]]


def register_handlers() -> Dict[str, CommandHandler]:
    return {
        "sync": handle_sync,
        "refresh_cache": handle_refresh_cache,
        "SHUTDOWN": handle_shutdown,
        "LIST_DIR": handle_list_dir,
        "LIST_PROJECTS": handle_list_projects,

        # Upload commands
        "UPLOAD_FILE": handle_upload_file,
        "UPLOAD_DIRECTORY": handle_upload_directory,
        "CANCEL_UPLOAD": handle_cancel_upload,
        "UPLOAD_PAUSE": handle_pause_upload,
        "UPLOAD_RESUME": handle_resume_upload,

        # Download commands
        "DOWNLOAD_FILE": handle_download_file,
        "PAUSE_DOWNLOAD": handle_pause_download,
        "RESUME_DOWNLOAD": handle_resume_download,
        "CANCEL_DOWNLOAD": handle_cancel_download,
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

    # We must have a project_id
    if not project_id:
        logger.error("Missing required field project_id in UPLOAD_FILE command")
        return

    # We also must have a project_path
    if not project_path:
        logger.error("Missing required field project_path in UPLOAD_FILE command")
        return

    # The server may send us just a project_path. If that happens, then we need to resolve it to a file_path
    # based on the local project's path. The project_path the server sends us is the path on the MC server.
    # All MC server projects start with /, so we need to resolve to the local client path to the project.
    # To do this, we get the local path for the project, then join project_path and the local project's path
    # to get the full local path. Note that project_path may start with a /, so we strip it off before joining.
    if not file_path:
        p = desktop.get_local_project_by_id(project_id)
        if not p:
            logger.error(f"Project {project_id} not found in local projects")
            return
        file_path = os.path.join(p["project_dir_path"], project_path.lstrip("/"))

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
        "project_path": "/path/to/file/in/project", # This assumes / for project root, client will resolve to file_path
        "directory_path": "/local/path/to/data",
        "project_id": 1047,
        "recursive": true,
        "chunk_size": 1048576 // optional
    }
    """
    payload = cmd.get("payload") or {}
    mc_project_path = payload.get("mc_project_path")
    local_directory_path = payload.get("local_directory_path")
    project_id = payload.get("project_id")
    recursive = payload.get("recursive", True)
    chunk_size = payload.get("chunk_size", 1024 * 1024)

    if not project_id:
        logger.error("Missing required field project_id in UPLOAD_DIRECTORY command")
        return

    if not mc_project_path:
        logger.error("Missing required field mc_project_path in UPLOAD_DIRECTORY command")
        return

    p = desktop.get_local_project_by_id(project_id)
    if not p:
        logger.error(f"Project {project_id} not found in local projects")
        return

    if not local_directory_path:
        local_directory_path = os.path.join(p["project_dir_path"], mc_project_path.lstrip("/"))

    file_manager = cmd.get("_file_manager")
    if not file_manager:
        logger.error("File transfer manager not available")
        return

    print(f"Calling upload_directory with recursive={recursive}")
    print(f"   dest_dir = {mc_project_path}")
    print(f"   dir_path = {local_directory_path}")
    print(f"   local_project_root = {p['project_dir_path']}")
    try:
        await file_manager.upload_directory(
            dest_dir=mc_project_path,
            dir_path=local_directory_path,
            project_id=project_id,
            recursive=recursive,
            chunk_size=chunk_size,
        )
        logger.info(f"Queued upload of directory: {local_directory_path}")
    except Exception as e:
        logger.error(f"Failed to queue upload of directory: {e}")
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

async def handle_download_file(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    """
    Handler for DOWNLOAD_FILE command from the server.

    Expected payload:
    {
        "project_path": "/path/to/download/file/to/in/project", # Optional
        "file_path": "/full/path/to/download/file/to, # Optional, but one of project_path or file_path must be specified
        "project_id": 1047, # materials commons project id
        "file_id": 159681, # materials commons file id
        "size": 54323 # Size in bytes
        "checksum": "md5-checksum-here" # md5 checksum of file
    }
    """
    print(f"[handler] download_file -> {cmd}")
    payload = cmd.get("payload") or {}
    project_path = payload.get("project_path")
    file_path = payload.get("file_path")
    project_id = payload.get("project_id")
    file_id = payload.get("file_id")
    size = payload.get("size")
    checksum = payload.get("checksum")

    # We must have a project_id
    if not project_id:
        logger.error("Missing required field project_id in DOWNLOAD_FILE command")
        return

    # We also must have a file_id
    if not file_id:
        logger.error("Missing required field file_id in DOWNLOAD_FILE command")
        return

    # we must have a file_path or project_path
    if not file_path and not project_path:
        logger.error("Missing required field file_path or project_path in DOWNLOAD_FILE command")
        return

    # We must have a size
    if not size:
        logger.error("Missing required field size in DOWNLOAD_FILE command")
        return

    # We must have a checksum
    if not checksum:
        logger.error("Missing required field checksum in DOWNLOAD_FILE command")

    if project_path:
        # If we have a project path, then we need to translate it to a
        # local file path
        p = desktop.get_local_project_by_id(project_id)
        if not p:
            logger.error(f"Project {project_id} not found in local projects")
            return
        file_path = os.path.join(p["project_dir_path"], project_path.lstrip("/"))

    # This will be set by the listener in websocket_server
    file_manager = cmd.get("_file_manager")
    if not file_manager:
        logger.error("File transfer manager not available")

    try:
        print(f"I would download to {file_path} with size {size} and checksum {checksum}, file_id {file_id}")
        await file_manager.download_file(project_id, file_id, file_path, size, checksum)
        logger.info(f"Queued download of file: {file_path}")
    except Exception as e:
        logger.error(f"Failed to queue download of file: {e}")

async def handle_pause_download(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] pause_download -> {cmd}")

async def handle_resume_download(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] resume_download -> {cmd}")

async def handle_cancel_download(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] cancel_download -> {cmd}")