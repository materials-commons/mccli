from dataclasses import dataclass
from pathlib import Path
from typing import Optional, List

import aiosqlite

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
        await conn.execute("CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);")
        await conn.execute("CREATE INDEX IF NOT EXISTS idx_files_last_seen ON files(last_seen_ts);")
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
        # self._write_conn.commit()

    async def upsert_many(self, records: List[FileRecord]):
        conn = self._write_conn
        params = [
            (
                r.path,
                r.size,
                r.mtime_ns,
                r.ctime_ns,
                r.last_seen_ts,
                r.checksum,
                r.status,
                r.remote_file_id,
                r.transfer_id,
            )
            for r in records
        ]

        if not params:
            return

        async with self.transaction():
            await conn.executemany(
                """
                INSERT INTO files(path, size, mtime_ns, ctime_ns, last_seen_ts, checksum, status, remote_file_id,
                                  transfer_id)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(path) DO UPDATE SET size           = excluded.size,
                                                mtime_ns       = excluded.mtime_ns,
                                                ctime_ns       = excluded.ctime_ns,
                                                last_seen_ts   = excluded.last_seen_ts,
                                                checksum       = COALESCE(excluded.checksum, files.checksum),
                                                status         = COALESCE(excluded.status, files.status),
                                                remote_file_id = COALESCE(excluded.remote_file_id, files.remote_file_id),
                                                transfer_id    = COALESCE(excluded.transfer_id, files.transfer_id)
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



