import asyncio
from pathlib import Path
from typing import Optional, AsyncIterator

from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.models import LocalProject, FileState, FileDecision, WalkObservation
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
                   ignore_fn: Optional[IgnoreFunc] = None) -> AsyncIterator[tuple[Path, dict[str, FileState]]]:
        sem = asyncio.Semaphore(self.max_concurrent)

        async def single_entry_reconcile(obs: WalkObservation) -> FileDecision:
            async with sem:
                if obs.local_path is not None:
                    file_path = obs.local_path
                elif obs.remote_path is not None:
                    file_path = projects.remote_to_local_project_path(proj_base=Path(self.proj.local_path),
                                                                      remote_path=obs.remote_path)
                elif obs.local_entry is not None:
                    file_path = obs.local_entry.path
                elif obs.remote_entry is not None:
                    file_path = projects.remote_to_local_project_path(proj_base=Path(self.proj.local_path),
                                                                      remote_path=obs.remote_entry.path)
                else:
                    raise ValueError("No local or remote path found for entry")

                project_path = projects.local_to_remote_project_path(Path(self.proj.local_path), Path(file_path))
                remote_entry = obs.remote_entry.raw if obs.remote_entry else None
                decision = await observe_and_reconcile2(file_record=obs.file_record,
                                                        project_path=project_path.as_posix(),
                                                        file_path=file_path.as_posix(),
                                                        remote_entry=remote_entry,
                                                        recompute_checksum=self.recompute_checksum)
                return decision

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
