"""Temporary functions"""
import json
import os.path
from collections.abc import Iterable
from materials_commons.cli.exceptions import MCCLIException

def get_dataset(client, project_id, dataset_id):
    """Temporary workaround because Client.get_dataset is returning the wrong dataset"""
    # TODO: update
    dataset = client.update_dataset_file_selection(project_id, dataset_id, {})
    dataset.project_id = project_id
    add_owner(client, dataset)
    return dataset

def set_file_paths(client, project_id, files):
    """Temporary workaround because Client.get_dataset is returning the wrong dataset"""
    # TODO: update
    directories_by_id = dict()
    for file in files:
        if file.path is not None:
            continue
        if file.directory_id not in directories_by_id:
            directory = client.get_directory(project_id, file.directory_id)
            directories_by_id[directory.id] = directory
        file.path = os.path.join(directories_by_id[file.directory_id].path, file.name)

def get_dataset_file_selection(client, project_id, dataset_id):
    """Return dataset file selection dict"""
    dataset = get_dataset(client, project_id, dataset_id)
    if 'file_selection' not in dataset._data:
        raise MCCLIException("dataset does not include 'file_selection'")
    return dataset._data['file_selection']

def get_published_dataset_file_selection(client, dataset_id):
    """Return dataset file selection dict"""
    dataset = get_published_dataset(client, dataset_id)
    if 'file_selection' not in dataset._data:
        raise MCCLIException("dataset does not include 'file_selection'")
    return dataset._data['file_selection']

def _add_owner(client, obj):
    if hasattr(obj, 'owner'):
        return
    elif hasattr(obj, 'owner_id'):
        if not hasattr(client, '_users_by_id') or obj.owner_id not in client._users_by_id:
            users = client.list_users()
            client._users_by_id = {u.id:u for u in users}
        if obj.owner_id not in client._users_by_id:
            raise MCCLIException("Could not find owner_id:" + str(owner_id))
        obj.owner = client._users_by_id[obj.owner_id]
    else:
        raise MCCLIException("Object does not have owner or owner_id")

def add_owner(client, objects):
    """Add 'owner' based on 'owner_id'

    Args:
        client (materials_commons.api.Client): Materials Commons Client
        objects (object or Iterable of objects): Objects with 'owner_id'

    Notes:
        This will create a cache, client._user_by_id, a dict of owner_id:materials_commons.api.User.
    """
    if isinstance(objects, Iterable):
        for obj in objects:
            _add_owner(client, obj)
    else:
        _add_owner(client, objects)
