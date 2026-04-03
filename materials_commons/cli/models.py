from dataclasses import dataclass
from typing import Literal, Optional

from materials_commons.api import models
from materials_commons.api import client as mcapi

from materials_commons.cli.walk import DirEntryInfo


@dataclass(frozen=True)
class FileRecord:
    path: str
    name: str
    is_clean_local_copy: int
    dir: Optional[str] = None

    local_size: Optional[int] = None
    local_mtime_ns: Optional[int] = None
    local_ctime_ns: Optional[int] = None
    local_last_seen_ts: Optional[int] = None
    local_checksum: Optional[str] = None

    remote_file_id: Optional[int] = None
    remote_size: Optional[int] = None
    remote_ctime_ns: Optional[int] = None
    remote_checksum: Optional[str] = None
    remote_last_seen_ts: Optional[int] = None

    status: Optional[str] = None
    origin: Optional[str] = None
    transfer_id: Optional[str] = None


@dataclass
class RemoteFileEntry:
    path: str
    dir: str
    name: str
    remote_file_id: Optional[int] = None
    remote_size: Optional[int] = None
    remote_ctime_ns: Optional[int] = None
    remote_checksum: Optional[str] = None


@dataclass
class RemoteDirectory:
    directory_path: str
    files: list[RemoteFileEntry]
    subdirs: list[str]


@dataclass
class LocalObserved:
    path: str
    dir: str
    name: str
    exists: bool
    project_path: str
    local_size: Optional[int]
    local_mtime_ns: Optional[int]
    local_ctime_ns: Optional[int]
    local_checksum: Optional[str] = None
    checksum_outdated: bool = False
    is_dir: bool = False
    is_symlink: bool = False
    is_file: bool = False


@dataclass
class FileDecision:
    action: Literal["skip", "download", "conflict", "adopt_clean", "db_update_only"]
    reason: str
    updated_record: FileRecord


@dataclass
class DirectoryDecision:
    to_download: list[FileDecision]
    to_skip: list[FileDecision]
    to_conflict: list[FileDecision]
    to_db_update_only: list[FileDecision]


Action = Literal["skip", "download", "conflict", "adopt", "db_update"]


@dataclass(frozen=True)
class FileDecision:
    action: Action
    reason: str
    updated: bool
    updated_record: FileRecord


@dataclass
class FileEntry:
    # name: str
    # local_path: Path
    local_entry: Optional[DirEntryInfo]
    remote_entry: Optional[models.File]
    file_record: Optional[FileRecord]
    file_decision: Optional[FileDecision]
    exception: Optional[Exception] = None


class LocalProject(models.Project):
    def __init__(self, remote: Optional[mcapi.Client] = None, local_path: Optional[str] = None, data={}):
        super().__init__(data)
        self.remote = remote
        self.local_path = local_path
