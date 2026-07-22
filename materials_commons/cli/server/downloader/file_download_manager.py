import asyncio
from typing import Dict, Any, Optional, Callable
import logging

from materials_commons.cli.requests import DownloadRequest
from materials_commons.cli.server.downloader.file_downloader import FileDownloader, logger

logger.setLevel(logging.WARNING)

class FileDownloadManager:
    """Manages multiple concurrent file downloads"""

    def __init__(self, send_queue: asyncio.Queue, db_write_queue: asyncio.Queue, client_id: str, mcurl: str, apitoken: str, max_concurrent: int = 3):
        self.send_queue = send_queue
        self.db_write_queue = db_write_queue
        self.client_id = client_id
        self.mcurl = mcurl
        self.apitoken = apitoken
        self.max_concurrent = max_concurrent

        # Active downloads indexed by transfer_id
        self.active_downloads: Dict[str, FileDownloader] = {}
        self.download_queue: asyncio.Queue = asyncio.Queue()
        self.results: Dict[str, bool] = {}
        self._workers_running = False
        self._worker_tasks: list[asyncio.Task] = []

    async def start_workers(self) -> None:
        """Start background workers to process download queue"""
        if self._workers_running:
            logger.info("Download workers already running")
            return
        self._workers_running = True
        self._worker_tasks = [
            asyncio.create_task(self._download_worker(i))
            for i in range(self.max_concurrent)
        ]

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
            download_request: DownloadRequest,
            chunk_size: int = 1024 * 1024,
            progress_callback: Optional[Callable[[int, int], None]] = None
    ) -> str:
        """
        Queue a file for download. Returns transfer_id.
        """
        downloader = FileDownloader(
            send_queue=self.send_queue,
            db_write_queue=self.db_write_queue,
            download_request=download_request,
            client_id=self.client_id,
            chunk_size=chunk_size,
            progress_callback=progress_callback
        )

        file_path = download_request.observation.local_path
        await self.download_queue.put(downloader)
        logger.info(f"Queued download: {file_path} (transfer_id: {downloader.transfer_id})")

        return downloader.transfer_id

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

    async def stop_workers(self):
        """Stop all worker tasks"""
        self._workers_running = False
        for worker in self._worker_tasks:
            worker.cancel()
            try:
                await worker
            except asyncio.CancelledError:
                pass
