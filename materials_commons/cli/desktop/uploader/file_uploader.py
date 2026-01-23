import asyncio
import hashlib
import logging
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional, Callable, Dict, Any

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
            window_size: int = 10,
            progress_callback: Optional[Callable[[int, int], None]] = None
    ):
        self.send_queue = send_queue
        self.file_path = Path(file_path)
        self.project_path = Path(project_path)
        self.project_id = project_id
        self.client_id = client_id
        self.chunk_size = chunk_size
        self.window_size = window_size
        self.progress_callback = progress_callback

        self.transfer_id = str(uuid.uuid4())
        self.file_size = self.file_path.stat().st_size
        self.bytes_sent = 0
        self.paused = False
        self.cancelled = False

        # Sliding window for chunk sending
        self.next_chunk_to_send = 0
        self.in_flight_chunks = 0
        self.last_acked_chunk = -1
        self._state_lock = asyncio.Lock()

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

            # # Step 3: Send file chunks
            # if not await self._send_chunks():
            #     return False

            ### New
            # Step 3: Start ACK processor and chunk sender concurrently
            process_acks_task = asyncio.create_task(self._process_acks())
            send_chunks_task = asyncio.create_task(self._send_chunks_windowed())

            # Wait for both to complete
            send_chunks_result = await send_chunks_task
            if not send_chunks_result:
                process_acks_task.cancel()
                return False

            # Wait for all ACKs to be received
            await process_acks_task
            ### End New

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
                "checksum": self._calculate_md5()
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

    async def _send_chunks2(self):
        # Start ACK processor and chunk sender concurrently
        process_acks_task = asyncio.create_task(self._process_acks())
        send_chunks_task = asyncio.create_task(self._send_chunks_windowed())

        # Wait for both to complete
        send_chunks_result = await send_chunks_task
        if not send_chunks_result:
            process_acks_task.cancel()
            return False

        # Wait for all ACKs to be received
        await process_acks_task

        # We don't care about the process_acks_task result, just return True.
        # If there are errors, it will be seen in later processing.
        return True

    async def _send_chunks_windowed(self, start_chunk: int = 0) -> bool:
        """Send file chunks using sliding window (don't wait for each ACK)"""
        total_chunks = (self.file_size + self.chunk_size - 1) // self.chunk_size

        with open(self.file_path, 'rb') as f:
            # Single seek to start position (for resume)
            if start_chunk > 0:
                f.seek(start_chunk * self.chunk_size)

            async with self._state_lock:
                self.next_chunk_to_send = start_chunk

            while True:
                # Wait if paused
                while self.paused and not self.cancelled:
                    await asyncio.sleep(0.1)

                if self.cancelled:
                    return False

                # Check if we can send (within window and not done)
                async with self._state_lock:
                    if self.next_chunk_to_send >= total_chunks:
                        break  # All chunks sent

                    # Wait if the window is full
                    if self.in_flight_chunks >= self.window_size:
                        # Release the lock while waiting
                        window_full = True
                    else:
                        # Reserve a slot in the window
                        window_full = False
                        chunk_seq = self.next_chunk_to_send
                        self.next_chunk_to_send += 1
                        self.in_flight_chunks += 1

                # If the window was full, wait and retry
                if window_full:
                    await asyncio.sleep(0.01)
                    continue

                # Read the next chunk (outside lock - file I/O can be slow)
                chunk = f.read(self.chunk_size)
                if not chunk:
                    break

                print(f"Sending chunk {chunk_seq} ({len(chunk)} bytes)")
                # Build binary frame
                header = {
                    "transfer_id": self.transfer_id,
                    "sequence": chunk_seq,
                    "size": len(chunk),
                    "is_last": (chunk_seq == total_chunks - 1)
                }

                binary_frame = {
                    "_binary_frame": True,
                    "header": header,
                    "data": chunk
                }

                await self.send_queue.put(binary_frame)

        return True

    async def _process_acks(self):
        """Process ACKs asynchronously"""
        total_chunks = (self.file_size + self.chunk_size - 1) // self.chunk_size

        while True:
            async with self._state_lock:
                if self.last_acked_chunk >= total_chunks - 1:
                    break  # All chunks ACKed

            try:
                # Wait for ACK with timeout (outside lock)
                ack_msg = await asyncio.wait_for(self.response_queue.get(), timeout=30.0)
            except asyncio.TimeoutError:
                logger.error("Timeout waiting for CHUNK_ACK")
                raise

            command = ack_msg.get("command")

            if command == "CHUNK_ACK":
                payload = ack_msg["payload"]
                chunk_seq = payload["chunk_sequence"]
                bytes_received = payload["bytes_received"]

                # Update the state under lock
                async with self._state_lock:
                    if chunk_seq > self.last_acked_chunk:
                        self.last_acked_chunk = chunk_seq

                        # Recalculate in-flight chunks
                        self.in_flight_chunks = (self.next_chunk_to_send - 1) - self.last_acked_chunk

                    self.bytes_sent = bytes_received

                # Progress callback outside lock
                if self.progress_callback:
                    self.progress_callback(self.bytes_sent, self.file_size)

            elif command == "CHUNK_ERROR":
                error = ack_msg["payload"].get("error", "unknown")
                logger.error(f"Chunk error: {error}")
                raise Exception(f"Chunk error: {error}")

        logger.debug(f"All chunks ACKed for {self.file_path.name}")

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


