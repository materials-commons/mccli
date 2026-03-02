import asyncio

from materials_commons.cli.filedb import FileIndexDB


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
                pass

    async def _index(self, file_path: str, project_path: str):
        """Index a file"""
        print(f"Indexing {file_path} in {project_path}")
        pass

    async def _db_queue_worker(self):
        """Worker that processes db writes from queue"""
        while self._workers_running:
            try:
                db_entry = await self.db_write_queue.get()
            except asyncio.CancelledError:
                return

            try:
                self.db.upsert(db_entry)
            except Exception as e:
                pass

    async def stop_workers(self):
        """Stop background workers"""
        self._workers_running = False