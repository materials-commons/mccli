import asyncio
import json
import logging
import time
import uuid
from dataclasses import replace
from datetime import datetime, timezone
from os import makedirs
from typing import Optional, Callable

import requests

from materials_commons.cli.old.functions import checksum
from materials_commons.cli.requests import DownloadRequest, DBWriteRequest
from materials_commons.cli.server import projects
from materials_commons.cli.terminal_progress import TerminalProgress

logger = logging.getLogger(__name__)


class FileDownloader:
    """Handles download of a single file via HTTP with resume capability"""

    def __init__(
            self,
            send_queue: Optional[asyncio.Queue],
            db_write_queue: asyncio.Queue,
            download_request: DownloadRequest,
            client_id: Optional[str],
            chunk_size: int = 1024 * 1024,  # 1MB chunks for streaming
            progress_callback: Optional[Callable[[int, int], None]] = None
    ):
        self.send_queue = send_queue
        self.db_write_queue = db_write_queue
        self.download_request = download_request
        self.file_id = download_request.observation.remote_entry.remote_file_id
        if download_request.observation.local_path is None:
            self.file_path = projects.remote_to_local_project_path(download_request.project.local_path, download_request.observation.remote_path)
        else:
            self.file_path = download_request.observation.local_path
        self.base_url = download_request.project.remote.base_url
        self.apitoken = download_request.project.remote.apikey
        self.project_id = download_request.project.id
        self.client_id = client_id
        self.expected_size = download_request.observation.remote_entry.size
        self.expected_checksum = download_request.observation.remote_entry.checksum
        self.chunk_size = chunk_size
        self.progress_callback = progress_callback

        self.transfer_id = str(uuid.uuid4())
        self.bytes_received = 0
        self.paused = False
        self.cancelled = False

        # Paths for partial download tracking
        self.part_file = self.file_path.with_suffix(self.file_path.suffix + '.part')
        self.meta_file = self.file_path.with_suffix(self.file_path.suffix + '.meta.json')

        self.download_url = f"{self.base_url}/projects/{self.project_id}/files/{self.file_id}/download"

    async def download(self) -> bool:
        """
        Download the file with resume support. Returns True on success.
        """
        try:
            # Make sure the destination directory exists
            dir_path = self.file_path.resolve().parent
            await asyncio.to_thread(makedirs,dir_path.as_posix(), exist_ok=True)

            # Check if we can resume
            resume_from = 0
            if await self._can_resume():
                resume_from = self.bytes_received
                logger.info(f"Resuming download from byte {resume_from}")

            # Download the file
            if not await self._download_with_ranges(resume_from):
                await self._send_completion_message(success=False, error="Download failed")
                await self._send_completion_message(success=False, error="Checksum verification failed")
                return False

            # Verify checksum if provided
            if self.expected_checksum:
                if not await self._verify_checksum():
                    logger.error("Checksum verification failed")
                    return False

            # Move .part file to the final destination
            self.part_file.rename(self.file_path.as_posix())

            # Clean up metadata
            if self.meta_file.exists():
                self.meta_file.unlink()

            # Get info so we can set the database
            sinfo = self.file_path.stat()

            # TODO: Need to get the remote_ctime_ns somehow

            updated_record = replace(self.download_request.updated_record,
                                     remote_checksum=self.expected_checksum,
                                     local_size=self.expected_size,
                                     local_checksum=self.expected_checksum,
                                     local_mtime_ns=sinfo.st_mtime_ns,
                                     local_ctime_ns=sinfo.st_ctime_ns,
                                     remote_last_seen_ts=int(time.time()),
                                     local_last_seen_ts=int(time.time()),
                                     remote_size=self.expected_size,
                                     remote_file_id=self.file_id)
            db_write_request = DBWriteRequest(project=self.download_request.project,
                                              data=updated_record,
                                              command="single")
            await self.db_write_queue.put(db_write_request)
            logger.info(f"Download complete: {self.file_path}")
            await self._send_completion_message(success=True)
            return True

        except Exception as e:
            logger.error(f"Download failed: {e}")
            await self._save_metadata()  # Save state for resume
            await self._send_completion_message(success=False, error=str(e))
            return False

    async def _can_resume(self) -> bool:
        """Check if we can resume a previous download"""
        if not self.part_file.exists() or not self.meta_file.exists():
            return False

        try:
            with open(self.meta_file, 'r') as f:
                metadata = json.load(f)

            # Validate metadata
            if metadata.get('file_id') != self.file_id:
                return False

            if metadata.get('transfer_id') != self.transfer_id:
                # Different transfer, can't resume
                return False

            # Get the current size of the partial file
            partial_size = self.part_file.stat().st_size
            bytes_downloaded = metadata.get('bytes_downloaded', 0)

            if partial_size != bytes_downloaded:
                logger.warning("Partial file size mismatch with metadata")
                return False

            self.bytes_received = bytes_downloaded
            return True

        except Exception as e:
            logger.error(f"Error reading resume metadata: {e}")
            return False

    async def _download_with_ranges(self, resume_from: int = 0) -> bool:
        """Download the file using HTTP Range requests with streaming"""

        # Get the event loop so we can schedule progress rendering. Since the routine
        # _download_with_ranges_blocking is not async it will need the loop do the
        # progress. We have to get the async loop here in the async routine, and then
        # pass it to the blocking download routine.
        loop = asyncio.get_running_loop()
        try:
            await self._render_progress(status="starting")
            success = await asyncio.to_thread(self._download_with_ranges_blocking, resume_from, loop)

            if not success:
                await self._save_metadata()
                await self._render_progress(status="failed downloading")
                return False

            return success

        except requests.exceptions.RequestException as e:
            logger.error(f"HTTP request failed: {e}")
            await self._save_metadata()
            await self._render_progress(status=f"failed with HTTP exception: {e}")
            return False
        except Exception as e:
            logger.error(f"Download error: {e}")
            await self._save_metadata()
            await self._render_progress(status=f"failed with exception: {e}")
            return False

    def _download_with_ranges_blocking(self, resume_from: int = 0, loop: Optional[asyncio.AbstractEventLoop] = None) -> bool:
        """Blocking download implementation. Runs in a worker thread."""
        headers = {
            'Authorization': f'Bearer {self.apitoken}',
        }

        # Add Range header if resuming
        if resume_from > 0:
            headers['Range'] = f'bytes={resume_from}-'

        response = None

        try:
            response = requests.get(
                self.download_url,
                verify=False,
                headers=headers,
                stream=True,
                timeout=30
            )

            # Check if the server supports range requests
            if resume_from > 0 and response.status_code != 206:
                logger.warning("Server doesn't support range requests, starting from beginning")
                resume_from = 0
                self.bytes_received = 0

            if response.status_code not in [200, 206]:
                logger.error(f"Download failed with status {response.status_code}")
                return False

            # Get total size from headers
            if 'Content-Length' in response.headers:
                content_length = int(response.headers['Content-Length'])
                if resume_from > 0:
                    self.expected_size = resume_from + content_length
                else:
                    self.expected_size = content_length

            # Open the file in append mode if resuming, write mode otherwise
            mode = 'ab' if resume_from > 0 else 'wb'
            last_progress_render_at = 0.0
            progress_interval_seconds = 0.25

            def schedule_progress_render(status: str = "downloading", force: bool = False) -> None:
                nonlocal last_progress_render_at

                if loop is None:
                    return

                now = time.monotonic()
                if not force and now - last_progress_render_at < progress_interval_seconds:
                    return

                last_progress_render_at = now
                asyncio.run_coroutine_threadsafe(
                    self._render_progress(self.bytes_received, self.expected_size, status=status),
                    loop,
                )

            schedule_progress_render(status="downloading", force=True)

            with open(self.part_file, mode) as f:
                for chunk in response.iter_content(chunk_size=self.chunk_size):
                    # Check for pause/cancel
                    while self.paused and not self.cancelled:
                        time.sleep(0.1)

                    if self.cancelled:
                        logger.info("Download cancelled")
                        schedule_progress_render(status="cancelled", force=True)
                        return False

                    if chunk:
                        # Write chunk
                        f.write(chunk)
                        self.bytes_received += len(chunk)

                        schedule_progress_render()

                        # Progress callback
                        if self.progress_callback:
                            self.progress_callback(self.bytes_received, self.expected_size)

                        # Periodically save metadata for resume
                        if self.bytes_received % (self.chunk_size * 10) == 0:
                            self._save_metadata_blocking()

            schedule_progress_render(status="download complete", force=True)

            return True

        finally:
            if response is not None:
                response.close()

    async def _save_metadata(self):
        """Save download state for resume capability"""
        await asyncio.to_thread(self._save_metadata_blocking)

    def _save_metadata_blocking(self):
        """Save download state for resume capability"""
        metadata = {
            'transfer_id': self.transfer_id,
            'file_id': self.file_id,
            'file_path': str(self.file_path),
            'download_url': self.download_url,
            'bytes_downloaded': self.bytes_received,
            'total_size': self.expected_size,
            'expected_checksum': self.expected_checksum,
            'timestamp': datetime.now(timezone.utc).isoformat()
        }

        try:
            with open(self.meta_file, 'w') as f:
                json.dump(metadata, f, indent=2)
        except Exception as e:
            logger.error(f"Failed to save metadata: {e}")

    async def _verify_checksum(self) -> bool:
        """Verify MD5 checksum of the downloaded file"""
        actual_checksum = await asyncio.to_thread(checksum, self.part_file.as_posix())

        if actual_checksum != self.expected_checksum:
            logger.error(f"Checksum mismatch: expected {self.expected_checksum}, got {actual_checksum}")
            return False

        logger.info("Checksum verification passed")
        return True

    async def _send_completion_message(self, success: bool, error: str = None):
        """Send download completion message via websocket"""
        msg = {
            "command": "DOWNLOAD_COMPLETE" if success else "DOWNLOAD_FAILED",
            "id": str(uuid.uuid4()),
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "clientId": self.client_id,
            "payload": {
                "transfer_id": self.transfer_id,
                "file_id": self.file_id,
                "file_path": str(self.file_path),
                "bytes_received": self.bytes_received,
                "success": success
            }
        }

        if error:
            msg["payload"]["error"] = error

        if self.send_queue:
            await self.send_queue.put(msg)

    def pause(self):
        """Pause the download"""
        self.paused = True
        logger.info(f"Download paused: {self.file_path}")

    def resume_pause(self):
        """Resume from pause"""
        self.paused = False
        logger.info(f"Download resumed: {self.file_path}")

    def cancel(self):
        """Cancel the download"""
        self.cancelled = True
        logger.info(f"Download cancelled: {self.file_path}")

    async def _render_progress(self,
                               bytes_received: Optional[int] = None,
                               total_bytes: Optional[int] = None,
                               status: str = "downloading") -> None:
        """Render this download's progress on a stable terminal row."""
        bytes_received = self.bytes_received if bytes_received is None else bytes_received
        total_bytes = self.expected_size if total_bytes is None else total_bytes

        percent = 100.0 if total_bytes == 0 else min((bytes_received / total_bytes) * 100.0, 100.0)

        line = (
            f"{self.file_path.as_posix()} | "
            f"download | "
            f"{bytes_received} / {total_bytes} bytes | "
            f"{percent:6.2f}% | "
            f"{status}"
        )

        await TerminalProgress.render(f"download:{self.transfer_id}", line,
                                      done=status not in ("downloading", "starting"))
