import json
from dataclasses import dataclass
from functools import cached_property
from pathlib import Path
from typing import Optional, Any

from materials_commons.api.client import Client

from materials_commons.cli.config import Config
from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.old.exceptions import MCCLIException
from materials_commons.cli.old.functions import project_path


@dataclass
class LocalProject:
    id: int
    name: str
    local_path: Path
    client: Client
    filedb: Optional[FileIndexDB] = None

    @property
    def remote(self):
        # Old code referenced the client as the 'remote' property. This is
        # deprecated and should be removed. However, it is still used in
        # some places. So this property is here to support that.
        return self.client

    @cached_property
    def db_path(self) -> Path:
        return self.local_path / ".mc" / "mc2.sqlite"

    async def get_filedb(self) -> "FileIndexDB":
        if self.filedb is None:
            self.filedb = await FileIndexDB.create(self.db_path)
        return self.filedb

    @classmethod
    def load(cls, path: str | Path, *, client: Optional[Client] = None) -> "LocalProject":
        local_root = project_path(str(path))
        if not local_root:
            raise MCCLIException(f"No Materials Commons project found at {path}")

        config_path = Path(local_root) / ".mc" / "config.json"

        with open(config_path, "r") as f:
            data = json.load(f)

        project_id = int(data["project_id"])
        # Hack for now until we can store name
        if "name" not in data:
            project_name = Path(path).name
        else:
            project_name = data["name"]

        if client is None:
            config = Config.load()
            client = config.default_remote.make_client()

        return cls(
            id=project_id,
            name=project_name,
            local_path=Path(local_root),
            client=client
        )

    def to_remote_path(self, path: Path | str) -> Path:
        """
            Converts a local project path to its corresponding remote path. For example,
            if the proj_base is "/home/user/myproject" and the full_path is "/home/user/myproject/dir/data.txt",
            the function will return "/dir/data.txt".

            Args:
                path (Path): The full local path to be converted.

            Returns:
                Path: The remote path corresponding to the local path.
            """
        full_path = Path(path).resolve()
        # remote_path will be the relative path from proj_base to full_path, i.e., it
        # won't start with a slash, so we need to add one to get the correct path.
        if full_path.as_posix() == self.local_path.as_posix():
            return Path("/")
        remote_path = full_path.relative_to(self.local_path)
        return Path("/" + remote_path.as_posix())


