import json
import os.path
import requests
import materials_commons.api as mcapi
from materials_commons.cli.exceptions import MCCLIException

def isfile(file_or_dir):
    return isinstance(file_or_dir, mcapi.File) and file_or_dir.mime_type != "directory"

def isdir(file_or_dir):
    return isinstance(file_or_dir, mcapi.File) and file_or_dir.mime_type == "directory"

def get_parent_id(file_or_dir):
    """Get file_or_dir.directory_id, else raise"""
    if hasattr(file_or_dir, 'directory_id'):
        if file_or_dir.directory_id == file_or_dir.id:
            raise MCCLIException("directory_id == id")
        return file_or_dir.directory_id
    else:
        raise MCCLIException("file_or_dir is missing attribute directory_id")

def make_local_abspath(proj_local_path, mcpath):
    return os.path.normpath(os.path.join(proj_local_path, os.path.relpath(mcpath, "/")))

def make_mcpath(proj_local_path, local_abspath):
    """
        :param str proj_local_path: Path to project, i.e. "/path/to/project"
        :param str local_abspath: Path to file in project, i.e. "/path/to/project/path/to/file"
        :return str mcpath: Returns a Materials Commons style path, i.e. "/path/to/file"
    """
    relpath = os.path.relpath(local_abspath, proj_local_path)
    if relpath == ".":
        mcpath = "/"
    else:
        mcpath = os.path.join("/", relpath)
    return mcpath

def get_by_path_if_exists(client, project_id, file_path):
    """
    Get file (or directory) by path in project, if it exists.
    :param int project_id: The id of the project containing the file or directory
    :param file_path: The Materials Commons path to the file or directory
    :return: The file or None
    :rtype File or None
    """
    try:
        return client.get_file_by_path(project_id, file_path)
    except mcapi.MCAPIError as e:
        if e.response.status_code == 404:
            return None
        raise e

def _check_file_selection_dirs(path, file_selection, orig_path=None):
    """Recursively checks if a path is included in a dataset file selection, and why"""
    if path == orig_path:
        selected_by = "(self)"
    else:
        selected_by = path
    if path in file_selection['include_dirs']:
        return (True, selected_by)
    elif path in file_selection['exclude_dirs']:
        return (False, selected_by)
    else:
        parent = os.path.dirname(path)
        if parent == "/":
            return (False, None)
        else:
            return _check_file_selection_dirs(parent, file_selection, orig_path=orig_path)

def check_file_selection(path, file_selection):
    """Check if a file or directory is selected in a dataset file selection, and why.

    Notes:
        A file is selected in a dataset's file selection if it is listed explicitly in "include_files". It is not selected if it is listed in "exclude_files". If it is neither explicitly included or excluded, its parent directories are searched up to the project root directory to see if they are included or excluded. If the first parent directory listed is in "include_dirs", then the file is also selected. If the first parent directory listed is in "exclude_dirs", or if no parent directory is listed at all, the file is not selected.

    Args:
        path (str): Materials Commons path of file or directory to check
        file_selection (dict): A file selection dict. Expected format: ::

            {
                "include_files": [... list of file paths ...],
                "exclude_files": [... list of file paths ...],
                "include_dirs": [... list of directory paths ...],
                "exclude_dirs": [... list of directory paths ...]
            }

    Returns:
        (selected, selected_by):

        selected (bool): True if selected, False if not selected

        selected_by (str or None): One of "(self)" if included/excluded explicitly; Else, "<path>", the path of the first parent directory that is included or excluded; Otherwise, None, to indicate that it is not selected, but neither included nor excluded.
    """
    result = None
    if path in file_selection['include_files']:
        result = (True, "(self)")
    elif path in file_selection['exclude_files']:
        result = (False, "(self)")
    else:
        result = _check_file_selection_dirs(path, file_selection, orig_path=path)
    return result
