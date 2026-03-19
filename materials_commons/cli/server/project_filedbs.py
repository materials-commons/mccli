from pathlib import Path

from materials_commons.cli.functions import project_path
from materials_commons.cli.server import projects

from materials_commons.cli.filedb import FileIndexDB

class ProjectFileDBs:
    def __init__(self):
        self._filedbs_for_project: dict[int, FileIndexDB] = {}

    async def get_filedb(self, project_id: int) -> FileIndexDB:
        if project_id not in self._filedbs_for_project:
            p = projects.get_local_project_by_id(str(project_id))
            if not p:
                # We are not running in server mode, so look for the project dir a different way.
                cwd = Path.cwd()
                proj_path = project_path(cwd)
                self._filedbs_for_project[project_id] = await FileIndexDB.create(Path(proj_path) / ".mc" / "mc2.sqlite")
            else:
                self._filedbs_for_project[project_id] = await FileIndexDB.create(Path(p["project_dir_path"]) / ".mc" / "mc2.sqlite")

        return self._filedbs_for_project.get(project_id)

    async def close_dbs(self):
        for db in self._filedbs_for_project.values():
            await db.close()