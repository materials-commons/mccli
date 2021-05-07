import argparse
import json
import os
import requests
import sys

import materials_commons.api as mcapi
import materials_commons.cli.functions as clifuncs
from materials_commons.cli.exceptions import MCCLIException


def init_project(name, description="", prefix=None, remote_config=None):
    """Initialize directory prefix/name as a new project

    Arguments
    ---------
    name: str, Project name. If prefix/name does not exist it will be created.
    description: str, Project description.
    prefix: str, The project directory will be created at prefix/name.
    remote_config: RemoteConfig, The remote where the project will be created.

    Returns
    -------
    proj: mcapi.Project

    Raises
    ------
    MCCLIException: If any of the following occur:
        - prefix does not exist
        - prefix/name is a file
        - prefix/name/.mc already exists
    """
    if prefix is None:
        prefix = os.getcwd()

    if not os.path.exists(prefix):
        raise MCCLIException("Error in init_project: '" + prefix + "' does not exist.")

    proj_path = os.path.join(prefix, name)

    if os.path.isfile(proj_path):
        raise MCCLIException("Error in init_project: '" + proj_path + "' is a file.")

    if os.path.exists(os.path.join(proj_path, ".mc")):
        pconfig = clifuncs.read_project_config(proj_path)

        # if .mc directory already exists, print error message
        if pconfig:
            try:
                proj = clifuncs.make_local_project(proj_path)
            except MCCLIException as e:
                # print(e)
                s = "A .mc directory already exists, but could not find existing project.\n"
                s += "This may mean the project was deleted.\n"
                s += "If you wish to create a new project here, first delete the .mc directory.\n"
                raise MCCLIException(s)

            raise MCCLIException("Already in project.  name: " + proj.name + "   id: " + str(proj.id))
    else:
        if not os.path.exists(proj_path):
            os.mkdir(proj_path)

    # create new project
    client = remote_config.make_client()
    try:
        proj_request = mcapi.CreateProjectRequest(description=description)
        proj = client.create_project(name, attrs=proj_request)
    except requests.exceptions.ConnectionError as e:
        print(e)
        raise MCCLIException("Could not connect to " + remote_config.mcurl)
    proj.local_path = proj_path
    proj.remote = client

    # create project config directory and file
    pconfig = clifuncs.ProjectConfig(proj.local_path)
    pconfig.remote = remote_config
    pconfig.project_id = proj.id
    pconfig.project_uuid = proj.uuid
    pconfig.save()

    return proj

def make_parser():
    """Make argparse.ArgumentParser for `mc init`"""
    parser = argparse.ArgumentParser(
        description='Initialize current working directory as a new project',
        prog='mc init')
    clifuncs.add_remote_option(parser, 'Remote to create project at')
    parser.add_argument('--desc', type=str, default='', help='Project description')
    return parser

def init_subcommand(argv, working_dir):
    """
    Initialize a new project

    mc init [--remote <remote>] [--desc <description>]

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    # get remote, from command line option or default
    remote_config = clifuncs.optional_remote_config(args)

    proj_path = working_dir
    name = os.path.basename(proj_path)
    prefix = os.path.dirname(proj_path)

    proj = init_project(name, args.desc, prefix=prefix, remote_config=remote_config)

    print("Created new project at:", remote_config.mcurl)
    clifuncs.print_projects([proj], proj)
    print("")
