import asyncio
from datetime import timezone
from pathlib import Path
from typing import Optional, AsyncIterator

from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.models import OldLocalProject, FileState, FileDecision, Observation, RemoteFileEntry
from materials_commons.cli.reconcile3 import SingleFileReconciler, ReconcileMode
from materials_commons.cli.server import projects
from materials_commons.cli.walk import ListDirFunc, IgnoreFunc, async_walk, path_to_local_file_entry


class AsyncReconciler:
    def __init__(self,
                 proj: OldLocalProject,
                 db: FileIndexDB,
                 reconcile_mode: ReconcileMode,
                 # recompute_checksum: bool = True,
                 max_concurrent: int = 10):
        self.proj = proj
        self.db = db
        # self.recompute_checksum = recompute_checksum
        self.max_concurrent = max_concurrent
        self.reconcile_mode = reconcile_mode
        self.reconciler = SingleFileReconciler(mode=reconcile_mode)

    async def walk(self, path: str | Path, listdir_fn: ListDirFunc, recursive: bool = False,
                   ignore_fn: Optional[IgnoreFunc] = None) -> AsyncIterator[tuple[Path, dict[str, FileState]]]:
        sem = asyncio.Semaphore(self.max_concurrent)

        async def single_entry_reconcile(obs: Observation) -> FileDecision:
            async with sem:
                return await self.reconciler.reconcile_file(obs)

        async for current_path, observations in async_walk(path, recursive=recursive, listdir_fn=listdir_fn,
                                                           ignore_fn=ignore_fn):
            path_states: dict[str, FileState] = {}
            remote_dir = projects.local_to_remote_project_path(Path(self.proj.local_path), Path(current_path))

            # Create a map of file_records to name
            file_records = await self.db.get_files_by_dir(remote_dir.as_posix())
            file_records_map = {file_record.name: file_record for file_record in file_records}

            # Go through all the observations and match to the file records
            for obs in observations:
                obs.file_record = file_records_map.get(obs.name, None)
                path_states[obs.name] = FileState(observation=obs)

            results = await asyncio.gather(
                *[single_entry_reconcile(state.observation) for state in path_states.values()],
                return_exceptions=True)

            # At this point path_entries contains entries in one of 4 states:
            # 1. Both remote and local entries exist
            # 2. Only remote entry exists
            # 3. Only local entry exists
            # 4. Lookup failed, and we got an exception

            for state, result in zip(path_states.values(), results):
                if isinstance(result, Exception):
                    state.exception = result
                else:
                    state.file_decision = result

            yield current_path, path_states

    async def reconcile_file(self, path: str | Path) -> FileState:
        remote_project_path = projects.local_to_remote_project_path(Path(self.proj.local_path), Path(path))
        file_record = await self.db.get_file_by_path(remote_project_path.as_posix())
        mc_remote_file = await asyncio.to_thread(self.proj.remote.get_file_by_path, self.proj.id,
                                                 remote_project_path.as_posix())
        local_file_entry = path_to_local_file_entry(Path(path))
        remote_file_entry = RemoteFileEntry(
            path=Path(mc_remote_file.path) / mc_remote_file.name,
            name=mc_remote_file.name,
            kind="file",
            size=mc_remote_file.size,
            mtime_ns=int(mc_remote_file.updated_at.replace(tzinfo=timezone.utc).timestamp() * 1_000_000_000),
            ctime_ns=int(mc_remote_file.created_at.replace(tzinfo=timezone.utc).timestamp() * 1_000_000_000),
            remote_file_id=getattr(mc_remote_file, "id", None),
            checksum=getattr(mc_remote_file, "checksum", None),
            raw=mc_remote_file,
        )
        obs = Observation(
            remote_entry=remote_file_entry,
            local_entry=local_file_entry,
            file_record=file_record,
            local_path=Path(path),
            remote_path=remote_project_path,
        )
        decision = await self.reconciler.reconcile_file(obs)

        return FileState(
            observation=obs,
            file_decision=decision,
        )
