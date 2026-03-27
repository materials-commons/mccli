import asyncio
from pathlib import Path

from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.functions import project_path
from materials_commons.cli.server import projects


class ProjectFileDBs:
    def __init__(self):
        self._filedbs_for_project: dict[int, FileIndexDB] = {}
        self._locks_for_project: dict[int, asyncio.Lock] = {}
        self._locks_guard = asyncio.Lock()

    async def _get_lock(self, project_id: int) -> asyncio.Lock:
        async with self._locks_guard:
            lock = self._locks_for_project.get(project_id)
            if lock is None:
                lock = asyncio.Lock()
                self._locks_for_project[project_id] = lock
            return lock

    async def get_filedb(self, project_id: int) -> FileIndexDB:
        lock = await self._get_lock(project_id)
        async with lock:
            db = self._filedbs_for_project.get(project_id)
            if db is not None:
                return db

            p = projects.get_local_project_by_id(str(project_id))
            if not p:
                cwd = Path.cwd()
                proj_path = await asyncio.to_thread(project_path, cwd)
                db_path = Path(proj_path) / ".mc" / "mc2.sqlite"
            else:
                db_path = Path(p["project_dir_path"]) / ".mc" / "mc2.sqlite"

            print(f"Creating new filedb instance for {project_id}")
            db = await FileIndexDB.create(db_path)
            self._filedbs_for_project[project_id] = db
            return db

    async def close_dbs(self):
        async with self._locks_guard:
            dbs = list(self._filedbs_for_project.values())
            self._filedbs_for_project.clear()
            self._locks_for_project.clear()

        for db in dbs:
            await db.close()
