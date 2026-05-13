import asyncio
import os
from pathlib import Path
from typing import Optional

from materials_commons.cli.models import FileRecord
from materials_commons.cli.reconcile2 import observe_and_reconcile
from materials_commons.cli.requests import IndexRequest, DBWriteRequest


def file_has_changed(file_record: Optional[FileRecord], finfo: os.stat_result) -> bool:
    """Check if a file has changed since the last index. Returns True if file_record is None."""
    if file_record is None:
        return True

    return (
            file_record.local_size != finfo.st_size or
            file_record.local_mtime_ns != finfo.st_mtime_ns
    )


class FileIndexManager:
    """Manages multiple concurrent file indexers"""

    def __init__(self, db_queue: asyncio.Queue, index_queue: asyncio.Queue[IndexRequest],
                 max_concurrent: int = 3):
        self._db_queue = db_queue
        self._index_queue: asyncio.Queue[IndexRequest] = index_queue
        self._workers_running = False
        self._max_concurrent = max_concurrent
        self._worker_tasks: list[asyncio.Task] = []

    async def start_workers(self) -> None:
        """Start background workers to process the index queue"""
        self._workers_running = True
        self._worker_tasks = [
            asyncio.create_task(self._index_file_worker())
            for i in range(self._max_concurrent)
        ]

    async def _index_file_worker(self):
        """Worker that processes index requests from the indexer queue"""
        while self._workers_running:
            try:
                index_request: IndexRequest = await asyncio.wait_for(self._index_queue.get(), timeout=1.0)
            except asyncio.TimeoutError:
                continue

            try:
                await self._index(index_request)
            except Exception as e:
                print(f"Error indexing {index_request.file_path}: {e}")
            finally:
                self._index_queue.task_done()

    async def _index(self, index_request: IndexRequest):
        """Index a file. Checks if the file has changed since the last index."""
        db = await index_request.project.get_filedb()
        file_decision = await observe_and_reconcile(db=db,
                                                    project_path=Path(index_request.project_path).as_posix(),
                                                    file_path=Path(index_request.file_path).as_posix(),
                                                    remote_entry=index_request.remote_entry,
                                                    recompute_checksum=True)

        if not file_decision.updated_record:
            print(f"Skipping {index_request.file_path} in {index_request.project_path} as it hasn't changed")
            return

        db_write_request = DBWriteRequest(project=index_request.project, command="single",
                                          data=file_decision.updated_record)
        await self._db_queue.put(db_write_request)
        print(f"Indexed {index_request.file_path} in {index_request.project_path}", flush=True)

    async def stop_workers(self):
        """Stop background workers"""
        self._workers_running = False
        for worker in self._worker_tasks:
            worker.cancel()
            try:
                await worker
            except asyncio.CancelledError:
                pass
