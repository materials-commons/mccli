import asyncio
import os
from pathlib import Path
from typing import Dict, Any, Optional, Callable

from materials_commons.cli.desktop.downloader.file_downloader import FileDownloader, logger


class FileDownloadManager:
    """Manages multiple concurrent file downloads"""

    def __init__(self, send_queue: asyncio.Queue, client_id: str, max_concurrent: int = 3):
        self.send_queue = send_queue
        self.client_id = client_id
        self.max_concurrent = max_concurrent

        # Active downloads indexed by transfer_id
        self.active_downloads: Dict[str, FileDownloader] = {}
        self.download_queue: asyncio.Queue = asyncio.Queue()
        self.results: Dict[str, bool] = {}
        self._workers_running = False

    async def start_workers(self):
        """Start background workers to process download queue"""
        self._workers_running = True
        workers = [
            asyncio.create_task(self._download_worker(i))
            for i in range(self.max_concurrent)
        ]
        return workers

    async def _download_worker(self, worker_id: int):
        """Worker that processes downloads from queue"""
        logger.info(f"Download worker {worker_id} started")

        while self._workers_running:
            try:
                # Get download from queue with timeout
                downloader = await asyncio.wait_for(
                    self.download_queue.get(),
                    timeout=1.0
                )
            except asyncio.TimeoutError:
                continue

            try:
                logger.info(f"Worker {worker_id} starting download: {downloader.file_path.name}")
                self.active_downloads[downloader.transfer_id] = downloader

                success = await downloader.download()
                self.results[downloader.transfer_id] = success

                del self.active_downloads[downloader.transfer_id]

                if success:
                    logger.info(f"Worker {worker_id} completed: {downloader.file_path.name}")
                else:
                    logger.error(f"Worker {worker_id} failed: {downloader.file_path.name}")

            except Exception as e:
                logger.error(f"Worker {worker_id} error: {e}")
                self.results[downloader.transfer_id] = False
                if downloader.transfer_id in self.active_downloads:
                    del self.active_downloads[downloader.transfer_id]

            finally:
                self.download_queue.task_done()

    async def download_file(
            self,
            file_id: int,
            file_path: str,
            download_url: str,
            project_id: int,
            expected_size: Optional[int] = None,
            expected_checksum: Optional[str] = None,
            chunk_size: int = 1024 * 1024,
            progress_callback: Optional[Callable[[int, int], None]] = None
    ) -> str:
        """
        Queue a file for download. Returns transfer_id.
        """
        downloader = FileDownloader(
            send_queue=self.send_queue,
            file_id=file_id,
            file_path=file_path,
            download_url=download_url,
            project_id=project_id,
            client_id=self.client_id,
            expected_size=expected_size,
            expected_checksum=expected_checksum,
            chunk_size=chunk_size,
            progress_callback=progress_callback
        )

        await self.download_queue.put(downloader)
        logger.info(f"Queued download: {file_path} (transfer_id: {downloader.transfer_id})")

        return downloader.transfer_id

    async def download_directory(
            self,
            directory_id: int,
            dest_path: str,
            files: list[Dict[str, Any]],
            project_id: int,
            base_url: str,
            chunk_size: int = 1024 * 1024,
            progress_callback: Optional[Callable[[str, int, int], None]] = None
    ) -> list[str]:
        """
        Download all files in a directory. Returns list of transfer_ids.

        Args:
            directory_id: Server-side directory ID
            dest_path: Local destination directory path
            files: List of file metadata dicts with keys: id, name, path, size, checksum
            project_id: Project ID
            base_url: Base URL for file downloads (e.g., "https://api.mc.org/api/v3")
            chunk_size: Chunk size for streaming
            progress_callback: Optional callback for progress updates
        """
        dest_path = Path(dest_path)
        dest_path.mkdir(parents=True, exist_ok=True)

        transfer_ids = []

        for file_info in files:
            file_id = file_info['id']
            file_name = file_info['name']
            file_relative_path = file_info.get('path', '')  # Relative path within directory
            file_size = file_info.get('size')
            file_checksum = file_info.get('checksum')

            # Construct local destination path
            if file_relative_path:
                local_file_path = dest_path / file_relative_path / file_name
            else:
                local_file_path = dest_path / file_name

            # Ensure parent directory exists
            local_file_path.parent.mkdir(parents=True, exist_ok=True)

            # Construct download URL
            download_url = f"{base_url}/projects/{project_id}/files/{file_id}/download"

            logger.info(f"Queueing download: {file_name} -> {local_file_path}")

            transfer_id = await self.download_file(
                file_id=file_id,
                file_path=str(local_file_path),
                download_url=download_url,
                project_id=project_id,
                expected_size=file_size,
                expected_checksum=file_checksum,
                chunk_size=chunk_size,
                progress_callback=lambda sent, total, fname=file_name: (
                    progress_callback(fname, sent, total) if progress_callback else None
                )
            )
            transfer_ids.append(transfer_id)

        logger.info(f"Queued {len(transfer_ids)} files for download to {dest_path}")
        return transfer_ids

    def pause_download(self, transfer_id: str):
        """Pause an active download"""
        if transfer_id in self.active_downloads:
            self.active_downloads[transfer_id].pause()
            logger.info(f"Paused download: {transfer_id}")

    def resume_download(self, transfer_id: str):
        """Resume a paused download"""
        if transfer_id in self.active_downloads:
            self.active_downloads[transfer_id].resume_pause()
            logger.info(f"Resumed download: {transfer_id}")

    def cancel_download(self, transfer_id: str):
        """Cancel an active download"""
        if transfer_id in self.active_downloads:
            self.active_downloads[transfer_id].cancel()
            logger.info(f"Cancelled download: {transfer_id}")

    async def wait_all(self):
        """Wait for all queued downloads to complete"""
        await self.download_queue.join()

    def get_active_downloads(self) -> list[Dict[str, Any]]:
        """Get list of currently active downloads"""
        return [
            {
                "transfer_id": downloader.transfer_id,
                "file_name": downloader.file_path.name,
                "bytes_received": downloader.bytes_received,
                "file_size": downloader.expected_size,
                "progress_pct": (
                    (downloader.bytes_received / downloader.expected_size) * 100
                    if downloader.expected_size else 0
                )
            }
            for downloader in self.active_downloads.values()
        ]

    def stop_workers(self):
        """Stop all worker tasks"""
        self._workers_running = False
