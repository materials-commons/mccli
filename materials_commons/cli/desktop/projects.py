import os
import json

_projects = None

def list_local_projects(reload=False):
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

def get_local_project_by_id(project_id):
    projects = list_local_projects()
    for project in projects:
        if project["project_id"] == project_id:
            return project
    return None