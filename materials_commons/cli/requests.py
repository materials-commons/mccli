from dataclasses import dataclass
from pathlib import Path
from typing import Optional, Literal

from materials_commons.cli.local_project import LocalProject
from materials_commons.cli.models import Observation, FileRecord
import materials_commons.api.models as mcmodel


@dataclass(frozen=True)
class UploadRequest:
    """Request to upload a file to Materials Commons"""
    observation: Observation
    updated_record: FileRecord
    project: LocalProject


@dataclass(frozen=True)
class IndexRequest:
    """Request to index a file"""
    file_path: str | Path
    project_path: str | Path
    remote_entry: Optional[mcmodel.File]
    project: LocalProject


DBWriteRequestType = Literal["single", "multi"]

@dataclass(frozen=True)
class DBWriteRequest:
    project: LocalProject
    command: DBWriteRequestType
    data: FileRecord | list[FileRecord]
