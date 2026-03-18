import asyncio

from materials_commons.cli.server.project_filedbs import ProjectFileDBs


class DBManager:
    """
    Manages writes to the project databases by serializing them through a single task. The
    DB Manager maintains a has of project databases. Communication to the DBManager
    is managed through a queue that its task pulls from and writes records from the queue
    to the project database.
    """

    def __init__(self, db_queue: asyncio.Queue, project_dbs: ProjectFileDBs):
        self._db_queue = db_queue
        self._project_dbs = project_dbs
        self._workers_running = False

    async def start_workers(self):
        self._workers_running = True
        workers = [asyncio.create_task(self._db_queue_worker())]
        return workers

    async def _db_queue_worker(self):
        while self._workers_running:
            try:
                (command, project_id, data) = await asyncio.wait_for(self._db_queue.get(), timeout=1.0)
            except asyncio.TimeoutError:
                continue

            project_db = self._project_dbs.get_filedb(project_id)
            if project_db is None:
                # Log an error here
                continue

            # Switch to aiosqlite so we can do await on the upserts
            try:
                if command == 'single':
                    await asyncio.to_thread(project_db.upsert, data)
                elif command == 'multi':
                    await asyncio.to_thread(project_db.upsert_many, data)
                else:
                    print(f"Command {command} not recognized")
            except Exception as e:
                print(f"Error updating database: {e}")
            finally:
                self._db_queue.task_done()

    async def stop_workers(self):
        self._workers_running = False