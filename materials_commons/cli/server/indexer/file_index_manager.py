import asyncio
import os

from datetime import datetime
from materials_commons.cli.filedb import FileIndexDB, FileRecord
from materials_commons.cli.functions import checksum


def file_has_changed(file_record: FileRecord, finfo: os.stat_result) -> bool:
    """Check if a file has changed since the last index. Returns True if file_record is None."""
    if file_record is None:
        return True

    return (
            file_record.size != finfo.st_size or
            file_record.mtime_ns != finfo.st_mtime_ns
    )


class FileIndexManager:
    """Manages multiple concurrent file indexers"""

    def __init__(self, db: FileIndexDB, queue: asyncio.Queue, max_concurrent: int = 3):
        self._db = db
        self._index_queue = queue
        self._workers_running = False
        self._max_concurrent = max_concurrent

        # TODO: db_write_queue should be passed in so it can be shared by other parts of the system that need to write to the db.
        self._db_write_queue = asyncio.Queue()

    async def start_workers(self):
        """Start background workers to process the index queue"""
        self._workers_running = True
        workers = [
            asyncio.create_task(self._index_file_worker())
            for i in range(self._max_concurrent)
        ]
        db_queue_worker = asyncio.create_task(self._db_queue_worker())
        workers.append(db_queue_worker)
        return workers

    async def _index_file_worker(self):
        """Worker that processes index requests from queue"""
        while self._workers_running:
            try:
                (file_path, project_path) = await asyncio.wait_for(self._index_queue.get(), timeout=1.0)
            except asyncio.TimeoutError:
                continue

            try:
                await self._index(file_path, project_path)
            except Exception as e:
                print(f"Error indexing {file_path}: {e}")
            finally:
                self._index_queue.task_done()

    async def _index(self, file_path: str, project_path: str):
        """Index a file. Checks if the file has changed since the last index."""
        finfo = await asyncio.to_thread(os.stat, file_path)
        file_record = await asyncio.to_thread(self._db.get_file_by_path, project_path)

        if not file_has_changed(file_record, finfo):
            print(f"Skipping {file_path} in {project_path} as it hasn't changed")
            return

        print(f"Indexing {file_path} in {project_path}")
        # Update or create the file record
        csum = await asyncio.to_thread(checksum, file_path)
        file_record = FileRecord(
            path=project_path,
            size=finfo.st_size,
            mtime_ns=finfo.st_mtime_ns,
            ctime_ns=finfo.st_ctime_ns,
            last_seen_ts=int(datetime.now().timestamp()),
            checksum=csum,
            status="indexed",
        )
        await self._db_write_queue.put(file_record)
        # print(f"Done indexing {file_path} in {project_path} got Hash: {csum}")

    async def _db_queue_worker(self):
        """Worker that processes db writes from queue"""
        while self._workers_running:
            try:
                db_entry = await asyncio.wait_for(self._db_write_queue.get(), timeout=1.0)
                # print(f"Writing to db: {db_entry}")
            except asyncio.TimeoutError:
                continue
            except asyncio.CancelledError:
                return

            try:
                await asyncio.to_thread(self._db.upsert, db_entry)
            except Exception as e:
                print(f"Error writing to db: {e}")
            finally:
                self._db_write_queue.task_done()

    async def stop_workers(self):
        """Stop background workers"""
        self._workers_running = False
