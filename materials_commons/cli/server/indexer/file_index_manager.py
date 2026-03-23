import asyncio
import os

from datetime import datetime
from materials_commons.cli.models import FileRecord
from materials_commons.cli.functions import checksum
from materials_commons.cli.server.project_filedbs import ProjectFileDBs


def file_has_changed(file_record: FileRecord, finfo: os.stat_result) -> bool:
    """Check if a file has changed since the last index. Returns True if file_record is None."""
    if file_record is None:
        return True

    return (
            file_record.local_size != finfo.st_size or
            file_record.local_mtime_ns != finfo.st_mtime_ns
    )


class FileIndexManager:
    """Manages multiple concurrent file indexers"""

    def __init__(self, project_file_dbs: ProjectFileDBs, db_queue: asyncio.Queue, index_queue: asyncio.Queue, max_concurrent: int = 3):
        self._project_file_dbs = project_file_dbs
        self._db_queue = db_queue
        self._index_queue = index_queue
        self._workers_running = False
        self._max_concurrent = max_concurrent

    async def start_workers(self):
        """Start background workers to process the index queue"""
        self._workers_running = True
        workers = [
            asyncio.create_task(self._index_file_worker())
            for i in range(self._max_concurrent)
        ]
        return workers

    async def _index_file_worker(self):
        """Worker that processes index requests from queue"""
        while self._workers_running:
            try:
                (file_path, project_path, project_id) = await asyncio.wait_for(self._index_queue.get(), timeout=1.0)
            except asyncio.TimeoutError:
                continue

            try:
                await self._index(file_path, project_path, project_id)
            except Exception as e:
                print(f"Error indexing {file_path}: {e}")
                raise e
            finally:
                self._index_queue.task_done()

    async def _index(self, file_path: str, project_path: str, project_id: int):
        """Index a file. Checks if the file has changed since the last index."""
        finfo = await asyncio.to_thread(os.stat, file_path)
        db = await self._project_file_dbs.get_filedb(project_id)

        if db is None:
            raise Exception(
                f"Project {project_id} not found in filedb."
            )
        file_record = await db.get_file_by_path(project_path)

        if not file_has_changed(file_record, finfo):
            print(f"Skipping {file_path} in {project_path} as it hasn't changed")
            return

        print(f"Indexing {file_path} in {project_path}")
        # Update or create the file record
        csum = await asyncio.to_thread(checksum, file_path)
        file_name = os.path.basename(project_path)
        dir = os.path.dirname(project_path)
        file_record = FileRecord(
            path=project_path,
            name=file_name,
            dir=dir,
            is_clean_local_copy=0,
            local_size=finfo.st_size,
            local_mtime_ns=finfo.st_mtime_ns,
            local_ctime_ns=finfo.st_ctime_ns,
            local_last_seen_ts=int(datetime.now().timestamp()),
            local_checksum=csum,
            status="indexed",
        )
        await self._db_queue.put(("single", project_id, file_record))
        # print(f"Done indexing {file_path} in {project_path} got Hash: {csum}")

    def stop_workers(self):
        """Stop background workers"""
        self._workers_running = False
