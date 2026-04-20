import asyncio
import json
import os
import threading
from pathlib import Path
from typing import Optional

import materials_commons.api as mcapi
from materials_commons.api import models

from materials_commons.cli.filedb import FileIndexDB
from materials_commons.cli.functions import make_local_project

# Cache list of projects
_projects = None
_projects_lock = threading.Lock()


def _scan_local_projects():
    projects = []
    current_directory = os.getcwd()

    for entry in os.listdir(current_directory):
        project_dir = os.path.join(current_directory, entry)

        if not os.path.isdir(project_dir):
            continue

        config_path = os.path.join(project_dir, ".mc", "config.json")
        if not os.path.exists(config_path):
            continue

        try:
            with open(config_path, "r") as f:
                data = json.load(f)

                if "project_id" in data:
                    projects.append({
                        "directory": entry,
                        "project_dir_path": project_dir,
                        "project_id": data["project_id"]
                    })
        except (json.JSONDecodeError, IOError):
            continue

    return projects


def _get_cached_or_scanned_projects(reload=False):
    global _projects

    with _projects_lock:
        if _projects is not None and not reload:
            return _projects

    projects = _scan_local_projects()

    with _projects_lock:
        _projects = projects
        return _projects


def list_local_projects(reload=False):
    """
    Retrieves a list of local projects in the current working directory. Each project must contain
    a specific configuration file to be included in the list.

    Parameters:
    reload (bool, optional): When set to True, forces reloading of the project list even if a cached
        version exists. Defaults to False.

    Returns:
    list: A list of dictionaries representing local projects. Each dictionary contains the following keys:
        - directory (str): The name of the project directory.
        - project_dir_path (str): The absolute path to the project directory.
        - project_id (str): The unique identifier of the project as specified in the configuration file.

    Raises:
    JSONDecodeError: If a configuration file is not a valid JSON.
    IOError: If there is an issue reading the configuration file, such as missing file permissions.
    """
    return _get_cached_or_scanned_projects(reload)


def get_local_project_by_id(project_id, reload=False):
    """
    Retrieves a local project by its unique project ID.

    This function searches through a list of local projects to find a project
    that matches the given project ID. The project details are returned if a match
    is found. If no match is found, the function returns None.

    Args:
        project_id (str): The unique identifier of the project to be retrieved.
        reload (bool, optional): When set to True, forces reloading of the project list even if a cached
        version exists. Defaults to False.

    Returns:
        dict | None: The project details as a dictionary if found, otherwise None.
    """
    projects = _get_cached_or_scanned_projects(reload)
    for project in projects:
        if project["project_id"] == project_id:
            return project
    return None


async def async_list_local_projects(reload=False):
    return await asyncio.to_thread(list_local_projects, reload)


async def async_get_local_project_by_id(project_id, reload=False):
    projects = await async_list_local_projects(reload)
    for project in projects:
        if project["project_id"] == project_id:
            return project
    return None


def local_to_remote_project_path(proj_base: Path, full_path: Path) -> Path:
    """
    Converts a local project path to its corresponding remote path. For example,
    if the proj_base is "/home/user/myproject" and the full_path is "/home/user/myproject/dir/data.txt",
    the function will return "/dir/data.txt".

    Args:
        proj_base (Path): The base directory of the local project.
        full_path (Path): The full local path to be converted.

    Returns:
        Path: The remote path corresponding to the local path.
    """
    # remote_path will be the relative path from proj_base to full_path, i.e., it
    # won't start with a slash, so we need to add one to get the correct path.
    if full_path.as_posix() == proj_base.as_posix():
        return Path("/")
    remote_path = full_path.relative_to(proj_base)
    return Path("/" + remote_path.as_posix())


def remote_to_local_project_path(proj_base: Path, remote_path: Path) -> Path:
    """
    Converts a remote project path to its corresponding local path. For example,
    if the proj_base is "/home/user/myproject" and the remote_path is "/dir/data.txt",
    the function will return "/home/user/myproject/dir/data.txt".

    Args:
        proj_base (Path): The base directory of the local project.
        remote_path (Path): The remote path to be converted.

    Returns:
        Path: The local path corresponding to the remote path.
    """

    # To get the full path, we need to remove the leading slash from the remote path
    return proj_base / remote_path.as_posix().lstrip("/")


async def list_remote_project_dir_by_path(c: mcapi.Client, project_id: int, project_path: str) -> Optional[
    dict[str, models.File]]:
    """List the contents of a directory in a remote project. Builds a dict of directory entries by path."""
    path_entries = await asyncio.to_thread(c.list_directory_by_path, project_id, project_path)
    if not path_entries:
        return {}

    entries = {}
    for entry in path_entries:
        if entry.directory is None:
            continue
        entries[os.path.join(entry.directory.path, entry.name)] = entry
    return entries


async def get_remote_file_by_path(c: mcapi.Client, project_id: int, project_path: str) -> Optional[models.File]:
    """Get a file from a remote project by its path."""
    try:
        return await asyncio.to_thread(c.get_file_by_path, project_id, project_path)
    except Exception:
        return None


def project_config_dir_path(path: str):
    curr = path
    while True:
        dot_mc_dir = os.path.join(curr, '.mc')
        if os.path.isdir(dot_mc_dir):
            # Found .mc directory, return path
            return curr
        elif curr == os.path.dirname(curr):
            # Reached root (/) directory, no .mc directory found
            return None
        else:
            # Move up one level
            curr = os.path.dirname(curr)


async def get_local_project(path: str):
    return await asyncio.to_thread(make_local_project, path)


async def is_dir(db: FileIndexDB, proj: models.Project, path: str) -> bool:
    # First check if the path exists locally
    try:
        p = Path(path)
        return p.is_dir()
    except Exception:
        pass
    # sinfo = await safe_stat(path)
    # if sinfo is not None:
    #     return stat.S_ISDIR(sinfo.st_mode)

    # Next, check if the path exists in the database
    remote_entry = await db.get_file_by_path(path)
    if remote_entry is not None:
        # Only files are tracked in the database, so if the entry was found
        # then the path is not a directory.
        return False

    # Finally, check if the entry is a directory on the server
    proj_path = local_to_remote_project_path(proj.local_path, Path(path))
    try:
        f = await asyncio.to_thread(proj.remote.get_file_by_path, proj.id, proj_path)
        if f is None:
            return False
        return f.mime_type == "directory"
    except Exception:
        return False
