import asyncio
import os
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, Any, Optional, Callable

from materials_commons.cli.requests import UploadRequest
from materials_commons.cli.server.uploader.file_uploader import FileUploader, logger


class FileUploadManager:
    """Manages multiple concurrent file uploads"""

    def __init__(self,
                 send_queue: asyncio.Queue,
                 db_write_queue: asyncio.Queue,
                 client_id: str,
                 max_concurrent: int = 3):
        self.send_queue = send_queue
        self.db_write_queue = db_write_queue
        self.client_id = client_id
        self.max_concurrent = max_concurrent

        # Active uploads indexed by transfer_id
        self.active_uploads: Dict[str, FileUploader] = {}
        self.upload_queue: asyncio.Queue = asyncio.Queue()
        self.results: Dict[str, bool] = {}
        self._workers_running = False
        self._worker_tasks: list[asyncio.Task] = []

    async def start_workers(self) -> None:
        """Start background workers to process upload queue"""
        self._workers_running = True
        self._worker_tasks = [
            asyncio.create_task(self._upload_worker(i))
            for i in range(self.max_concurrent)
        ]

    async def _upload_worker(self, worker_id: int):
        """Worker that processes uploads from queue"""
        logger.info(f"Upload worker {worker_id} started")

        while self._workers_running:
            try:
                # Get upload from queue with timeout
                uploader = await asyncio.wait_for(
                    self.upload_queue.get(),
                    timeout=1.0
                )
            except asyncio.TimeoutError:
                continue

            try:
                logger.info(f"Worker {worker_id} starting upload: {uploader.file_path.name}")
                self.active_uploads[uploader.transfer_id] = uploader

                success = await uploader.upload()
                self.results[uploader.transfer_id] = success

                del self.active_uploads[uploader.transfer_id]

                if success:
                    logger.info(f"Worker {worker_id} completed: {uploader.file_path.name}")
                else:
                    logger.error(f"Worker {worker_id} failed: {uploader.file_path.name}")

            except Exception as e:
                logger.error(f"Worker {worker_id} error: {e}")
                self.results[uploader.transfer_id] = False
                if uploader.transfer_id in self.active_uploads:
                    del self.active_uploads[uploader.transfer_id]

            finally:
                self.upload_queue.task_done()

    async def handle_message(self, msg: Dict[str, Any]):
        """
        Route incoming messages to the appropriate uploader.
        Called by the main receiver loop.
        """
        command = msg.get("command")

        # Messages that contain transfer_id in payload
        if command in ["TRANSFER_ACCEPT", "TRANSFER_REJECT", "CHUNK_ACK", "CHUNK_ERROR",
                       "TRANSFER_FINALIZE", "UPLOAD_FAILED", "TRANSFER_RESUME_RESPONSE"]:
            transfer_id = msg.get("payload", {}).get("transfer_id")
            if transfer_id and transfer_id in self.active_uploads:
                await self.active_uploads[transfer_id].handle_response(msg)

    async def upload_file(
            self,
            upload_request: UploadRequest,
            chunk_size: int = 1024 * 1024,
            progress_callback: Optional[Callable[[int, int], None]] = None
    ) -> str:
        """
        Queue a file for upload. Returns transfer_id. This works by creating an instance of FileUploader
        and then queueing this object. The queued FileUploader will then be picked up by a worker to perform
        the upload.
        """
        db = await upload_request.project.get_filedb()
        uploader = FileUploader(
            db=db,
            ws_send_queue=self.send_queue,
            db_write_queue=self.db_write_queue,
            upload_request=upload_request,
            client_id=self.client_id,
            chunk_size=chunk_size,
            progress_callback=progress_callback
        )

        file_path = upload_request.observation.path
        await self.upload_queue.put(uploader)
        logger.info(f"Queued upload: {file_path} (transfer_id: {uploader.transfer_id})")

        return uploader.transfer_id

    # TODO: This should be removed, and the place where its called should do an async_reconciler
    # walk and call the upload_file() method in this class.
    async def upload_directory(
            self,
            dest_dir: str,
            dir_path: str,
            project_id: int,
            recursive: bool = True,
            chunk_size: int = 1024 * 1024,
            progress_callback: Optional[Callable[[str, int, int], None]] = None
    ) -> list[str]:
        """
        Upload all files in a directory. Returns list of transfer_ids. This method walks the directory (or
        directory tree if recursive=True) and queues each file for upload by calling self.upload_file().
        """
        dir_path = Path(dir_path)
        transfer_ids = []

        def get_files():
            # Yields filepaths via recursive or shallow traversal
            if recursive:
                for root, _, filenames in os.walk(dir_path):
                    for filename in filenames:
                        yield os.path.join(root, filename)
            else:
                with os.scandir(dir_path) as d:
                    for entry in d:
                        if entry.is_file():
                            yield entry.path

        for file_path in get_files():
            # Let's get the destination for the file. First we need to get the file relative to the
            # dir_path we are walking. For example if dir_path is /home/user/proj/Aging
            # and file_path is /home/user/proj/Aging/B/C/D/file.txt then relative_path is B/C/D/file.txt
            relative_path = Path(file_path).relative_to(dir_path)

            # Now we want to construct the destination. That will be dest_dir/relative_path. For example.
            destination = Path(dest_dir) / relative_path

            transfer_id = await self.upload_file(file_path, project_id, str(destination), chunk_size, progress_callback)
            transfer_ids.append(transfer_id)

        logger.info(f"Queued {len(transfer_ids)} files from {dir_path}")
        return transfer_ids

    async def resume_transfer(self, transfer_id: str) -> bool:
        """
        Resume a previously interrupted transfer.
        """
        # Send TRANSFER_RESUME request via queue
        msg = {
            "command": "TRANSFER_RESUME",
            "id": str(uuid.uuid4()),
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "clientId": self.client_id,
            "payload": {
                "transfer_id": transfer_id
            }
        }

        await self.send_queue.put(msg)

        # TODO: Need to handle TRANSFER_RESUME_RESPONSE through handle_message
        logger.info(f"Sent TRANSFER_RESUME for {transfer_id}")

        return True

    def pause_upload(self, transfer_id: str):
        """Pause an active upload"""
        if transfer_id in self.active_uploads:
            self.active_uploads[transfer_id].pause()

    def resume_upload(self, transfer_id: str):
        """Resume a paused upload"""
        if transfer_id in self.active_uploads:
            self.active_uploads[transfer_id].resume_pause()

    def cancel_upload(self, transfer_id: str):
        """Cancel an active upload"""
        if transfer_id in self.active_uploads:
            self.active_uploads[transfer_id].cancel()

    async def wait_all(self):
        """Wait for all queued uploads to complete"""
        await self.upload_queue.join()

    def get_active_uploads(self) -> list[Dict[str, Any]]:
        """Get list of currently active uploads"""
        return [
            {
                "transfer_id": uploader.transfer_id,
                "file_name": uploader.file_path.name,
                "bytes_sent": uploader.bytes_sent,
                "file_size": uploader.file_size,
                "progress_pct": (uploader.bytes_sent / uploader.file_size) * 100
            }
            for uploader in self.active_uploads.values()
        ]

    async def stop_workers(self):
        """Stop all worker tasks"""
        self._workers_running = False
        for worker in self._worker_tasks:
            worker.cancel()
            try:
                await worker
            except asyncio.CancelledError:
                pass

    async def close_dbs(self):
        await self.project_filedbs.close_dbs()
