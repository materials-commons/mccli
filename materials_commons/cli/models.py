import os
from dataclasses import dataclass
from datetime import timezone
from pathlib import Path
from typing import Literal, Optional

from materials_commons.api import client as mcapi
from materials_commons.api import models


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


EntryKind = Literal["file", "dir"]


@dataclass
class RemoteFileEntry:
    raw: models.File
    path: Path
    name: str
    kind: Optional[EntryKind]
    size: Optional[int]
    mtime_ns: Optional[int]
    ctime_ns: Optional[int]
    remote_file_id: Optional[int] = None
    checksum: Optional[str] = None

    @property
    def is_dir(self) -> bool:
        return self.kind == "dir"

    @property
    def is_file(self) -> bool:
        return self.kind == "file"


@dataclass
class LocalFileEntry:
    raw: Path
    path: Path
    name: str
    kind: Optional[EntryKind]
    is_symlink: bool
    size: Optional[int]
    mtime_ns: Optional[int]
    ctime_ns: Optional[int]

    @property
    def is_dir(self) -> bool:
        return self.kind == "dir"

    @property
    def is_file(self) -> bool:
        return self.kind == "file"


@dataclass
class WalkObservation:
    local_path: Optional[Path] = None
    remote_path: Optional[Path] = None

    file_record: Optional[FileRecord] = None
    local_entry: Optional[LocalFileEntry] = None
    remote_entry: Optional[RemoteFileEntry] = None

    @property
    def name(self) -> str:
        if self.local_entry:
            return self.local_entry.name
        if self.remote_entry:
            return self.remote_entry.name
        if self.file_record:
            return self.file_record.name
        raise ValueError("No name source")

    @property
    def path(self) -> Path:
        if self.local_entry:
            return self.local_entry.path
        if self.remote_entry:
            return self.remote_entry.path
        if self.file_record:
            return Path(self.file_record.path)
        raise ValueError("No path source")

    def has_local(self) -> bool:
        return self.local_entry is not None

    def has_remote(self) -> bool:
        return self.remote_entry is not None

    def has_record(self) -> bool:
        return self.file_record is not None

    def local_is_symlink(self) -> bool:
        return self.local_entry is not None and self.local_entry.is_symlink

    @property
    def is_dir(self) -> bool:
        if self.local_entry is not None and self.local_entry.kind == "dir":
            return True
        if self.remote_entry is not None and self.remote_entry.kind == "dir":
            return True
        return False

    def is_file(self) -> bool:
        if self.local_entry is not None and self.local_entry.kind == "file":
            return True
        if self.remote_entry is not None and self.remote_entry.kind == "file":
            return True
        return False

    def local_kind(self) -> Optional[EntryKind]:
        if self.local_entry:
            return self.local_entry.kind
        else:
            return None

    def remote_kind(self) -> Optional[EntryKind]:
        if self.remote_entry:
            return self.remote_entry.kind
        else:
            return None

    def kinds_match(self) -> bool:
        if self.local_kind() is None or self.remote_kind() is None:
            return False
        if self.local_entry.kind is None:
            return False
        return self.local_entry.kind == self.remote_entry.kind

    def has_kind_conflict(self) -> bool:
        return self.has_local() and self.has_remote() and not self.kinds_match()

    def local_entry_matches_record(self, include_checksum: bool = False) -> bool:
        if self.local_entry is None or self.file_record is None:
            return False

        matches = (
                self.local_entry.size == self.file_record.local_size
                and self.local_entry.mtime_ns == self.file_record.local_mtime_ns
                and self.local_entry.ctime_ns == self.file_record.local_ctime_ns
        )

        if include_checksum:
            matches = matches and (
                    self.file_record.local_checksum is not None
            )

        return matches

    def remote_entry_matches_record(self, include_checksum: bool = False) -> bool:
        if self.remote_entry is None or self.file_record is None:
            return False

        matches = (
                self.remote_entry.size == self.file_record.remote_size
                and self.remote_entry.ctime_ns == self.file_record.remote_ctime_ns
        )

        if include_checksum:
            matches = matches and (
                    self.remote_entry.checksum is not None
                    and self.remote_entry.checksum == self.file_record.remote_checksum
            )

        return matches

    def local_is_stale(self, include_checksum: bool = False) -> bool:
        return self.has_record() and self.has_local() and not self.local_entry_matches_record(include_checksum)

    def remote_is_stale(self, include_checksum: bool = False) -> bool:
        return self.has_record() and self.has_remote() and not self.remote_entry_matches_record(include_checksum)

    def record_is_stale(self, include_checksum: bool = False) -> bool:
        return self.local_is_stale(include_checksum) or self.remote_is_stale(include_checksum)


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
class FileState:
    observation: WalkObservation
    file_decision: Optional[FileDecision] = None
    exception: Optional[Exception] = None


@dataclass(frozen=True)
class DirectorySnapshot:
    path: Path
    entries: dict[str, FileState]


class OldLocalProject(models.Project):
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
    r_updated_at: Optional[float] = None
    r_size: Optional[int] = None
    r_type: Optional[str] = None
    r_id: Optional[int] = None
    eq: Optional[str] = None

    @classmethod
    def from_file_state(cls, state: FileState) -> 'LSEntry':
        if state.observation.local_entry and state.observation.remote_entry:
            return cls.local_and_remote_entry(state)
        elif state.observation.local_entry:
            return cls.local_only_state(state)
        elif state.observation.remote_entry:
            return cls.remote_only_state(state)
        else:
            raise ValueError("FileEntry must have either local or remote entry")

    @classmethod
    def local_and_remote_entry(cls, state: FileState) -> 'LSEntry':
        checksums_equal = state.file_decision.updated_record.local_checksum == state.observation.remote_entry.checksum
        l_mtime = state.file_decision.updated_record.local_mtime_ns / 1_000_000_000
        r_mtime = state.observation.remote_entry.mtime_ns / 1_000_000_000
        return cls(
            name=state.observation.local_entry.name,
            l_updated_at=l_mtime,
            l_size=state.file_decision.updated_record.local_size,
            l_type="D" if state.observation.local_entry.is_dir else "F",
            l_id=state.file_decision.updated_record.remote_file_id,
            r_updated_at=r_mtime,
            r_size=state.observation.remote_entry.size,
            r_type="D" if state.observation.remote_entry.is_dir else "F",
            r_id=state.observation.remote_entry.raw.id,
            eq="eq" if checksums_equal else None
        )

    @classmethod
    def local_only_state(cls, state: FileState) -> 'LSEntry':
        local_id: Optional[int] = None
        if state.file_decision.updated_record.remote_file_id is not None:
            local_id = state.file_decision.updated_record.remote_file_id
        mtime = state.file_decision.updated_record.local_mtime_ns / 1_000_000_000
        return cls(
            name=state.observation.local_entry.name,
            l_updated_at=mtime,
            l_size=state.file_decision.updated_record.local_size,
            l_type="D" if state.observation.local_entry.is_dir else "F",
            l_id=local_id,
            r_updated_at=None,
            r_size=None,
            r_type=None,
            r_id=None,
            eq=None
        )

    @classmethod
    def remote_only_state(cls, state: FileState) -> 'LSEntry':
        r_type = "D" if state.observation.remote_entry.is_dir else "F"
        r_mtime = state.observation.remote_entry.mtime_ns / 1_000_000_000
        return cls(
            name=state.observation.remote_entry.name,
            l_updated_at=None,
            l_size=None,
            l_type=None,
            l_id=None,
            r_updated_at=r_mtime,
            r_size=state.observation.remote_entry.size,
            r_type=r_type,
            r_id=state.observation.remote_entry.raw.id,
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
    def from_file_state(cls, state: FileState) -> 'LSAction':
        action = state.file_decision.action
        if action == "db_update":
            action = "preserve"
        reason = state.file_decision.reason

        if state.observation.local_entry and state.observation.remote_entry:
            # Both local and remote exist
            l_type = "D" if state.observation.local_entry.is_dir else "F"
            r_type = "D" if state.observation.remote_entry.is_dir else "F"
            if r_type == "D" and l_type == "D":
                reason = "local and remote directories exist"
                action = "skip"
            return cls(
                name=state.observation.local_entry.name,
                local_remote='L/R',
                action=action,
                reason=reason,
                l_type=l_type,
                r_type=r_type,
            )
        elif state.observation.local_entry:
            # Only local exists
            l_type = "D" if state.observation.local_entry.is_dir else "F"
            r_type = "-"
            if l_type == "F":
                action = "upload"
            return cls(
                name=state.observation.local_entry.name,
                local_remote='L',
                action=action,
                reason=reason,
                l_type=l_type,
                r_type=r_type,
            )
        else:
            # Only remote exists
            l_type = "-"
            r_type = "D" if state.observation.remote_entry.is_dir else "F"
            return cls(
                name=state.observation.remote_entry.name,
                local_remote='R',
                action=action,
                reason=reason,
                l_type=l_type,
                r_type=r_type,
            )
