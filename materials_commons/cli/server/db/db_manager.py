import asyncio
from dataclasses import dataclass
from typing import cast

from materials_commons.cli.models import FileRecord

from materials_commons.cli.local_project import LocalProject

@dataclass(frozen=True)
class DBWriteRequest:
    project: LocalProject
    command: str
    data: FileRecord | list[FileRecord]

class DBManager:
    """
    Manages writes to the project databases by serializing them through a single task. Communication to the DBManager
    is managed through a queue that its task pulls from and writes records from the queue to the project database.
    """

    def __init__(self, db_queue: asyncio.Queue[DBWriteRequest]):
        self._db_queue: asyncio.Queue[DBWriteRequest] = db_queue
        self._workers_running = False
        self._worker_tasks: list[asyncio.Task] = []

    async def start_workers(self) -> None:
        self._workers_running = True
        self._worker_tasks = [asyncio.create_task(self._db_queue_worker())]

    async def _db_queue_worker(self):
        while self._workers_running:
            try:
                write_request: DBWriteRequest = await asyncio.wait_for(self._db_queue.get(), timeout=1.0)
            except asyncio.TimeoutError:
                continue

            try:
                db = await write_request.project.get_filedb()
                if write_request.command == 'single':
                    record = cast(FileRecord, write_request.data)
                    async with db.transaction():
                        await db.upsert(record)
                elif write_request.command == 'multi':
                    records = cast(list[FileRecord], write_request.data)
                    async with db.transaction():
                        await db.upsert_many(records)
                else:
                    print(f"Command {write_request.command} not recognized")
            except Exception as e:
                print(f"Error updating database: {e}")
            finally:
                self._db_queue.task_done()

    async def stop_workers(self):
        self._workers_running = False
        for worker in self._worker_tasks:
            worker.cancel()
            try:
                await worker
            except asyncio.CancelledError:
                pass