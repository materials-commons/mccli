import argparse
import os
import sys

import materials_commons.api as mcapi
from .. import functions as clifuncs
from .. import tree_functions as treefuncs
from ..treedb import RemoteTree


def mkdir_subcommand(argv):
    """
    Make remote directories.

    mc mkdir [<pathspec> ...]

    """
    parser = argparse.ArgumentParser(
        description='Make remote directories',
        prog='mc mkdir')
    parser.add_argument('paths', nargs='*', default=[os.getcwd()], help='Directory names')
    parser.add_argument('-p', action="store_true", default=False, help='Create intermediate directories as necessary')
    parser.add_argument('--remote-only', action="store_true", default=False,
                        help='Make remote directories only. Does not compare to local tree.')

    # ignore 'mc ls'
    args = parser.parse_args(argv)

    proj = clifuncs.make_local_project()
    pconfig = clifuncs.read_project_config()

    # convert cli input to materials commons path convention: /path/to/file_or_dir
    mcpaths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths)

    remotetree = None
    if pconfig.remote_updatetime:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    for path in mcpaths:
        treefuncs.mkdir(proj, path, remote_only=args.remote_only, create_intermediates=args.p, remotetree=remotetree)

    return
