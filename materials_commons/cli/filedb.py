from dataclasses import dataclass
from pathlib import Path
from typing import Optional, List
from materials_commons.cli.models import FileRecord

import aiosqlite


def to_project_db_path(project_root: Path | str) -> Path:
    return Path(project_root) / ".mc" / "mc2.sqlite"


class FileIndexDB:
    def __init__(self, db_path: Path):
        self.db_path = db_path
        self._write_conn: Optional[aiosqlite.Connection] = None

    @classmethod
    async def create(cls, db_path: Path) -> 'FileIndexDB':
        self = cls(db_path)
        self._write_conn = await self._write_connect()
        await FileIndexDB._init_db(self._write_conn)
        return self

    @staticmethod
    async def _init_db(conn: aiosqlite.Connection):
        await conn.execute(
            """
            CREATE TABLE IF NOT EXISTS files
            (
                path                TEXT PRIMARY KEY,
                dir                 TEXT    NOT NULL,
                name                TEXT    NOT NULL,
                is_clean_local_copy INTEGER NOT NULL DEFAULT 0,

                local_size          INTEGER NOT NULL,
                local_mtime_ns      INTEGER NOT NULL,
                local_ctime_ns      INTEGER NOT NULL,
                local_last_seen_ts  INTEGER NOT NULL,
                local_checksum      TEXT,

                remote_file_id      INTEGER,
                remote_size         INTEGER,
                remote_ctime_ns     INTEGER,
                remote_checksum     TEXT,
                remote_last_seen_ts INTEGER,

                status              TEXT,
                origin              TEXT,
                transfer_id         TEXT
            ) STRICT;
            """
        )
        await conn.execute("CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);")
        await conn.execute("CREATE INDEX IF NOT EXISTS idx_files_last_seen ON files(local_last_seen_ts);")
        await conn.commit()

    async def _write_connect(self) -> aiosqlite.Connection:
        conn = await aiosqlite.connect(self.db_path, timeout=30.0, check_same_thread=False)
        conn.row_factory = aiosqlite.Row
        await conn.execute("PRAGMA journal_mode=WAL;")
        await conn.execute("PRAGMA busy_timeout=5000;")
        await conn.execute("PRAGMA synchronous=NORMAL;")
        await conn.execute("PRAGMA cache_size=100000;")
        await conn.execute("PRAGMA temp_store=MEMORY;")
        await conn.execute("PRAGMA foreign_keys=ON;")
        return conn

    async def _read_connect(self) -> aiosqlite.Connection:
        conn = await aiosqlite.connect(self.db_path)
        conn.row_factory = aiosqlite.Row
        await conn.execute("PRAGMA query_only=ON;")
        await conn.execute("PRAGMA busy_timeout=5000;")
        await conn.execute("PRAGMA foreign_keys=ON;")
        return conn

    async def close(self):
        if self._write_conn is None:
            return
        await self._write_conn.close()
        self._write_conn = None

    async def __aenter__(self) -> 'FileIndexDB':
        if self._write_conn is None:
            self._write_conn = await self._write_connect()
            await FileIndexDB._init_db(self._write_conn)
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        await self.close()

    class _Transaction:
        def __init__(self, conn: aiosqlite.Connection):
            self._conn = conn

        async def __aenter__(self):
            await self._conn.execute("BEGIN;")
            return self._conn

        async def __aexit__(self, exc_type, exc_val, exc_tb):
            if exc_type is None:
                await self._conn.execute("COMMIT;")
            else:
                await self._conn.execute("ROLLBACK;")

    def transaction(self) -> '_Transaction':
        return self._Transaction(self._write_conn)

    async def upsert(self, record: FileRecord):
        await self._write_conn.execute(
            """
            INSERT INTO files(path, dir, name, is_clean_local_copy, local_size, local_mtime_ns, local_ctime_ns,
                              local_last_seen_ts, local_checksum, remote_file_id, remote_size, remote_ctime_ns,
                              remote_checksum, remote_last_seen_ts, status, origin, transfer_id)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(path) DO UPDATE SET dir                 = excluded.dir,
                                            name                = excluded.name,
                                            is_clean_local_copy = excluded.is_clean_local_copy,

                                            local_size          = excluded.local_size,
                                            local_mtime_ns      = excluded.local_mtime_ns,
                                            local_ctime_ns      = excluded.local_ctime_ns,
                                            local_last_seen_ts  = excluded.local_last_seen_ts,
                                            local_checksum      = COALESCE(excluded.local_checksum, files.local_checksum),

                                            remote_file_id      = COALESCE(excluded.remote_file_id, files.remote_file_id),
                                            remote_size         = COALESCE(excluded.remote_size, files.remote_size),
                                            remote_ctime_ns     = COALESCE(excluded.remote_ctime_ns, files.remote_ctime_ns),
                                            remote_checksum     = COALESCE(excluded.remote_checksum, files.remote_checksum),
                                            remote_last_seen_ts = COALESCE(excluded.remote_last_seen_ts, files.remote_last_seen_ts),

                                            status              = COALESCE(excluded.status, files.status),
                                            origin              = COALESCE(excluded.origin, files.origin),
                                            transfer_id         = COALESCE(excluded.transfer_id, files.transfer_id)
            """,
            (
                record.path,
                record.dir,
                record.name,
                record.is_clean_local_copy,

                record.local_size,
                record.local_mtime_ns,
                record.local_ctime_ns,
                record.local_last_seen_ts,
                record.local_checksum,

                record.remote_file_id,
                record.remote_size,
                record.remote_ctime_ns,
                record.remote_checksum,
                record.remote_last_seen_ts,

                record.status,
                record.origin,
                record.transfer_id,
            )
        )
        # self._write_conn.commit()

    async def upsert_many(self, records: List[FileRecord]):
        conn = self._write_conn
        params = [
            (
                r.path,
                r.dir,
                r.name,
                r.is_clean_local_copy,

                r.local_size,
                r.local_mtime_ns,
                r.local_ctime_ns,
                r.local_last_seen_ts,
                r.local_checksum,

                r.remote_file_id,
                r.remote_size,
                r.remote_ctime_ns,
                r.remote_checksum,
                r.remote_last_seen_ts,

                r.status,
                r.origin,
                r.transfer_id,
            )
            for r in records
        ]

        if not params:
            return

        async with self.transaction():
            await conn.executemany(
                """
                INSERT INTO files(path, dir, name, is_clean_local_copy, local_size, local_mtime_ns,
                                  local_ctime_ns, local_last_seen_ts, local_checksum, remote_file_id, remote_size,
                                  remote_ctime_ns, remote_checksum, remote_last_seen_ts, status, origin, transfer_id)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(path) DO UPDATE SET dir                 = excluded.dir,
                                                name                = excluded.name,
                                                is_clean_local_copy = excluded.is_clean_local_copy,

                                                local_size          = excluded.local_size,
                                                local_mtime_ns      = excluded.local_mtime_ns,
                                                local_ctime_ns      = excluded.local_ctime_ns,
                                                local_last_seen_ts  = excluded.local_last_seen_ts,
                                                local_checksum      = COALESCE(excluded.local_checksum, files.local_checksum),

                                                remote_file_id      = COALESCE(excluded.remote_file_id, files.remote_file_id),
                                                remote_size         = COALESCE(excluded.remote_size, files.remote_size),
                                                remote_ctime_ns     = COALESCE(excluded.remote_ctime_ns, files.remote_ctime_ns),
                                                remote_checksum     = COALESCE(excluded.remote_checksum, files.remote_checksum),
                                                remote_last_seen_ts = COALESCE(excluded.remote_last_seen_ts, files.remote_last_seen_ts),

                                                status              = COALESCE(excluded.status, files.status),
                                                origin              = COALESCE(excluded.origin, files.origin),
                                                transfer_id         = COALESCE(excluded.transfer_id, files.transfer_id)
                """,
                params,
            )

    async def get_file_by_path(self, path: str) -> Optional[FileRecord]:
        conn = await self._read_connect()
        try:
            cursor = await conn.execute("SELECT * FROM files WHERE path=?", (path,))
            row = await cursor.fetchone()
            if row is None:
                return None
            return FileRecord(**row)
        finally:
            await conn.close()

    async def get_files_by_dir(self, files_dir: str) -> List[FileRecord]:
        conn = await self._read_connect()
        try:
            cursor = await conn.execute("SELECT * FROM files WHERE dir=?", (files_dir,))
            rows = await cursor.fetchall()
            return [FileRecord(**row) for row in rows]
        finally:
            await conn.close()
