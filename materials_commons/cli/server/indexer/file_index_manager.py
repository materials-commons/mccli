import asyncio
import os

from datetime import datetime
from materials_commons.cli.filedb import FileIndexDB, FileRecord
from materials_commons.cli.functions import checksum


class FileIndexManager:
    """Manages multiple concurrent file indexers"""

    def __init__(self, db: FileIndexDB, queue: asyncio.Queue, max_concurrent: int = 3):
        self.db = db
        self._index_queue = queue
        self._workers_running = False
        self._max_concurrent = max_concurrent
        self.db_write_queue = asyncio.Queue()

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
        print("Index worker started")
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
        """Index a file"""
        print(f"Indexing {file_path} in {project_path}")
        hash = await asyncio.to_thread(checksum, file_path)
        finfo = os.stat(file_path)
        file_record = FileRecord(
            path = file_path,
            size = finfo.st_size,
            mtime_ns = finfo.st_mtime_ns,
            ctime_ns = finfo.st_ctime_ns,
            last_seen_ts = int(datetime.now().timestamp()),
            checksum = hash,
            status = "indexed",
        )
        await self.db_write_queue.put(file_record)
        print(f"Done indexing {file_path} in {project_path} got Hash: {hash}")
        pass

    async def _db_queue_worker(self):
        """Worker that processes db writes from queue"""
        while self._workers_running:
            try:
                db_entry = await asyncio.wait_for(self.db_write_queue.get(), timeout=1.0)
                print(f"Writing to db: {db_entry}")
            except asyncio.TimeoutError:
                continue
            except asyncio.CancelledError:
                return

            try:
                self.db.upsert(db_entry)
            except Exception as e:
                pass
            finally:
                self.db_write_queue.task_done()

    async def stop_workers(self):
        """Stop background workers"""
        self._workers_running = False