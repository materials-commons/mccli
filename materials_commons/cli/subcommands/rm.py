import argparse
import os
import sys

import materials_commons.cli.functions as clifuncs
import materials_commons.cli.tree_functions as treefuncs
from materials_commons.cli.treedb import LocalTree, RemoteTree

def make_parser():
    """Make argparse.ArgumentParser for `mc rm`"""
    parser = argparse.ArgumentParser(
        description='remove files and directories',
        prog='mc down')
    parser.add_argument('paths', nargs='*', default=None, help='Files or directories')
    parser.add_argument('-r', '--recursive', action="store_true", default=False,
                        help='Remove recursively')
    parser.add_argument('--remote-only', action="store_true", default=False,
                        help='Remove remote files only. Does not compare to local files.')
    parser.add_argument('--no-compare', action="store_true", default=False,
                        help='Remove even if local and remote files differ.')

    # needs re-working:
    # parser.add_argument('-n', '--dry-run', action="store_true", default=False,
    #                     help='Show what would be removed, without actually removing.')
    return parser

def rm_subcommand(argv, working_dir):
    """
    Remove files and directories from Materials Commons and locally

    mc rm [--local] [--remote] [--force] [<pathspec> ...]

    """
    parser = make_parser()
    args = parser.parse_args(argv)

    proj = clifuncs.make_local_project(working_dir)
    pconfig = clifuncs.read_project_config(proj.local_path)

    # convert cli input to materials commons path convention: <projectname>/path/to/file_or_dir
    paths = treefuncs.clipaths_to_mcpaths(proj.local_path, args.paths,
                                          working_dir)

    localtree = None
    if args.no_compare == False:
        localtree = LocalTree(proj.local_path)

    remotetree = None
    if pconfig.remote_updatetime:
        remotetree = RemoteTree(proj, pconfig.remote_updatetime)

    remover = treefuncs.remove(proj, paths, recursive=args.recursive, no_compare=args.no_compare, remote_only=args.remote_only, localtree=localtree, remotetree=remotetree)

    return
