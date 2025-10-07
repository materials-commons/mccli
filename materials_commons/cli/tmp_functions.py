"""Temporary functions"""
import json
import os.path
from collections.abc import Iterable
from materials_commons.cli.exceptions import MCCLIException

def get_dataset(client, project_id, dataset_id):
    """Temporary workaround because Client.get_dataset is returning the wrong dataset"""
    dataset = client.get_dataset(project_id, dataset_id)
    dataset.project_id = project_id # TODO: update when project_id returned
    add_owner(client, dataset) # TODO: update when owner returned as object
    return dataset

def _add_owner(client, obj):
    if hasattr(obj, 'owner'):
        return
    elif hasattr(obj, 'owner_id'):
        if not hasattr(client, '_users_by_id') or obj.owner_id not in client._users_by_id:
            users = client.list_users()
            client._users_by_id = {u.id:u for u in users}
        if obj.owner_id not in client._users_by_id:
            raise MCCLIException("Could not find owner_id:" + str(obj.owner_id))
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
