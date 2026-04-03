import asyncio
import os
from _asyncio import Task
from typing import Optional

from materials_commons.api import models

from materials_commons.cli.models import FileRecord
from materials_commons.cli.reconcile2 import observe_and_reconcile
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

    def __init__(self, project_file_dbs: ProjectFileDBs, db_queue: asyncio.Queue, index_queue: asyncio.Queue,
                 max_concurrent: int = 3):
        self._project_file_dbs = project_file_dbs
        self._db_queue = db_queue
        self._index_queue = index_queue
        self._workers_running = False
        self._max_concurrent = max_concurrent

    async def start_workers(self) -> list[Task[None]]:
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
                (file_path, project_path, remote_entry, project_id) = await asyncio.wait_for(self._index_queue.get(),
                                                                                             timeout=1.0)
            except asyncio.TimeoutError:
                continue

            try:
                await self._index(file_path, project_path, remote_entry, project_id)
            except Exception as e:
                print(f"Error indexing {file_path}: {e}")
                raise e
            finally:
                self._index_queue.task_done()

    async def _index(self, file_path: str, project_path: str, remote_entry: Optional[models.File], project_id: int):
        """Index a file. Checks if the file has changed since the last index."""
        db = await self._project_file_dbs.get_filedb(project_id)
        if db is None:
            raise Exception(
                f"Project {project_id} not found in filedb."
            )
        file_decision = await observe_and_reconcile(db=db,
                                                    project_path=project_path,
                                                    file_path=file_path,
                                                    remote_entry=remote_entry,
                                                    recompute_checksum=True)

        if not file_decision.updated_record:
            print(f"Skipping {file_path} in {project_path} as it hasn't changed")
            return

        await self._db_queue.put(("single", project_id, file_decision.updated_record))
        print(f"Indexed {file_path} in {project_path}")

    def stop_workers(self):
        """Stop background workers"""
        self._workers_running = False
