import asyncio
import logging
import os
from typing import Optional, Dict, Any

from materials_commons.cli import server
from materials_commons.cli.server.command_handlers.protocol import CommandHandlerLookup, HandlerFunc
from materials_commons.cli.server.downloader.file_download_manager import FileDownloadManager

logger = logging.getLogger(__name__)


class DownloadHandlerLookup(CommandHandlerLookup):
    """Handles transfer commands for Materials Commons CLI server."""

    def __init__(self, file_download_manager: FileDownloadManager):
        self._file_download_manager = file_download_manager
        self._handlers: Dict[str, HandlerFunc] = {
            # Download commands
            "DOWNLOAD_FILE": self._handle_download_file,
            "PAUSE_DOWNLOAD": self._handle_pause_download,
            "RESUME_DOWNLOAD": self._handle_resume_download,
            "CANCEL_DOWNLOAD": self._handle_cancel_download,
        }

    # Implement CommandHandlerLookup protocol
    def get_handler(self, cmd: str) -> Optional[HandlerFunc]:
        return self._handlers.get(cmd)

    async def _handle_download_file(self, queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
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
        # print(f"[handler] download_file -> {cmd}")
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
            p = server.get_local_project_by_id(project_id)
            if not p:
                logger.error(f"Project {project_id} not found in local projects")
                return
            file_path = os.path.join(p["project_dir_path"], project_path.lstrip("/"))

        try:
            # print(f"I would download to {file_path} with size {size} and checksum {checksum}, file_id {file_id}")
            await self._file_download_manager.download_file(project_id, file_id, file_path, size, checksum)
            logger.info(f"Queued download of file: {file_path}")
        except Exception as e:
            logger.error(f"Failed to queue download of file: {e}")

    async def _handle_pause_download(self, queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
        print(f"[handler] pause_download -> {cmd}")
        payload = cmd.get("payload") or {}
        transfer_id = payload.get("transfer_id")

        if not transfer_id:
            logger.error("Missing transfer_id in PAUSE_DOWNLOAD command")
            return

        self._file_download_manager.pause_download(transfer_id)

    async def _handle_resume_download(self, queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
        print(f"[handler] resume_download -> {cmd}")
        payload = cmd.get("payload") or {}
        transfer_id = payload.get("transfer_id")

        if not transfer_id:
            logger.error("Missing transfer_id in RESUME_DOWNLOAD command")
            return

        self._file_download_manager.resume_download(transfer_id)

    async def _handle_cancel_download(self, queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
        print(f"[handler] cancel_download -> {cmd}")
        payload = cmd.get("payload") or {}
        transfer_id = payload.get("transfer_id")

        if not transfer_id:
            logger.error("Missing transfer_id in CANCEL_DOWNLOAD command")
            return

        self._file_download_manager.cancel_download(transfer_id)
