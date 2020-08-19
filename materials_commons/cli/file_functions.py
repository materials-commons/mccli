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
