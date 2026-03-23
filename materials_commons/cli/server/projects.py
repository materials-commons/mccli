import asyncio
import os
import json
from pathlib import Path
from typing import Optional

import materials_commons.api as mcapi
from materials_commons.api import models

# Cache list of projects
_projects = None

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
    global _projects

    # if _projects are not None, and we haven't been asked to reload the _projects,
    # then return the cached list. A user might ask for a reload when they think
    # the list has changed.
    if _projects is not None:
        if not reload:
            return _projects
    _projects = []
    current_directory = os.getcwd()

    # Iterate over all entries in the current directory
    for entry in os.listdir(current_directory):
        project_dir = os.path.join(current_directory, entry)
        # print(f"project_dir: {project_dir}")

        # We only care about directories
        if not os.path.isdir(project_dir):
            continue

        # Check for the existence of .mc/config.json
        config_path = os.path.join(project_dir, ".mc", "config.json")
        if not os.path.exists(config_path):
            continue

        try:
            with open(config_path, 'r') as f:
                data = json.load(f)

                # Check if project_id exists in the JSON data
                if "project_id" in data:
                    _projects.append({
                        "directory": entry,
                        "project_dir_path": project_dir,
                        "project_id": data["project_id"]
                    })
        except (json.JSONDecodeError, IOError):
            # Skip files that aren't valid JSON or can't be read
            continue

    return _projects

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
    projects = list_local_projects(reload)
    for project in projects:
        if project["project_id"] == project_id:
            return project
    return None

def local_to_remote_project_path(proj_base: Path, full_path: Path) -> Path:
    """
    Converts a local project path to its corresponding remote path. For example,
    if the proj_base is "/home/user/projects" and the full_path is "/home/user/projects/my_project/data.txt",
    the function will return "/my_project/data.txt".

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
    if the proj_base is "/home/user/projects" and the remote_path is "/my_project/data.txt",
    the function will return "/home/user/projects/my_project/data.txt".

    Args:
        proj_base (Path): The base directory of the local project.
        remote_path (Path): The remote path to be converted.

    Returns:
        Path: The local path corresponding to the remote path.
    """

    # To get the full path, we need to remove the leading slash from the remote path
    return proj_base / remote_path.as_posix().lstrip("/")

async def list_remote_project_dir_by_path(c: mcapi.Client, project_id: int, project_path: str) -> Optional[dict[str, models.File]]:
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

