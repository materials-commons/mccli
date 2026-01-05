import os
import json


def list_local_projects():
    projects = []
    current_directory = os.getcwd()

    # Iterate over all entries in the current directory
    for entry in os.listdir(current_directory):
        project_dir = os.path.join(current_directory, entry)
        print(f"project_dir: {project_dir}")

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
                    projects.append({
                        "directory": entry,
                        "project_dir_path": project_dir,
                        "project_id": data["project_id"]
                    })
        except (json.JSONDecodeError, IOError):
            # Skip files that aren't valid JSON or can't be read
            continue

    return projects

def get_local_project_by_id(project_id):
    projects = list_local_projects()
    for project in projects:
        if project["project_id"] == project_id:
            return project
    return None