import asyncio
import json
import os
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional, Callable, Dict, Any
import hashlib
import logging

logger = logging.getLogger(__name__)


class FileUploader:
    """Handles upload of a single file via websocket using queue pattern"""

    def __init__(
            self,
            send_queue: asyncio.Queue,
            file_path: str,
            project_path: str,
            project_id: int,
            client_id: str,
            chunk_size: int = 1024 * 1024,  # 1MB default
            progress_callback: Optional[Callable[[int, int], None]] = None
    ):
        self.send_queue = send_queue
        self.file_path = Path(file_path)
        self.project_path = Path(project_path)
        self.project_id = project_id
        self.client_id = client_id
        self.chunk_size = chunk_size
        self.progress_callback = progress_callback

        self.transfer_id = str(uuid.uuid4())
        self.file_size = self.file_path.stat().st_size
        self.bytes_sent = 0
        self.paused = False
        self.cancelled = False

        # Response handling
        self.response_queue: asyncio.Queue = asyncio.Queue()
        self.waiting_for_response: Optional[str] = None  # Track what we're waiting for

    async def upload(self) -> bool:
        """
        Upload the file. Returns True on success, False on failure.
        """
        try:
            # Step 1: Initialize the transfer
            if not await self._send_transfer_init():
                return False

            # Step 2: Wait for acceptance
            if not await self._wait_for_acceptance():
                return False

            # Step 3: Send file chunks
            if not await self._send_chunks():
                return False

            # Step 4: Send completion
            if not await self._send_transfer_complete():
                return False

            # Step 5: Wait for finalization
            if not await self._wait_for_finalization():
                return False

            logger.info(f"File upload complete for {self.file_path}")
            return True

        except Exception as e:
            logger.error(f"Error uploading file {self.file_path}: {e}")
            return False

    async def resume(self, resume_from_byte: int, resume_from_chunk: int) -> bool:
        """Resume upload from a specific point"""
        self.bytes_sent = resume_from_byte
        logger.info(f"Resuming upload from byte {resume_from_byte} (chunk {resume_from_chunk})")

        # Send chunks starting from the resume point
        if not await self._send_chunks(start_chunk=resume_from_chunk):
            return False

        # Complete the transfer
        if not await self._send_transfer_complete():
            return False

        # Wait for finalization
        return await self._wait_for_finalization()

    async def handle_response(self, msg: Dict[str, Any]) -> None:
        """
        Called by the main receiver loop when a message for this transfer arrives.
        """
        await self.response_queue.put(msg)

    async def _send_transfer_init(self) -> bool:
        """Send TRANSFER_INIT message via queue"""
        msg = {
            "command": "TRANSFER_INIT",
            "id": str(uuid.uuid4()),
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "client_id": self.client_id,
            "payload": {
                "transfer_id": self.transfer_id,
                "project_id": self.project_id,
                "file_path": self.file_path.as_posix(),
                "project_path": self.project_path.as_posix(),
                "file_size": self.file_size,
                "chunk_size": self.chunk_size,
                "checksum": self._calculate_md5() if self.file_size < 100 * 1024 * 1024 else ""
            }
        }

        await self.send_queue.put(msg)
        logger.info(f"Sent TRANSFER_INIT for {self.file_path.name} ({self.file_size} bytes)")
        return True

    async def _wait_for_acceptance(self) -> bool:
        """Wait for TRANSFER_ACCEPT or TRANSFER_REJECT"""
        self.waiting_for_response = "TRANSFER_ACCEPT"

        try:
            msg = await asyncio.wait_for(self.response_queue.get(), timeout=30.0)
        except asyncio.TimeoutError:
            logger.error("Timeout waiting for TRANSFER_ACCEPT")
            return False
        finally:
            self.waiting_for_response = None

        if msg["command"] == "TRANSFER_ACCEPT":
            # Server might adjust chunk size
            server_chunk_size = msg["payload"].get("chunk_size", self.chunk_size)
            if server_chunk_size != self.chunk_size:
                logger.info(f"Server adjusted chunk size: {self.chunk_size} -> {server_chunk_size}")
                self.chunk_size = server_chunk_size
            return True

        elif msg["command"] == "TRANSFER_REJECT":
            reason = msg["payload"].get("reason", "unknown")
            logger.error(f"Transfer rejected: {reason}")
            return False

        logger.error(f"Unexpected response: {msg['command']}")
        return False

    async def _send_chunks(self, start_chunk: int = 0) -> bool:
        """Send file chunks as binary frames via queue"""
        sequence = start_chunk

        with open(self.file_path, 'rb') as f:
            # Seek to start position if resuming
            if start_chunk > 0:
                f.seek(start_chunk * self.chunk_size)

            while not self.cancelled:
                # Wait if paused
                while self.paused and not self.cancelled:
                    await asyncio.sleep(0.1)

                if self.cancelled:
                    return False

                # Read chunk
                chunk = f.read(self.chunk_size)
                if not chunk:
                    break

                # Build binary frame: JSON header + newline + chunk data
                # NOTE: We'll send this as a special dict with a marker for binary
                header = {
                    "transfer_id": self.transfer_id,
                    "sequence": sequence,
                    "size": len(chunk),
                    "is_last": False
                }

                # Package as binary frame indicator
                binary_frame = {
                    "_binary_frame": True,
                    "header": header,
                    "data": chunk
                }

                await self.send_queue.put(binary_frame)

                # Wait for ACK
                self.waiting_for_response = "CHUNK_ACK"
                try:
                    ack_msg = await asyncio.wait_for(self.response_queue.get(), timeout=10.0)
                except asyncio.TimeoutError:
                    logger.error(f"Timeout waiting for CHUNK_ACK (seq {sequence})")
                    return False
                finally:
                    self.waiting_for_response = None

                if ack_msg["command"] == "CHUNK_ACK":
                    self.bytes_sent = ack_msg["payload"]["bytes_received"]
                    sequence += 1

                    # Progress callback
                    if self.progress_callback:
                        self.progress_callback(self.bytes_sent, self.file_size)

                elif ack_msg["command"] == "CHUNK_ERROR":
                    error = ack_msg["payload"].get("error", "unknown")
                    logger.error(f"Chunk error: {error}")
                    return False

                else:
                    logger.error(f"Unexpected response: {ack_msg['command']}")
                    return False
        return True

    async def _send_transfer_complete(self) -> bool:
        """Send TRANSFER_COMPLETE message via queue"""
        msg = {
            "command": "TRANSFER_COMPLETE",
            "id": str(uuid.uuid4()),
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "client_id": self.client_id,
            "payload": {
                "transfer_id": self.transfer_id,
                "total_bytes": self.bytes_sent
            }
        }

        await self.send_queue.put(msg)
        logger.info(f"Sent TRANSFER_COMPLETE for {self.file_path.name}")
        return True

    async def _wait_for_finalization(self) -> bool:
        """Wait for TRANSFER_FINALIZE or error"""
        self.waiting_for_response = "TRANSFER_FINALIZE"

        try:
            msg = await asyncio.wait_for(self.response_queue.get(), timeout=30.0)
        except asyncio.TimeoutError:
            logger.error("Timeout waiting for TRANSFER_FINALIZE")
            return False
        finally:
            self.waiting_for_response = None

        if msg["command"] == "TRANSFER_FINALIZE":
            logger.info(f"Transfer finalized: {self.file_path.name}")
            return True

        elif msg["command"] == "UPLOAD_FAILED":
            error = msg["payload"].get("error", "unknown")
            logger.error(f"Transfer failed: {error}")
            return False

        logger.error(f"Unexpected response: {msg['command']}")
        return False

    def pause(self):
        """Pause the upload"""
        self.paused = True
        logger.info(f"Upload paused: {self.file_path.name}")

    def resume_pause(self):
        """Resume from pause"""
        self.paused = False
        logger.info(f"Upload resumed: {self.file_path.name}")

    def cancel(self):
        """Cancel the upload"""
        self.cancelled = True
        logger.info(f"Upload cancelled: {self.file_path.name}")

    def _calculate_md5(self, chunk_size=8192) -> str:
        """Calculate md5 hash of file"""
        md5_hash = hashlib.md5()
        with open(self.file_path, "rb") as f:
            while True:
                chunk = f.read(chunk_size)
                if not chunk:
                    break
                md5_hash.update(chunk)
        return md5_hash.hexdigest()


class FileTransferManager:
    """Manages multiple concurrent file uploads"""

    def __init__(self, send_queue: asyncio.Queue, client_id: str, max_concurrent: int = 3):
        self.send_queue = send_queue
        self.client_id = client_id
        self.max_concurrent = max_concurrent

        # Active uploads indexed by transfer_id
        self.active_uploads: Dict[str, FileUploader] = {}
        self.upload_queue: asyncio.Queue = asyncio.Queue()
        self.results: Dict[str, bool] = {}

        self._semaphore = asyncio.Semaphore(max_concurrent)
        self._workers_running = False

    async def start_workers(self):
        """Start background workers to process upload queue"""
        self._workers_running = True
        workers = [
            asyncio.create_task(self._upload_worker(i))
            for i in range(self.max_concurrent)
        ]
        return workers

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
                async with self._semaphore:
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
            file_path: str,
            project_id: int,
            project_path: str,
            chunk_size: int = 1024 * 1024,
            progress_callback: Optional[Callable[[int, int], None]] = None
    ) -> str:
        """
        Queue a file for upload. Returns transfer_id.
        """
        uploader = FileUploader(
            send_queue=self.send_queue,
            file_path=file_path,
            project_path=project_path,
            project_id=project_id,
            client_id=self.client_id,
            chunk_size=chunk_size,
            progress_callback=progress_callback
        )

        await self.upload_queue.put(uploader)
        logger.info(f"Queued upload: {file_path} (transfer_id: {uploader.transfer_id})")

        return uploader.transfer_id

    async def upload_directory(
            self,
            dir_path: str,
            project_id: int,
            directory_id: int,
            recursive: bool = True,
            progress_callback: Optional[Callable[[str, int, int], None]] = None
    ) -> list[str]:
        """
        Upload all files in a directory. Returns list of transfer_ids.
        """
        dir_path = Path(dir_path)
        transfer_ids = []

        if recursive:
            files = dir_path.rglob('*')
        else:
            files = dir_path.glob('*')

        for file_path in files:
            if file_path.is_file():
                # Wrap progress callback with filename
                file_progress = None
                if progress_callback:
                    file_progress = lambda sent, total, fname=file_path.name: \
                        progress_callback(fname, sent, total)

                transfer_id = await self.upload_file(
                    str(file_path),
                    project_id,
                    directory_id,
                    progress_callback=file_progress
                )
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

    def stop_workers(self):
        """Stop all worker tasks"""
        self._workers_running = False
