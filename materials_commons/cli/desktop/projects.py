import os
import json

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