import hashlib
import os
import sqlite3
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Optional, Tuple

@dataclass(frozen=True)
class FileRecord:
    path: str
    size: int
    mtime_ns: int
    ctime_ns: int
    last_seen_ts: int
    checksum: Optional[str] = None
    status: Optional[str] = None
    remote_file_id: Optional[int] = None
    transfer_id: Optional[str] = None


class FileIndexDB:
    def __init__(self, db_path: Path):
        self.db_path = db_path
        self._init_db()

    def _init_db(self):
        with self._connect() as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS files (
                    path TEXT PRIMARY KEY,
                    size INTEGER NOT NULL,
                    mtime_ns INTEGER NOT NULL,
                    ctime_ns INTEGER NOT NULL,
                    last_seen_ts INTEGER NOT NULL,
                    checksum TEXT,
                    status TEXT,
                    remote_file_id INTEGER,
                    transfer_id TEXT
                ) STRICT;
                """
            )
            conn.execute("CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);")
            conn.execute("CREATE INDEX IF NOT EXISTS idx_files_last_seen ON files(last_seen_ts);")


    def _connect(self):
        conn = sqlite3.connect(self.db_path)
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA journal_mode=WAL;")
        conn.execute("PRAGMA busy_timeout=5000;")
        conn.execute("PRAGMA synchronous=NORMAL;")
        conn.execute("PRAGMA cache_size=100000;")
        conn.execute("PRAGMA temp_store=MEMORY;")
        conn.execute("PRAGMA locking_mode=EXCLUSIVE;")
        conn.execute("PRAGMA foreign_keys=ON;")
        return conn

    def upsert(self, record: FileRecord):
        with self._connect() as conn:
            conn.execute(
                """
                INSERT INTO files(path, size, mtime_ns, ctime_ns, last_seen_ts, checksum, status, remote_file_id, transfer_id) 
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(path) DO UPDATE SET
                    size = excluded.size,
                    mtime_ns = excluded.mtime_ns,
                    ctime_ns = excluded.ctime_ns,
                    last_seen_ts = excluded.last_seen_ts,
                    checksum = COALESCE(excluded.checksum, files.checksum),
                    status = COALESCE(excluded.status, files.status),
                    remote_file_id = COALESCE(excluded.remote_file_id, files.remote_file_id),
                    transfer_id = COALESCE(excluded.transfer_id, files.transfer_id)
                """,
                (
                    record.path,
                    record.size,
                    record.mtime_ns,
                    record.ctime_ns,
                    record.last_seen_ts,
                    record.checksum,
                    record.status,
                    record.remote_file_id,
                    record.transfer_id,
                )
            )

    def get_file_by_path(self, path: str) -> Optional[FileRecord]:
        with self._connect() as conn:
            cursor = conn.execute("SELECT * FROM files WHERE path=?", (path,))
            row = cursor.fetchone()
            if row is None:
                return None
            return FileRecord(**row)

def walk(root: Path) -> Iterable[Path]:
    for dirpath, dirnames, filenames in os.walk(root, followlinks=False):
        base = Path(dirpath)
        for name in filenames:
            yield base / name

def build_file_db_from_scan(db: FileIndexDB, root: Path, compute_checksums: bool = True):
    scan_started = int(time.time())
    count = 0
    for p in walk(root):
        if not p.is_file():
            continue


