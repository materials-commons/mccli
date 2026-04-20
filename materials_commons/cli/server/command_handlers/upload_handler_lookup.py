import asyncio
import logging
import os
from typing import Optional, Dict, Any

from materials_commons.cli import server
from materials_commons.cli.server.command_handlers.protocol import CommandHandlerLookup, HandlerFunc
from materials_commons.cli.server.uploader.file_upload_manager import FileUploadManager

logger = logging.getLogger(__name__)


class UploadHandlerLookup(CommandHandlerLookup):
    """Handles upload commands for Materials Commons CLI server."""

    def __init__(self, file_upload_manager: FileUploadManager):
        self._file_upload_manager = file_upload_manager
        self._handlers: Dict[str, HandlerFunc] = {
            # Upload commands
            "UPLOAD_FILE": self._handle_upload_file,
            "UPLOAD_DIRECTORY": self._handle_upload_directory,
            "CANCEL_UPLOAD": self._handle_cancel_upload,
            "UPLOAD_PAUSE": self._handle_pause_upload,
            "UPLOAD_RESUME": self._handle_resume_upload,

            # File Upload Subcommands
            # These subcommands are sent after an upload is accepted and
            # are used to manage the transfer process
            "TRANSFER_ACCEPT": self._handle_transfer_subcommand,
            "TRANSFER_REJECT": self._handle_transfer_subcommand,
            "TRANSFER_RESUME_RESPONSE": self._handle_transfer_subcommand,
            "TRANSFER_FINALIZE": self._handle_transfer_subcommand,
            "CHUNK_ACK": self._handle_transfer_subcommand,
            "CHUNK_ERROR": self._handle_transfer_subcommand,
            "UPLOAD_FAILED": self._handle_transfer_subcommand,
        }

    # Implement CommandHandlerLookup protocol
    def get_handler(self, cmd: str) -> Optional[HandlerFunc]:
        return self._handlers.get(cmd)

    async def _handle_upload_file(self, queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
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
        # print(f"[handler] upload_file_request -> {cmd}")
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
            p = server.get_local_project_by_id(project_id)
            if not p:
                logger.error(f"Project {project_id} not found in local projects")
                return
            file_path = os.path.join(p["project_dir_path"], project_path.lstrip("/"))

        logger.info(f"Received upload request for: {file_path}")

        try:
            transfer_id = await self._file_upload_manager.upload_file(
                file_path=file_path,
                project_id=project_id,
                project_path=project_path,
                chunk_size=chunk_size,
                progress_callback=lambda sent, total: logger.debug(f"{file_path}: {sent}/{total}")
            )
            logger.info(f"Queued upload: {file_path} (transfer_id: {transfer_id})")
        except Exception as e:
            logger.error(f"Failed to queue upload: {e}")

    async def _handle_upload_directory(self, queue: asyncio.Queue, cmd: Dict[str, Any]):
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

        p = server.get_local_project_by_id(project_id)
        if not p:
            logger.error(f"Project {project_id} not found in local projects")
            return

        if not local_directory_path:
            local_directory_path = os.path.join(p["project_dir_path"], mc_project_path.lstrip("/"))

        # print(f"Calling upload_directory with recursive={recursive}")
        # print(f"   dest_dir = {mc_project_path}")
        # print(f"   dir_path = {local_directory_path}")
        # print(f"   local_project_root = {p['project_dir_path']}")
        try:
            await self._file_upload_manager.upload_directory(
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

    async def _handle_cancel_upload(self, queue: asyncio.Queue, cmd: Dict[str, Any]):
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

        logger.info(f"Received cancel request for transfer: {transfer_id}")
        self._file_upload_manager.cancel_upload(transfer_id)

    async def _handle_pause_upload(self, queue: asyncio.Queue, cmd: Dict[str, Any]):
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

        logger.info(f"Received pause request for transfer: {transfer_id}")
        self._file_upload_manager.pause_upload(transfer_id)

    async def _handle_resume_upload(self, queue: asyncio.Queue, cmd: Dict[str, Any]):
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

        logger.info(f"Received resume request for transfer: {transfer_id}")
        self._file_upload_manager.resume_upload(transfer_id)

    async def _handle_transfer_subcommand(self, queue: asyncio.Queue, cmd: Dict[str, Any]):
        await self._file_upload_manager.handle_message(cmd)
