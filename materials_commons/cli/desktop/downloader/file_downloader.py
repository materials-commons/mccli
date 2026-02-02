# materials_commons/cli/desktop/downloader/file_downloader.py
import asyncio
import hashlib
import json
import logging
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional, Callable, Dict, Any
import requests

logger = logging.getLogger(__name__)


class FileDownloader:
    """Handles download of a single file via HTTP with resume capability"""

    def __init__(
            self,
            send_queue: asyncio.Queue,
            file_id: int,
            file_path: str,  # Destination path on local filesystem
            base_url: str,  # REST endpoint URL
            project_id: int,
            client_id: str,
            apitoken: str,
            expected_size: Optional[int] = None,
            expected_checksum: Optional[str] = None,
            chunk_size: int = 1024 * 1024,  # 1MB chunks for streaming
            progress_callback: Optional[Callable[[int, int], None]] = None
    ):
        self.send_queue = send_queue
        self.file_id = file_id
        self.file_path = Path(file_path)
        self.base_url = base_url
        self.apitoken = apitoken
        self.project_id = project_id
        self.client_id = client_id
        self.expected_size = expected_size
        self.expected_checksum = expected_checksum
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
            # Check if we can resume
            resume_from = 0
            if await self._can_resume():
                resume_from = self.bytes_received
                logger.info(f"Resuming download from byte {resume_from}")

            # Download the file
            print("calling download with ranges")
            if not await self._download_with_ranges(resume_from):
                return False
            print("download complete")

            # Verify checksum if provided
            if self.expected_checksum:
                if not await self._verify_checksum():
                    logger.error("Checksum verification failed")
                    return False

            # Move .part file to final destination
            self.part_file.rename(self.file_path)

            # Clean up metadata
            if self.meta_file.exists():
                self.meta_file.unlink()

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

            # Get current size of partial file
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
        """Download file using HTTP Range requests with streaming"""
        headers = {
            'Authorization': f'Bearer {self.apitoken}',
        }

        # Add Range header if resuming
        if resume_from > 0:
            headers['Range'] = f'bytes={resume_from}-'

        try:
            # Use asyncio to run requests in thread pool (requests is blocking)
            loop = asyncio.get_event_loop()
            response = await loop.run_in_executor(
                None,
                lambda: requests.get(
                    self.download_url,
                    verify=False,
                    headers=headers,
                    stream=True,
                    timeout=30
                )
            )

            # Check if server supports range requests
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

            # Open file in append mode if resuming, write mode otherwise
            mode = 'ab' if resume_from > 0 else 'wb'

            with open(self.part_file, mode) as f:
                for chunk in response.iter_content(chunk_size=self.chunk_size):
                    # Check for pause/cancel
                    while self.paused and not self.cancelled:
                        await asyncio.sleep(0.1)

                    if self.cancelled:
                        logger.info("Download cancelled")
                        await self._save_metadata()
                        return False

                    if chunk:
                        # Write chunk (blocking I/O)
                        await loop.run_in_executor(None, f.write, chunk)
                        self.bytes_received += len(chunk)

                        # Progress callback
                        if self.progress_callback:
                            self.progress_callback(self.bytes_received, self.expected_size)

                        # Periodically save metadata for resume
                        if self.bytes_received % (self.chunk_size * 10) == 0:
                            await self._save_metadata()

            return True

        except requests.exceptions.RequestException as e:
            logger.error(f"HTTP request failed: {e}")
            await self._save_metadata()
            return False
        except Exception as e:
            logger.error(f"Download error: {e}")
            await self._save_metadata()
            return False

    async def _save_metadata(self):
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
        """Verify MD5 checksum of downloaded file"""
        loop = asyncio.get_event_loop()
        actual_checksum = await loop.run_in_executor(None, self._calculate_md5, self.part_file)

        if actual_checksum != self.expected_checksum:
            logger.error(f"Checksum mismatch: expected {self.expected_checksum}, got {actual_checksum}")
            return False

        logger.info("Checksum verification passed")
        return True

    def _calculate_md5(self, file_path: Path, chunk_size=8192) -> str:
        """Calculate MD5 hash of file"""
        md5_hash = hashlib.md5()
        with open(file_path, "rb") as f:
            while True:
                chunk = f.read(chunk_size)
                if not chunk:
                    break
                md5_hash.update(chunk)
        return md5_hash.hexdigest()

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

        await self.send_queue.put(msg)

    def _get_token(self) -> str:
        """Get authentication token (implement based on your auth system)"""
        # TODO: Retrieve from your config/session
        from materials_commons.cli import Config
        return Config.instance().token

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
