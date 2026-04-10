import asyncio
from pathlib import Path
from typing import Optional, AsyncIterator

from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.models import LocalProject, FileEntry, FileDecision
from materials_commons.cli.reconcile2 import observe_and_reconcile2
from materials_commons.cli.server import projects
from materials_commons.cli.walk import ListDirFunc, IgnoreFunc, async_walk


class AsyncReconciler:
    def __init__(self,
                 proj: LocalProject,
                 db: FileIndexDB,
                 recompute_checksum: bool = True,
                 max_concurrent: int = 10):
        self.proj = proj
        self.db = db
        self.recompute_checksum = recompute_checksum
        self.max_concurrent = max_concurrent

    async def walk(self, path: str | Path, listdir_fn: ListDirFunc, recursive: bool = False,
                   ignore_fn: Optional[IgnoreFunc] = None) -> \
            AsyncIterator[tuple[Path, dict[str, FileEntry]]]:
        sem = asyncio.Semaphore(self.max_concurrent)

        async def single_entry_reconcile(e: FileEntry) -> FileDecision:
            async with sem:
                if e.local_entry is not None:
                    file_path = e.local_entry.path
                else:
                    remote_path = Path(e.remote_entry.directory.path) / e.remote_entry.name
                    file_path = projects.remote_to_local_project_path(proj_base=Path(self.proj.local_path),
                                                                      remote_path=remote_path)

                project_path = projects.local_to_remote_project_path(Path(self.proj.local_path), Path(file_path))
                return await observe_and_reconcile2(file_record=e.file_record,
                                                    project_path=project_path.as_posix(),
                                                    file_path=file_path.as_posix(),
                                                    remote_entry=e.remote_entry,
                                                    recompute_checksum=self.recompute_checksum)

        async for current_path, entries in async_walk(path, recursive=recursive, listdir_fn=listdir_fn,
                                                      ignore_fn=ignore_fn):
            path_entries: dict[str, FileEntry] = {}
            remote_dir = projects.local_to_remote_project_path(Path(self.proj.local_path), Path(current_path))
            remote_entries = await projects.list_remote_project_dir_by_path(self.proj.remote, self.proj.id,
                                                                            remote_dir.as_posix())

            # Create a map of file_records to name
            file_records = await self.db.get_files_by_dir(remote_dir.as_posix())
            file_records_map = {file_record.name: file_record for file_record in file_records}

            # First, we go through all the remote entries and add them to the path_entries dict
            for remote_entry in remote_entries.values():
                path_entries[remote_entry.name] = FileEntry(remote_entry=remote_entry, local_entry=None,
                                                            file_decision=None, file_record=None)

            # Next, we go through all the local entries. If that local entry exists, then the remote and the
            # local entries are linked. Otherwise, we have a local only entry.
            for entry in entries:
                found_remote_entry = path_entries.get(entry.name, None)
                file_record = file_records_map.get(entry.name, None)
                if found_remote_entry:
                    found_remote_entry.local_entry = entry
                    found_remote_entry.file_record = file_record
                else:
                    path_entries[entry.name] = FileEntry(local_entry=entry, remote_entry=None,
                                                         file_decision=None, file_record=file_record)

            # Now run reconciliation against each of the path_entries
            entries_list = list(path_entries.values())
            results = await asyncio.gather(*[single_entry_reconcile(entry) for entry in entries_list],
                                           return_exceptions=True)

            # At this point path_entries contains entries in one of 4 states:
            # 1. Both remote and local entries exist
            # 2. Only remote entry exists
            # 3. Only local entry exists
            # 4. Lookup failed, and we got an exception

            for entry, result in zip(entries_list, results):
                if isinstance(result, Exception):
                    entry.exception = result
                else:
                    entry.file_decision = result

            yield current_path, path_entries
