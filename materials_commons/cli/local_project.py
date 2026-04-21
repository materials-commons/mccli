import json
from dataclasses import dataclass
from functools import cached_property
from pathlib import Path
from typing import Optional

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
