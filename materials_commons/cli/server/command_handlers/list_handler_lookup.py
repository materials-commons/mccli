import asyncio
import logging
from dataclasses import asdict
from datetime import datetime
from pathlib import Path
from typing import Optional, Dict, Any

from materials_commons.cli import server
from materials_commons.cli.async_reconciler import AsyncReconciler
from materials_commons.cli.filedb import FileIndexDB, to_project_db_path
from materials_commons.cli.models import FileState, LSAction
from materials_commons.cli.server import projects
from materials_commons.cli.server.command_handlers.protocol import CommandHandlerLookup, HandlerFunc
from materials_commons.cli.walk import local_listdir, make_merged_listdir_func

logger = logging.getLogger(__name__)


class ListHandlerLookup(CommandHandlerLookup):
    def __init__(self):
        self._handlers: Dict[str, HandlerFunc] = {
            # List commands
            "LIST_DIRECTORY": _handle_list_directory,
            "LIST_PROJECTS": _handle_list_projects,
            "LIST_PROJECT_DIRECTORY": _handle_list_project_directory,
            "LIST_PROJECT_DIRECTORY_ACTIONS": _handle_list_project_directory_actions,
        }

    def get_handler(self, cmd: str) -> Optional[HandlerFunc]:
        return self._handlers.get(cmd)


async def _handle_list_project_directory_actions(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] list_project_directory_actions -> {cmd}")
    payload = cmd.get("payload") or {}
    request_id = payload.get("request_id")
    project_path = payload.get("project_path")
    project_id = payload.get("project_id")

    proj = projects.get_local_project_by_id(project_id)
    if not proj or not project_path:
        await queue.put({
            "command": "LIST_PROJECT_DIRECTORY_ACTIONS",
            "payload": {"request_id": request_id, "files": []}
        })
        return
    response_payload = {"files": [], "request_id": request_id}

    project_dir_path = Path(proj["project_dir_path"])
    local_project_path = projects.remote_to_local_project_path(project_dir_path, Path(project_path))
    proj = await projects.get_old_local_project(local_project_path.as_posix())
    db = await FileIndexDB.create(to_project_db_path(proj.local_path))

    def build_files(file_entries: dict[str, FileState]) -> list[dict]:
        """
        Build a list of file entries as dictionaries for the response payload. Because this can
        be expensive to run on the event loop, it is offloaded to a thread.
        """
        files_list = []
        for entry_name in sorted(file_entries):
            entry = file_entries[entry_name]
            files_list.append(asdict(LSAction.from_file_state(entry)))
        return files_list

    async_reconciler = AsyncReconciler(db=db, proj=proj, recompute_checksum=False)
    listdir_fn = make_merged_listdir_func(proj)
    files = []
    async for current_path, entries in async_reconciler.walk(path=local_project_path, listdir_fn=listdir_fn,
                                                             recursive=False, ignore_fn=None):
        # run build_files in a thread to avoid blocking the event loop
        files.extend(await asyncio.to_thread(build_files, entries))

    response_payload["files"] = files
    await queue.put({"command": "LIST_PROJECT_DIRECTORY_ACTIONS", "payload": response_payload})


async def _handle_list_project_directory(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] list_project_directory -> {cmd}")
    payload = cmd.get("payload") or {}
    request_id = payload.get("request_id")
    response_payload = {"files": [], "request_id": request_id}
    project_path = payload.get("project_path")
    project_id = payload.get("project_id")

    proj = projects.get_local_project_by_id(project_id)
    if not proj or not project_path:
        print("not proj or project_path")
        await queue.put({"command": "LIST_PROJECT_DIRECTORY", "payload": response_payload})
        return

    project_dir_path = Path(proj["project_dir_path"])
    local_project_path = projects.remote_to_local_project_path(project_dir_path, Path(project_path))
    print(f"[handler] list_project_directory local_project_path = {local_project_path}")

    # This should be moved off the event loop. We should also do this in an async_walk.
    files = []
    try:
        for entry in local_project_path.iterdir():
            try:
                stat = entry.stat()
                remote_proj_path = projects.local_to_remote_project_path(project_dir_path, Path(entry.as_posix()))
                print(f"Appending to files: {remote_proj_path.as_posix()} {stat.st_size}")
                files.append({
                    "name": entry.name,
                    "path": remote_proj_path.as_posix(),
                    "type": "directory" if entry.is_dir() else "file",
                    "size": stat.st_size,
                    "mtime": datetime.fromtimestamp(stat.st_mtime).isoformat() + "Z",
                    "ctime": datetime.fromtimestamp(stat.st_ctime).isoformat() + "Z",
                    "status": "ok",
                    "reason": "A reason",
                })
            except (OSError, PermissionError) as e:
                logger.warning(f"Could not stat {entry}: {e}")
    except Exception as e:
        logger.warning(f"Directory not found: {local_project_path} ({e})")
    response_payload["files"] = files
    await queue.put({"command": "LIST_PROJECT_DIRECTORY", "payload": response_payload})


async def _handle_list_directory(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] list_directory -> {cmd}")
    payload = cmd.get("payload") or {}
    request_id = payload.get("request_id")
    dir_path = payload.get("directory_path")
    response_payload = {"files": [], "request_id": request_id}
    if dir_path:
        files = []
        try:
            for entry in Path(dir_path).iterdir():
                try:
                    stat = entry.stat()
                    files.append({
                        "name": entry.name,
                        "path": entry.as_posix(),
                        "type": "directory" if entry.is_dir() else "file",
                        "size": stat.st_size,
                        "mtime": datetime.fromtimestamp(stat.st_mtime).isoformat() + "Z",
                        "ctime": datetime.fromtimestamp(stat.st_ctime).isoformat() + "Z"
                    })
                except (OSError, PermissionError) as e:
                    logger.warning(f"Could not stat {entry}: {e}")
        except Exception as e:
            logger.warning(f"Directory not found: {dir_path} ({e})")
        response_payload["files"] = files
    await queue.put({"command": "LIST_DIRECTORY", "payload": response_payload})


async def _handle_list_projects(queue: asyncio.Queue, cmd: Dict[str, Any]) -> None:
    print(f"[handler] list_projects -> {cmd}")
    payload = cmd.get("payload") or {}
    local_projects = server.list_local_projects()
    response_payload = {
        "projects": local_projects,
        "request_id": payload.get("request_id"),
    }
    print(f"[handler] list_projects returning -> {response_payload}")
    await queue.put({"command": "LIST_PROJECTS", "payload": response_payload})
