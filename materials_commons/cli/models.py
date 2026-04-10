import os
from dataclasses import dataclass
from datetime import timezone
from pathlib import Path
from typing import Literal, Optional, Protocol

from materials_commons.api import client as mcapi
from materials_commons.api import models


@dataclass(frozen=True)
class EntryStatInfo:
    size: int
    mtime_ns: int
    ctime_ns: int


class DirEntryInfo(Protocol):
    @property
    def path(self) -> Path: ...

    @property
    def name(self) -> str: ...

    @property
    def is_dir(self) -> bool: ...

    @property
    def is_file(self) -> bool: ...

    @property
    def is_symlink(self) -> bool: ...

    def stat(self) -> EntryStatInfo: ...


@dataclass
class LocalDirEntryInfo:
    entry: os.DirEntry

    @property
    def path(self) -> Path:
        return Path(self.entry.path)

    @property
    def name(self) -> str:
        return self.entry.name

    @property
    def is_dir(self) -> bool:
        return self.entry.is_dir(follow_symlinks=False)

    @property
    def is_file(self) -> bool:
        return self.entry.is_file(follow_symlinks=False)

    @property
    def is_symlink(self) -> bool:
        return self.entry.is_symlink()

    def stat(self) -> EntryStatInfo:
        sinfo = self.entry.stat()
        return EntryStatInfo(
            size=sinfo.st_size,
            mtime_ns=sinfo.st_mtime_ns,
            ctime_ns=sinfo.st_ctime_ns,
        )


@dataclass
class RemoteDirEntryInfo:
    remote_file: models.File

    @property
    def path(self) -> Path:
        return Path(self.remote_file.directory.path) / self.remote_file.name

    @property
    def name(self) -> str:
        return self.remote_file.name

    @property
    def is_dir(self) -> bool:
        return self.remote_file.mime_type == "directory"

    @property
    def is_file(self) -> bool:
        return self.remote_file.mime_type != "directory"

    @property
    def is_symlink(self) -> bool:
        return False

    def stat(self) -> EntryStatInfo:
        return EntryStatInfo(
            size=self.remote_file.size,
            ctime_ns=int(self.remote_file.created_at.replace(tzinfo=timezone.utc).timestamp() * 1_000_000_000),
            mtime_ns=int(self.remote_file.updated_at.replace(tzinfo=timezone.utc).timestamp() * 1_000_000_000),
        )


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
    # local_observed: Optional[LocalObserved]
    # local_entry: Optional[DirEntryInfo]
    local_entry: Optional[LocalObserved]
    remote_entry: Optional[models.File]
    file_record: Optional[FileRecord]
    file_decision: Optional[FileDecision]
    exception: Optional[Exception] = None


class LocalProject(models.Project):
    def __init__(self, remote: Optional[mcapi.Client] = None, local_path: Optional[str] = None, data={}):
        super().__init__(data)
        self.remote = remote
        self.local_path = local_path


@dataclass
class LSEntry:
    name: str
    l_updated_at: Optional[float] = None
    l_size: Optional[int] = None
    l_type: Optional[str] = None
    l_id: Optional[int] = None
    r_updated_at: Optional[int] = None
    r_size: Optional[int] = None
    r_type: Optional[str] = None
    r_id: Optional[int] = None
    eq: Optional[str] = None

    @classmethod
    def from_file_entry(cls, entry: FileEntry) -> 'LSEntry':
        if entry.local_entry and entry.remote_entry:
            return cls.local_and_remote_entry(entry)
        elif entry.local_entry:
            return cls.local_only_entry(entry)
        elif entry.remote_entry:
            return cls.remote_only_entry(entry)
        else:
            raise ValueError("FileEntry must have either local or remote entry")

    @classmethod
    def local_and_remote_entry(cls, entry: FileEntry) -> 'LSEntry':
        checksums_equal = entry.file_decision.updated_record.local_checksum == entry.remote_entry.checksum
        mtime = entry.file_decision.updated_record.local_mtime_ns / 1_000_000_000
        return cls(
            name=entry.local_entry.name,
            l_updated_at=mtime,
            l_size=entry.file_decision.updated_record.local_size,
            l_type="D" if entry.local_entry.is_dir else "F",
            l_id=entry.file_decision.updated_record.remote_file_id,
            r_updated_at=entry.remote_entry.updated_at,
            r_size=entry.remote_entry.size,
            r_type="D" if entry.remote_entry.mime_type == "directory" else "F",
            r_id=entry.remote_entry.id,
            eq="eq" if checksums_equal else None
        )

    @classmethod
    def local_only_entry(cls, entry: FileEntry) -> 'LSEntry':
        local_id: Optional[int] = None
        if entry.file_decision.updated_record.remote_file_id is not None:
            local_id = entry.file_decision.updated_record.remote_file_id
        mtime = entry.file_decision.updated_record.local_mtime_ns / 1_000_000_000
        return cls(
            name=entry.local_entry.name,
            l_updated_at=mtime,
            l_size=entry.file_decision.updated_record.local_size,
            l_type="D" if entry.local_entry.is_dir else "F",
            l_id=local_id,
            r_updated_at=None,
            r_size=None,
            r_type=None,
            r_id=None,
            eq=None
        )

    @classmethod
    def remote_only_entry(cls, entry: FileEntry) -> 'LSEntry':
        r_type = "D" if entry.remote_entry.mime_type == "directory" else "F"
        return cls(
            name=entry.remote_entry.name,
            l_updated_at=None,
            l_size=None,
            l_type=None,
            l_id=None,
            r_updated_at=entry.remote_entry.updated_at,
            r_size=entry.remote_entry.size,
            r_type=r_type,
            r_id=entry.remote_entry.id,
            eq=None
        )


@dataclass
class LSAction:
    name: str
    local_remote: str
    action: str
    reason: str
    l_type: str
    r_type: str

    @classmethod
    def from_file_entry(cls, entry: FileEntry) -> 'LSAction':
        action = entry.file_decision.action
        if action == "db_update":
            action = "preserve"
        reason = entry.file_decision.reason

        if entry.local_entry and entry.remote_entry:
            l_type = "D" if entry.local_entry.is_dir else "F"
            r_type = "D" if entry.remote_entry.mime_type == "directory" else "F"
            if r_type == "D" and l_type == "D":
                reason = "local and remote directories exist"
                action = "skip"
            return cls(
                name=entry.local_entry.name,
                local_remote='L/R',
                action=action,
                reason=reason,
                l_type=l_type,
                r_type=r_type,
            )
        elif entry.local_entry:
            l_type = "D" if entry.local_entry.is_dir else "F"
            r_type = "-"
            if l_type == "F":
                action = "upload"
            return cls(
                name=entry.local_entry.name,
                local_remote='L',
                action=action,
                reason=reason,
                l_type=l_type,
                r_type=r_type,
            )
        else:
            l_type = "-"
            r_type = "D" if entry.remote_entry.mime_type == "directory" else "F"
            return cls(
                name=entry.remote_entry.name,
                local_remote='R',
                action=action,
                reason=reason,
                l_type=l_type,
                r_type=r_type,
            )
